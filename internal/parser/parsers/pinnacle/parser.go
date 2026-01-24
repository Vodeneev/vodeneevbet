package pinnacle

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

var (
	logFileMu sync.Mutex
	logFile   *os.File
)

func init() {
	// Open log file for writing (append mode)
	var err error
	logFile, err = os.OpenFile("/tmp/pinnacle_parser.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// If we can't open log file, just continue without file logging
		logFile = nil
	}
}

func logToFile(msg string) {
	if logFile == nil {
		return
	}
	logFileMu.Lock()
	defer logFileMu.Unlock()
	_, _ = logFile.WriteString(fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), msg))
	_ = logFile.Sync()
}

type Parser struct {
	cfg     *config.Config
	client  *Client
	storage interfaces.Storage
}

func NewParser(cfg *config.Config) *Parser {
	// YDB is not used - data is served directly from in-memory store
	// This makes parsing much faster (no slow YDB writes)
	fmt.Println("Parser running in memory-only mode (no YDB storage)")

	baseURL := cfg.Parser.Pinnacle.BaseURL
	if baseURL == "" {
		baseURL = "https://guest.api.arcadia.pinnacle.com"
	}

	client := NewClient(baseURL, cfg.Parser.Pinnacle.APIKey, cfg.Parser.Pinnacle.DeviceUUID, cfg.Parser.Timeout, cfg.Parser.Pinnacle.ProxyList)

	return &Parser{
		cfg:     cfg,
		client:  client,
		storage: nil, // No YDB storage
	}
}

// runOnce performs a single parsing run
func (p *Parser) runOnce(ctx context.Context) error {
	// If matchup_ids are provided, run targeted mode.
	if len(p.cfg.Parser.Pinnacle.MatchupIDs) > 0 {
		for _, matchupID := range p.cfg.Parser.Pinnacle.MatchupIDs {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if err := p.processMatchup(ctx, matchupID); err != nil {
				fmt.Printf("Pinnacle: failed to process matchup %d: %v\n", matchupID, err)
			}
		}
		return nil
	}

	// Otherwise, discover and process all matchups for relevant sports.
	if err := p.processAll(ctx); err != nil {
		fmt.Printf("Pinnacle: failed to process all matchups: %v\n", err)
		return err
	}
	return nil
}

func (p *Parser) Start(ctx context.Context) error {
	fmt.Println("Starting Pinnacle parser (on-demand mode - parsing triggered by /matches requests)...")

	// Run once at startup to have initial data
	if err := p.runOnce(ctx); err != nil {
		return err
	}

	// Just wait for context cancellation (no background parsing)
	<-ctx.Done()
	return nil
}

// ParseOnce triggers a single parsing run (on-demand parsing)
func (p *Parser) ParseOnce(ctx context.Context) error {
	return p.runOnce(ctx)
}

func (p *Parser) Stop() error { return nil }
func (p *Parser) GetName() string {
	return "Pinnacle"
}

func (p *Parser) processAll(ctx context.Context) error {
	// Map project sports to Pinnacle sports.
	// For now: football -> Soccer.
	targetSportNames := []string{"Soccer"}
	if len(p.cfg.ValueCalculator.Sports) > 0 {
		targetSportNames = nil
		for _, s := range p.cfg.ValueCalculator.Sports {
			if strings.EqualFold(strings.TrimSpace(s), "football") {
				targetSportNames = append(targetSportNames, "Soccer")
			}
		}
		if len(targetSportNames) == 0 {
			targetSportNames = []string{"Soccer"}
		}
	}

	sports, err := p.client.GetSports()
	if err != nil {
		return err
	}
	nameToID := map[string]int64{}
	for _, sp := range sports {
		nameToID[sp.Name] = sp.ID
	}

	// Keep a modest window to avoid processing thousands of stale matchups.
	// Only include future matches (upcoming) - up to 48 hours in the future.
	// Strictly exclude live matches (matches that have already started).
	now := time.Now().UTC()
	maxStart := now.Add(48 * time.Hour)

	for _, sportName := range targetSportNames {
		sportID, ok := nameToID[sportName]
		if !ok || sportID == 0 {
			continue
		}

		// Get regular matchups
		matchups, err := p.client.GetSportMatchups(sportID)
		if err != nil {
			return err
		}

		markets, err := p.client.GetSportStraightMarkets(sportID)
		if err != nil {
			return err
		}

		// Filter markets upfront - only Period 0 (full match pre-match odds)
		marketsByMatchup := map[int64][]Market{}
		filteredStats := map[int64]map[string]int{} // matchupID -> reason -> count
		for _, m := range markets {
			reason := ""
			if m.IsAlternate {
				reason = "IsAlternate"
			} else if m.Status != "open" {
				reason = fmt.Sprintf("Status=%s", m.Status)
			} else if m.Period != 0 {
				reason = fmt.Sprintf("Period=%d", m.Period)
			}
			if reason != "" {
				if filteredStats[m.MatchupID] == nil {
					filteredStats[m.MatchupID] = make(map[string]int)
				}
				filteredStats[m.MatchupID][reason]++
				continue
			}
			marketsByMatchup[m.MatchupID] = append(marketsByMatchup[m.MatchupID], m)
		}

		// Group matchups by main matchup (parentId or self)
		group := map[int64][]RelatedMatchup{}
		filteredByTime := 0
		for _, mu := range matchups {

			st, err := time.Parse(time.RFC3339, mu.StartTime)
			if err != nil {
				continue
			}
			st = st.UTC()

			// Include only matches that haven't started yet (up to 48 hours in the future)
			// Strictly exclude live matches: if startTime is in the past or equal to now, skip it
			if !st.After(now) || st.After(maxStart) {
				filteredByTime++
				continue
			}
			mainID := mu.ID
			if mu.ParentID != nil && *mu.ParentID > 0 {
				mainID = *mu.ParentID
			}
			group[mainID] = append(group[mainID], mu)
		}

		// Process each main matchup as a match.
		for mainID, related := range group {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			// Collect markets for all related matchups
			var relMarkets []Market
			var alternateMarkets []Market // Fallback: alternate markets if no regular markets
			for _, mu := range related {
				relMarkets = append(relMarkets, marketsByMatchup[mu.ID]...)
				// Also collect alternate markets as fallback
				for _, m := range markets {
					if m.MatchupID == mu.ID && m.IsAlternate && m.Status == "open" && m.Period == 0 {
						alternateMarkets = append(alternateMarkets, m)
					}
				}
			}

			// Try to get markets directly if not found in general markets
			if len(relMarkets) == 0 && len(alternateMarkets) == 0 {
				directMarkets, err := p.client.GetRelatedStraightMarkets(mainID)
				if err == nil && len(directMarkets) > 0 {
					// Filter to only open markets with Period 0
					for _, m := range directMarkets {
						if m.Status == "open" && m.Period == 0 && !m.IsAlternate {
							relMarkets = append(relMarkets, m)
						}
					}
				}
			}

			// If no regular markets but we have alternate markets, use them
			if len(relMarkets) == 0 && len(alternateMarkets) > 0 {
				relMarkets = alternateMarkets
			}
			if len(relMarkets) == 0 {
				continue
			}

			m, err := buildMatchFromPinnacle(mainID, related, relMarkets)
			if err != nil || m == nil {
				continue
			}

			// Double-check: do not add live matches (matches that have already started)
			// This is a safety check in case the time filter above missed something
			if !m.StartTime.IsZero() {
				matchStartTime := m.StartTime.UTC()
				checkNow := time.Now().UTC()
				if !matchStartTime.After(checkNow) {
					// Match has already started, skip it
					continue
				}
			}

			// Add match to in-memory store for fast API access (primary storage)
			// YDB is not used - data is served directly from memory
			health.AddMatch(m)
		}
	}

	return nil
}

func (p *Parser) processMatchup(ctx context.Context, matchupID int64) error {
	related, err := p.client.GetRelatedMatchups(matchupID)
	if err != nil {
		return err
	}
	logRelatedMapping(matchupID, related)
	markets, err := p.client.GetRelatedStraightMarkets(matchupID)
	if err != nil {
		return err
	}

	m, err := buildMatchFromPinnacle(matchupID, related, markets)
	if err != nil {
		return err
	}
	if m == nil {
		return nil
	}

	// Do not add live matches (matches that have already started)
	if !m.StartTime.IsZero() {
		matchStartTime := m.StartTime.UTC()
		checkNow := time.Now().UTC()
		if !matchStartTime.After(checkNow) {
			// Match has already started, skip it
			return nil
		}
	}

	// Add match to in-memory store for fast API access (primary storage)
	// YDB is not used - data is served directly from memory
	health.AddMatch(m)

	return nil
}

func buildMatchFromPinnacle(matchupID int64, related []RelatedMatchup, markets []Market) (*models.Match, error) {
	var rm *RelatedMatchup
	for i := range related {
		if related[i].ID == matchupID {
			rm = &related[i]
			break
		}
	}
	if rm == nil && len(related) > 0 {
		rm = &related[0]
	}
	if rm == nil {
		return nil, fmt.Errorf("no related matchups for %d", matchupID)
	}

	home, away := "", ""
	for _, p := range rm.Participants {
		if p.Alignment == "home" {
			home = p.Name
		} else if p.Alignment == "away" {
			away = p.Name
		}
	}
	if home == "" || away == "" {
		return nil, fmt.Errorf("missing participants for %d", matchupID)
	}

	startTime, err := time.Parse(time.RFC3339, rm.StartTime)
	if err != nil {
		return nil, fmt.Errorf("parse startTime: %w", err)
	}

	matchID := models.CanonicalMatchID(home, away, startTime)
	bookmakerKey := "pinnacle"
	now := time.Now()

	match := &models.Match{
		ID:         matchID,
		Name:       fmt.Sprintf("%s vs %s", home, away),
		HomeTeam:   home,
		AwayTeam:   away,
		StartTime:  startTime,
		Sport:      "football",
		Tournament: rm.League.Name,
		Bookmaker:  "",
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Map matchupId -> standard event type (main + related children).
	matchupEventType := map[int64]models.StandardEventType{
		matchupID: models.StandardEventMainMatch,
	}

	foundByParent := false
	for _, r := range related {
		if r.ParentID == nil || *r.ParentID != matchupID {
			continue
		}
		if et, ok := inferStandardEventType(r); ok {
			matchupEventType[r.ID] = et
			foundByParent = true
		}
	}
	// Fallback: some guest API responses may not include parentId; in that case
	// treat other related matchup entities as candidates based on units/name.
	if !foundByParent {
		for _, r := range related {
			if r.ID == matchupID {
				continue
			}
			if et, ok := inferStandardEventType(r); ok {
				matchupEventType[r.ID] = et
			}
		}
	}

	eventsByType := map[models.StandardEventType]*models.Event{}
	getOrCreate := func(et models.StandardEventType) *models.Event {
		if ev, ok := eventsByType[et]; ok {
			return ev
		}
		ev := &models.Event{
			ID:         matchID + "_" + bookmakerKey + "_" + string(et),
			MatchID:    matchID,
			EventType:  string(et),
			MarketName: models.GetMarketName(et),
			Bookmaker:  "Pinnacle",
			Outcomes:   []models.Outcome{},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		eventsByType[et] = ev
		return ev
	}

	// Period 0 only (full match pre-match odds)
	marketsByMatchupID := make(map[int64][]Market)
	alternateMarketsByMatchupID := make(map[int64][]Market) // Fallback for alternate markets
	for _, mkt := range markets {
		// Only Period 0 (full match pre-match)
		if mkt.Period != 0 || mkt.Status != "open" {
			continue
		}
		if mkt.IsAlternate {
			alternateMarketsByMatchupID[mkt.MatchupID] = append(alternateMarketsByMatchupID[mkt.MatchupID], mkt)
		} else {
			marketsByMatchupID[mkt.MatchupID] = append(marketsByMatchupID[mkt.MatchupID], mkt)
		}
	}
	// Use alternate markets as fallback if no regular markets available
	for muID, altMarkets := range alternateMarketsByMatchupID {
		if len(marketsByMatchupID[muID]) == 0 && len(altMarkets) > 0 {
			marketsByMatchupID[muID] = altMarkets
		}
	}

	// Process regular markets first
	for _, mkt := range markets {
		// Only Period 0 (full match pre-match)
		if mkt.Period != 0 || mkt.Status != "open" {
			continue
		}
		// Skip alternate markets for now - we'll use them as fallback
		if mkt.IsAlternate {
			continue
		}
		et, ok := matchupEventType[mkt.MatchupID]
		if !ok {
			continue
		}
		ev := getOrCreate(et)
		appendMarketOutcomes(ev, mkt)
	}

	// If no events were created or events have no outcomes, try alternate markets as fallback
	hasOutcomes := false
	for _, ev := range eventsByType {
		if len(ev.Outcomes) > 0 {
			hasOutcomes = true
			break
		}
	}
	if !hasOutcomes {
		for _, mkt := range markets {
			if mkt.Period != 0 || mkt.Status != "open" {
				continue
			}
			if !mkt.IsAlternate {
				continue
			}
			et, ok := matchupEventType[mkt.MatchupID]
			if !ok {
				continue
			}
			ev := getOrCreate(et)
			appendMarketOutcomes(ev, mkt)
		}
	}

	// Emit events in stable order (main_match first).
	ordered := []models.StandardEventType{
		models.StandardEventMainMatch,
		models.StandardEventCorners,
		models.StandardEventYellowCards,
		models.StandardEventFouls,
		models.StandardEventShotsOnTarget,
		models.StandardEventOffsides,
		models.StandardEventThrowIns,
	}
	seen := map[models.StandardEventType]bool{}
	for _, et := range ordered {
		seen[et] = true
		if ev := eventsByType[et]; ev != nil && len(ev.Outcomes) > 0 {
			match.Events = append(match.Events, *ev)
		}
	}
	// Any extra event types (future mappings) sorted by name for determinism.
	var rest []string
	restByName := map[string]models.StandardEventType{}
	for et := range eventsByType {
		if seen[et] {
			continue
		}
		name := string(et)
		rest = append(rest, name)
		restByName[name] = et
	}
	sort.Strings(rest)
	for _, name := range rest {
		et := restByName[name]
		if ev := eventsByType[et]; ev != nil && len(ev.Outcomes) > 0 {
			match.Events = append(match.Events, *ev)
		}
	}

	return match, nil
}

func inferStandardEventType(r RelatedMatchup) (models.StandardEventType, bool) {
	// Pinnacle related matchup can encode statistical market via units="Corners" (etc)
	// or via league name. We try both.
	u := strings.ToLower(strings.TrimSpace(r.Units))
	ln := strings.ToLower(strings.TrimSpace(r.League.Name))
	s := u
	if s == "" {
		s = ln
	}

	switch {
	case strings.Contains(s, "corner"):
		return models.StandardEventCorners, true
	case strings.Contains(s, "booking"):
		// Pinnacle uses "Bookings" for cards market; we standardize into yellow_cards for now.
		return models.StandardEventYellowCards, true
	case strings.Contains(s, "yellow"):
		return models.StandardEventYellowCards, true
	case strings.Contains(s, "card"):
		return models.StandardEventYellowCards, true
	case strings.Contains(s, "foul"):
		return models.StandardEventFouls, true
	case strings.Contains(s, "shot") && strings.Contains(s, "target"):
		return models.StandardEventShotsOnTarget, true
	case strings.Contains(s, "offside"):
		return models.StandardEventOffsides, true
	case strings.Contains(s, "throw"):
		return models.StandardEventThrowIns, true
	default:
		return "", false
	}
}

func appendMarketOutcomes(ev *models.Event, m Market) {
	switch m.Type {
	case "moneyline":
		for _, pr := range m.Prices {
			odds := americanToDecimal(pr.Price)
			switch pr.Designation {
			case "home":
				ev.Outcomes = append(ev.Outcomes, newOutcome(ev.ID, "home_win", "", odds))
			case "away":
				ev.Outcomes = append(ev.Outcomes, newOutcome(ev.ID, "away_win", "", odds))
			case "draw":
				ev.Outcomes = append(ev.Outcomes, newOutcome(ev.ID, "draw", "", odds))
			}
		}
	case "total":
		for _, pr := range m.Prices {
			if pr.Points == nil {
				continue
			}
			line := formatLine(*pr.Points)
			odds := americanToDecimal(pr.Price)
			switch pr.Designation {
			case "over":
				ev.Outcomes = append(ev.Outcomes, newOutcome(ev.ID, "total_over", line, odds))
			case "under":
				ev.Outcomes = append(ev.Outcomes, newOutcome(ev.ID, "total_under", line, odds))
			}
		}
	case "spread":
		// In Pinnacle spread points are symmetric: home is typically -points, away is +points.
		for _, pr := range m.Prices {
			if pr.Points == nil {
				continue
			}
			odds := americanToDecimal(pr.Price)
			switch pr.Designation {
			case "home":
				ev.Outcomes = append(ev.Outcomes, newOutcome(ev.ID, "handicap_home", formatSignedLine(-*pr.Points), odds))
			case "away":
				ev.Outcomes = append(ev.Outcomes, newOutcome(ev.ID, "handicap_away", formatSignedLine(+*pr.Points), odds))
			}
		}
	}
}

func logRelatedMapping(matchupID int64, related []RelatedMatchup) {
	// Debug helper: print how related matchups map to StandardEventType.
	// This is critical for validating that Pinnacle "units"/league names map correctly.
	type row struct {
		id       int64
		parentID *int64
		units    string
		league   string
		mapped   string
	}
	var rows []row
	for _, r := range related {
		if r.ID == matchupID {
			continue
		}
		mapped := "SKIP"
		if et, ok := inferStandardEventType(r); ok {
			mapped = string(et)
		}
		rows = append(rows, row{
			id:       r.ID,
			parentID: r.ParentID,
			units:    r.Units,
			league:   r.League.Name,
			mapped:   mapped,
		})
	}
	if len(rows) == 0 {
		return
	}
	fmt.Printf("Pinnacle: related mapping for main matchup=%d (showing %d related)\n", matchupID, len(rows))
	for _, it := range rows {
		pid := "nil"
		if it.parentID != nil {
			pid = strconv.FormatInt(*it.parentID, 10)
		}
		fmt.Printf("  - related matchup=%d parentId=%s units=%q league=%q => %s\n", it.id, pid, it.units, it.league, it.mapped)
	}
}

func americanToDecimal(american int) float64 {
	if american == 0 {
		return 0
	}
	if american > 0 {
		return 1 + float64(american)/100.0
	}
	return 1 + 100.0/float64(-american)
}

func formatLine(points float64) string {
	// For totals lines we keep unsigned.
	return strconv.FormatFloat(points, 'f', -1, 64)
}

func formatSignedLine(points float64) string {
	if points == 0 {
		return "0"
	}
	if points > 0 {
		return "+" + strconv.FormatFloat(points, 'f', -1, 64)
	}
	return strconv.FormatFloat(points, 'f', -1, 64)
}

func newOutcome(eventID, outcomeType, param string, odds float64) models.Outcome {
	now := time.Now()
	id := fmt.Sprintf("%s_%s_%s", eventID, outcomeType, param)
	return models.Outcome{
		ID:          id,
		EventID:     eventID,
		OutcomeType: outcomeType,
		Parameter:   param,
		Odds:        odds,
		Bookmaker:   "Pinnacle",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
