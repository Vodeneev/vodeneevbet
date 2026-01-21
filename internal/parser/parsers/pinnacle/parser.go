package pinnacle

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

type Parser struct {
	cfg     *config.Config
	client  *Client
	storage interfaces.Storage
}

func NewParser(cfg *config.Config) *Parser {
	// Create YDB client (optional).
	base, err := storage.NewYDBClient(&cfg.YDB)
	var st interfaces.Storage
	if err == nil {
		st = storage.NewBatchYDBClient(base)
	}

	baseURL := cfg.Parser.Pinnacle.BaseURL
	if baseURL == "" {
		baseURL = "https://guest.api.arcadia.pinnacle.com"
	}

	client := NewClient(baseURL, cfg.Parser.Pinnacle.APIKey, cfg.Parser.Pinnacle.DeviceUUID, cfg.Parser.Timeout)

	return &Parser{
		cfg:     cfg,
		client:  client,
		storage: st,
	}
}

func (p *Parser) Start(ctx context.Context) error {
	interval := p.cfg.Parser.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	runOnce := func() {
		// If matchup_ids are provided, run targeted mode.
		if len(p.cfg.Parser.Pinnacle.MatchupIDs) > 0 {
			for _, matchupID := range p.cfg.Parser.Pinnacle.MatchupIDs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if err := p.processMatchup(ctx, matchupID); err != nil {
					fmt.Printf("Pinnacle: failed to process matchup %d: %v\n", matchupID, err)
				}
			}
			return
		}

		// Otherwise, discover and process all matchups for relevant sports.
		if err := p.processAll(ctx); err != nil {
			fmt.Printf("Pinnacle: failed to process all matchups: %v\n", err)
		}
	}

	runOnce()
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			runOnce()
		}
	}
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
	now := time.Now().UTC()
	maxStart := now.Add(48 * time.Hour)

	for _, sportName := range targetSportNames {
		sportID, ok := nameToID[sportName]
		if !ok || sportID == 0 {
			continue
		}

		matchups, err := p.client.GetSportMatchups(sportID)
		if err != nil {
			return err
		}
		markets, err := p.client.GetSportStraightMarkets(sportID)
		if err != nil {
			return err
		}

		// Filter markets upfront.
		marketsByMatchup := map[int64][]Market{}
		for _, m := range markets {
			if m.IsAlternate || m.Status != "open" || m.Period != 0 {
				continue
			}
			marketsByMatchup[m.MatchupID] = append(marketsByMatchup[m.MatchupID], m)
		}

		// Group matchups by main matchup (parentId or self).
		group := map[int64][]RelatedMatchup{}
		filteredByTime := 0
		for _, mu := range matchups {
			st, err := time.Parse(time.RFC3339, mu.StartTime)
			if err != nil {
				continue
			}
			st = st.UTC()
			
			// Log all matchups for debugging (especially looking for Bayern vs Union Saint-Gilloise)
			homeTeam, awayTeam := "", ""
			for _, p := range mu.Participants {
				if p.Alignment == "home" {
					homeTeam = p.Name
				} else if p.Alignment == "away" {
					awayTeam = p.Name
				}
			}
			matchName := fmt.Sprintf("%s vs %s", homeTeam, awayTeam)
			if strings.Contains(strings.ToLower(matchName), "bayern") || 
			   strings.Contains(strings.ToLower(matchName), "бавария") ||
			   strings.Contains(strings.ToLower(matchName), "union") ||
			   strings.Contains(strings.ToLower(matchName), "saint-gilloise") ||
			   strings.Contains(strings.ToLower(matchName), "gilloise") {
				fmt.Printf("Pinnacle DEBUG: Found potential match: %s (ID=%d, StartTime=%s, League=%s)\n", 
					matchName, mu.ID, mu.StartTime, mu.League.Name)
			}
			
			if st.Before(now.Add(-2*time.Hour)) || st.After(maxStart) {
				if strings.Contains(strings.ToLower(matchName), "bayern") || 
				   strings.Contains(strings.ToLower(matchName), "бавария") ||
				   strings.Contains(strings.ToLower(matchName), "union") ||
				   strings.Contains(strings.ToLower(matchName), "saint-gilloise") {
					fmt.Printf("Pinnacle DEBUG: Match %s filtered by time (start=%s, now=%s, window=[%s, %s])\n",
						matchName, st.Format(time.RFC3339), now.Format(time.RFC3339),
						now.Add(-2*time.Hour).Format(time.RFC3339), maxStart.Format(time.RFC3339))
				}
				filteredByTime++
				continue
			}
			mainID := mu.ID
			if mu.ParentID != nil && *mu.ParentID > 0 {
				mainID = *mu.ParentID
			}
			group[mainID] = append(group[mainID], mu)
		}
		if filteredByTime > 0 {
			fmt.Printf("Pinnacle: filtered %d matchups by time window\n", filteredByTime)
		}

		fmt.Printf("Pinnacle: processing %d main matchups for sport %s\n", len(group), sportName)

		// Process each main matchup as a match.
		for mainID, related := range group {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			// Collect markets for all related matchups.
			var relMarkets []Market
			for _, mu := range related {
				relMarkets = append(relMarkets, marketsByMatchup[mu.ID]...)
			}
			if len(relMarkets) == 0 {
				// Log if this might be the Bayern match
				for _, mu := range related {
					homeTeam, awayTeam := "", ""
					for _, p := range mu.Participants {
						if p.Alignment == "home" {
							homeTeam = p.Name
						} else if p.Alignment == "away" {
							awayTeam = p.Name
						}
					}
					matchName := fmt.Sprintf("%s vs %s", homeTeam, awayTeam)
					if strings.Contains(strings.ToLower(matchName), "bayern") || 
					   strings.Contains(strings.ToLower(matchName), "бавария") ||
					   strings.Contains(strings.ToLower(matchName), "union") ||
					   strings.Contains(strings.ToLower(matchName), "saint-gilloise") {
						fmt.Printf("Pinnacle DEBUG: Match %s (ID=%d) skipped - no markets available\n", matchName, mainID)
					}
				}
				fmt.Printf("Pinnacle: skipping matchup %d (no markets)\n", mainID)
				continue
			}

			m, err := buildMatchFromPinnacle(mainID, related, relMarkets)
			if err != nil || m == nil {
				if err != nil {
					fmt.Printf("Pinnacle: failed to build match for matchup %d: %v\n", mainID, err)
				}
				continue
			}

			fmt.Printf("Pinnacle: built match %s (%s vs %s), events=%d\n", m.ID, m.HomeTeam, m.AwayTeam, len(m.Events))

			if p.storage != nil {
				_ = p.storage.StoreMatch(ctx, m)
			}
			
			// Add match to in-memory store for fast API access
			if m != nil {
				health.AddMatch(m)
				fmt.Printf("Pinnacle: added match %s to in-memory store with %d events\n", m.ID, len(m.Events))
			}
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

	fmt.Printf("Pinnacle: built match %s (%s vs %s), events=%d\n", m.ID, m.HomeTeam, m.AwayTeam, len(m.Events))

	// Storage is optional.
	if p.storage == nil {
		// Still add to in-memory store even if YDB storage is not available
		if m != nil {
			health.AddMatch(m)
		}
		return nil
	}
	
	// Store in YDB and add to in-memory store
	err = p.storage.StoreMatch(ctx, m)
	if err == nil && m != nil {
		health.AddMatch(m)
	}
	
	return err
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
	
	// Debug: log all related matchups for Bayern match
	homeTeam, awayTeam := "", ""
	for _, p := range rm.Participants {
		if p.Alignment == "home" {
			homeTeam = p.Name
		} else if p.Alignment == "away" {
			awayTeam = p.Name
		}
	}
	isBayernMatch := strings.Contains(strings.ToLower(homeTeam), "bayern") || 
	                 strings.Contains(strings.ToLower(awayTeam), "union")
	
	if isBayernMatch {
		fmt.Printf("Pinnacle DEBUG: Processing match %s vs %s (mainID=%d), found %d related matchups\n",
			homeTeam, awayTeam, matchupID, len(related))
		for _, r := range related {
			parentIDStr := "nil"
			if r.ParentID != nil {
				parentIDStr = strconv.FormatInt(*r.ParentID, 10)
			}
			et, ok := inferStandardEventType(r)
			etStr := "UNKNOWN"
			if ok {
				etStr = string(et)
			}
			fmt.Printf("Pinnacle DEBUG:   Related matchup ID=%d, ParentID=%s, Units=%q, League=%q => EventType=%s\n",
				r.ID, parentIDStr, r.Units, r.League.Name, etStr)
		}
	}
	
	foundByParent := false
	for _, r := range related {
		if r.ParentID == nil || *r.ParentID != matchupID {
			continue
		}
		if et, ok := inferStandardEventType(r); ok {
			matchupEventType[r.ID] = et
			foundByParent = true
			if isBayernMatch {
				fmt.Printf("Pinnacle DEBUG:   Mapped related matchup ID=%d (ParentID=%d) => %s\n",
					r.ID, matchupID, string(et))
			}
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
				if isBayernMatch {
					fmt.Printf("Pinnacle DEBUG:   Mapped related matchup ID=%d (fallback, no ParentID) => %s\n",
						r.ID, string(et))
				}
			}
		}
	}
	
	if isBayernMatch {
		fmt.Printf("Pinnacle DEBUG:   Total mapped event types: %d\n", len(matchupEventType))
		for muID, et := range matchupEventType {
			fmt.Printf("Pinnacle DEBUG:     MatchupID=%d => %s\n", muID, string(et))
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

	// Period 0 only for now.
	marketsByMatchupID := make(map[int64][]Market)
	for _, mkt := range markets {
		if mkt.Period != 0 || mkt.Status != "open" {
			continue
		}
		marketsByMatchupID[mkt.MatchupID] = append(marketsByMatchupID[mkt.MatchupID], mkt)
	}
	
	if isBayernMatch {
		fmt.Printf("Pinnacle DEBUG:   Found markets for %d matchup IDs\n", len(marketsByMatchupID))
		for muID, mkts := range marketsByMatchupID {
			et, ok := matchupEventType[muID]
			etStr := "UNMAPPED"
			if ok {
				etStr = string(et)
			}
			fmt.Printf("Pinnacle DEBUG:     MatchupID=%d => %d markets, EventType=%s\n", muID, len(mkts), etStr)
		}
	}
	
	for _, mkt := range markets {
		if mkt.Period != 0 || mkt.Status != "open" {
			continue
		}
		et, ok := matchupEventType[mkt.MatchupID]
		if !ok {
			if isBayernMatch {
				fmt.Printf("Pinnacle DEBUG:   Skipping market for unmapped MatchupID=%d (Type=%s)\n",
					mkt.MatchupID, mkt.Type)
			}
			continue
		}
		ev := getOrCreate(et)
		appendMarketOutcomes(ev, mkt)
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

