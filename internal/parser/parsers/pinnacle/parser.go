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
	// For regular matchups, only include future matches (upcoming).
	// Live matches are handled separately via GetSportLiveMatchups which filters by isLive=true.
	now := time.Now().UTC()
	maxStart := now.Add(48 * time.Hour)
	minStart := now // Only include matches that haven't started yet (for regular matchups)

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

		// Get live matchups asynchronously - these are the ONLY source of truth for live matches
		liveMatchupsCh := make(chan []RelatedMatchup, 1)
		go func() {
			logMsg := fmt.Sprintf("Pinnacle: Fetching live matchups from /0.1/sports/%d/matchups/live endpoint\n", sportID)
			fmt.Print(logMsg)
			logToFile(logMsg)
			liveMatchups, err := p.client.GetSportLiveMatchups(sportID)
			if err != nil {
				logMsg = fmt.Sprintf("Pinnacle: Failed to fetch live matchups: %v\n", err)
				fmt.Print(logMsg)
				logToFile(logMsg)
				liveMatchupsCh <- nil
				return
			}
			logMsg = fmt.Sprintf("Pinnacle: Found %d live matchups from live endpoint\n", len(liveMatchups))
			fmt.Print(logMsg)
			logToFile(logMsg)
			liveMatchupsCh <- liveMatchups
		}()

		markets, err := p.client.GetSportStraightMarkets(sportID)
		if err != nil {
			return err
		}

		// Wait for live matchups - process them separately with async market fetching
		var liveMatchups []RelatedMatchup
		select {
		case liveMatchups = <-liveMatchupsCh:
			// Live matchups will be processed separately below
		case <-ctx.Done():
			return ctx.Err()
		}

		// Track live matchup IDs to avoid processing them in regular flow
		liveMatchupIDs := make(map[int64]bool)
		for _, mu := range liveMatchups {
			liveMatchupIDs[mu.ID] = true
			if mu.ParentID != nil {
				liveMatchupIDs[*mu.ParentID] = true
			}
		}

		// Filter markets upfront.
		// Allow Period 0 (full match) and Period -1 (live/current period) for live matches
		marketsByMatchup := map[int64][]Market{}
		filteredStats := map[int64]map[string]int{} // matchupID -> reason -> count
		for _, m := range markets {
			reason := ""
			if m.IsAlternate {
				reason = "IsAlternate"
			} else if m.Status != "open" {
				reason = fmt.Sprintf("Status=%s", m.Status)
			} else if m.Period != 0 && m.Period != -1 {
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

		// Group matchups by main matchup (parentId or self).
		// Skip live matchups - they will be processed separately
		group := map[int64][]RelatedMatchup{}
		filteredByTime := 0
		for _, mu := range matchups {
			// Skip if this is a live matchup - process separately
			if liveMatchupIDs[mu.ID] || (mu.ParentID != nil && liveMatchupIDs[*mu.ParentID]) {
				continue
			}

			st, err := time.Parse(time.RFC3339, mu.StartTime)
			if err != nil {
				continue
			}
			st = st.UTC()

			// Include only matches that haven't started yet (up to 48 hours in the future).
			// Live matches are handled separately via GetSportLiveMatchups which filters by isLive=true.
			// This ensures we don't include already finished matches in the regular matchups list.
			if st.Before(minStart) || st.After(maxStart) {
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

			// Collect markets for all related matchups.
			var relMarkets []Market
			var alternateMarkets []Market // Fallback: alternate markets if no regular markets
			for _, mu := range related {
				relMarkets = append(relMarkets, marketsByMatchup[mu.ID]...)
				// Also collect alternate markets as fallback
				for _, m := range markets {
					if m.MatchupID == mu.ID && m.IsAlternate && m.Status == "open" && (m.Period == 0 || m.Period == -1) {
						alternateMarkets = append(alternateMarkets, m)
					}
				}
			}

			// For live matchups, try to get markets directly if not found in general markets
			if len(relMarkets) == 0 && len(alternateMarkets) == 0 {
				// Try to get markets directly for the main matchup (useful for live matches)
				directMarkets, err := p.client.GetRelatedStraightMarkets(mainID)
				if err == nil && len(directMarkets) > 0 {
					// Filter to only open markets with Period 0 or -1
					for _, m := range directMarkets {
						if m.Status == "open" && (m.Period == 0 || m.Period == -1) && !m.IsAlternate {
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

			// Add match to in-memory store for fast API access (primary storage)
			// YDB is not used - data is served directly from memory
			health.AddMatch(m)
		}

		// Process live matchups separately - no time check, only presence in live endpoint matters
		// Fetch markets asynchronously for each live matchup
		if len(liveMatchups) > 0 {
			var logMsg string
			logMsg = fmt.Sprintf("Pinnacle: Processing %d live matchups separately (no time check, only from live endpoint)\n", len(liveMatchups))
			fmt.Print(logMsg)
			logToFile(logMsg)
			// Group live matchups by main matchup (for building match structure)
			// But fetch markets using the actual live matchup ID
			liveGroup := map[int64][]RelatedMatchup{}
			for _, mu := range liveMatchups {
				mainID := mu.ID
				if mu.ParentID != nil && *mu.ParentID > 0 {
					mainID = *mu.ParentID
				}
				liveGroup[mainID] = append(liveGroup[mainID], mu)
			}
			logMsg = fmt.Sprintf("Pinnacle: Grouped into %d unique live match groups\n", len(liveGroup))
			fmt.Print(logMsg)
			logToFile(logMsg)

			// Process each live matchup with async market fetching
			type liveMatchResult struct {
				mainID    int64
				matchupID int64 // actual matchup ID used for fetching
				related   []RelatedMatchup
				markets   []Market
				err       error
			}
			resultsCh := make(chan liveMatchResult, len(liveMatchups))

			// Fetch markets asynchronously for each live matchup using its actual ID
			for _, mu := range liveMatchups {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				// For live matches, try both live matchup ID and parentID to find which has open markets
				// We need to check which one actually returns open live markets with current odds
				matchupID := mu.ID
				// Determine mainID for grouping
				mainID := matchupID
				if mu.ParentID != nil && *mu.ParentID > 0 {
					mainID = *mu.ParentID
				}
				related := liveGroup[mainID]

				go func(mID int64, matchID int64, parentID *int64, rel []RelatedMatchup) {
					// Try live matchup ID first
					logMsg := fmt.Sprintf("Pinnacle: Trying to fetch markets for live matchup ID %d\n", matchID)
					fmt.Print(logMsg)
					logToFile(logMsg)
					liveMarkets, err := p.client.GetRelatedStraightMarkets(matchID)
					if err != nil {
						logMsg = fmt.Sprintf("Pinnacle: Failed to fetch markets for live matchup %d: %v\n", matchID, err)
						fmt.Print(logMsg)
						logToFile(logMsg)
					}
					
					// Filter to only open markets with Period 0 or -1
					filtered := make([]Market, 0, len(liveMarkets))
					for _, m := range liveMarkets {
						if m.Status == "open" && (m.Period == 0 || m.Period == -1) && !m.IsAlternate {
							filtered = append(filtered, m)
						}
					}
					logMsg = fmt.Sprintf("Pinnacle: Live matchup ID %d returned %d open markets (from %d total)\n", matchID, len(filtered), len(liveMarkets))
					fmt.Print(logMsg)
					logToFile(logMsg)
					
					// If no open markets from live matchup ID, try parentID
					if len(filtered) == 0 && parentID != nil && *parentID > 0 {
						logMsg = fmt.Sprintf("Pinnacle: No open markets from live matchup %d, trying parentID %d\n", matchID, *parentID)
						fmt.Print(logMsg)
						logToFile(logMsg)
						parentMarkets, err := p.client.GetRelatedStraightMarkets(*parentID)
						if err == nil {
							parentFiltered := make([]Market, 0, len(parentMarkets))
							for _, m := range parentMarkets {
								if m.Status == "open" && (m.Period == 0 || m.Period == -1) && !m.IsAlternate {
									parentFiltered = append(parentFiltered, m)
								}
							}
							logMsg = fmt.Sprintf("Pinnacle: ParentID %d returned %d open markets (from %d total)\n", *parentID, len(parentFiltered), len(parentMarkets))
							fmt.Print(logMsg)
							logToFile(logMsg)
							if len(parentFiltered) > 0 {
								filtered = parentFiltered
								logMsg = fmt.Sprintf("Pinnacle: Using parentID %d markets (has %d open markets vs 0 from live matchup %d)\n", *parentID, len(filtered), matchID)
								fmt.Print(logMsg)
								logToFile(logMsg)
							}
						}
					}
					
					resultsCh <- liveMatchResult{mainID: mID, matchupID: matchID, related: rel, markets: filtered, err: nil}
				}(mainID, matchupID, mu.ParentID, related)
			}

			// Collect results from async market fetches
			// Group by match ID to avoid duplicate updates and use best matchup ID with markets
			matchesByID := make(map[string]liveMatchResult) // key: match ID from buildMatchFromPinnacle
			for i := 0; i < len(liveMatchups); i++ {
				select {
				case result := <-resultsCh:
					if result.err != nil || len(result.markets) == 0 {
						if result.err != nil {
							logMsg = fmt.Sprintf("Pinnacle: Skipping live matchup %d (matchupID: %d): error=%v\n", result.mainID, result.matchupID, result.err)
							fmt.Print(logMsg)
							logToFile(logMsg)
						} else {
							logMsg = fmt.Sprintf("Pinnacle: Skipping live matchup %d (matchupID: %d): no open markets found\n", result.mainID, result.matchupID)
							fmt.Print(logMsg)
							logToFile(logMsg)
						}
						continue
					}
					// Build match to get its ID
					m, err := buildMatchFromPinnacle(result.mainID, result.related, result.markets)
					if err != nil || m == nil {
						logMsg = fmt.Sprintf("Pinnacle: Failed to build match from live matchup %d (matchupID: %d): err=%v\n", result.mainID, result.matchupID, err)
						fmt.Print(logMsg)
						logToFile(logMsg)
						continue
					}
					// Use match with most markets if we have multiple matchup IDs for same match
					if existing, exists := matchesByID[m.ID]; exists {
						if len(result.markets) > len(existing.markets) {
							logMsg = fmt.Sprintf("Pinnacle: Replacing match %s: using matchupID %d (%d markets) instead of %d (%d markets)\n", m.Name, result.matchupID, len(result.markets), existing.matchupID, len(existing.markets))
							fmt.Print(logMsg)
							logToFile(logMsg)
							matchesByID[m.ID] = result
						} else {
							logMsg = fmt.Sprintf("Pinnacle: Keeping existing match %s: matchupID %d has %d markets (new matchupID %d has %d)\n", m.Name, existing.matchupID, len(existing.markets), result.matchupID, len(result.markets))
							fmt.Print(logMsg)
							logToFile(logMsg)
						}
					} else {
						matchesByID[m.ID] = result
					}
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			// Add all unique matches to store
			liveMatchesAdded := 0
			for matchID, result := range matchesByID {
				m, err := buildMatchFromPinnacle(result.mainID, result.related, result.markets)
				if err != nil || m == nil {
					logMsg = fmt.Sprintf("Pinnacle: Failed to build match %s from result: err=%v\n", matchID, err)
					fmt.Print(logMsg)
					logToFile(logMsg)
					continue
				}
				// Add match to in-memory store - it will be available in /matches endpoint
				health.AddMatch(m)
				liveMatchesAdded++
				logMsg = fmt.Sprintf("Pinnacle: Successfully added live match: %s (matchupID: %d, mainID: %d, %d markets)\n", m.Name, result.matchupID, result.mainID, len(result.markets))
				fmt.Print(logMsg)
				logToFile(logMsg)
			}
			logMsg = fmt.Sprintf("Pinnacle: Completed processing live matchups: %d unique matches added out of %d total matchup IDs\n", liveMatchesAdded, len(liveMatchups))
			fmt.Print(logMsg)
			logToFile(logMsg)
		} else {
			logMsg := "Pinnacle: No live matchups found in live endpoint\n"
			fmt.Print(logMsg)
			logToFile(logMsg)
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

	// Period 0 only for now (full match), but allow Period -1 for live matches
	marketsByMatchupID := make(map[int64][]Market)
	alternateMarketsByMatchupID := make(map[int64][]Market) // Fallback for alternate markets
	for _, mkt := range markets {
		// Allow Period 0 (full match) and Period -1 (live/current period)
		// Period -1 is used by Pinnacle for live matches
		if (mkt.Period != 0 && mkt.Period != -1) || mkt.Status != "open" {
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
		// Allow Period 0 (full match) and Period -1 (live/current period)
		if (mkt.Period != 0 && mkt.Period != -1) || mkt.Status != "open" {
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
			if (mkt.Period != 0 && mkt.Period != -1) || mkt.Status != "open" {
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
