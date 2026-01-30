package pinnacle888

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

var runOnceMu sync.Mutex

type Parser struct {
	cfg     *config.Config
	client  *Client
	storage interfaces.Storage
}

func NewParser(cfg *config.Config) *Parser {
	// Data is served directly from in-memory store

	baseURL := cfg.Parser.Pinnacle888.BaseURL
	if baseURL == "" {
		baseURL = "https://guest.api.arcadia.pinnacle.com"
	}

	mirrorURL := cfg.Parser.Pinnacle888.MirrorURL

	client := NewClient(baseURL, mirrorURL, cfg.Parser.Pinnacle888.APIKey, cfg.Parser.Pinnacle888.DeviceUUID, cfg.Parser.Timeout, cfg.Parser.Pinnacle888.ProxyList)

	return &Parser{
		cfg:     cfg,
		client:  client,
		storage: nil, // No external storage - data served from memory
	}
}

// runOnce performs a single parsing run. Only one run executes at a time to avoid
// overlapping runs (periodic tick every 10s can start a new run before previous finishes),
// which would spawn thousands of goroutines and fill disk (logs, Chrome temp dirs).
func (p *Parser) runOnce(ctx context.Context) error {
	runOnceMu.Lock()
	defer runOnceMu.Unlock()
	start := time.Now()
	defer func() { slog.Info("Pinnacle888: runOnce finished", "duration", time.Since(start)) }()

	// Resolve mirror once at the start of each run; cache is reused. On error we don't re-resolve, next iteration will retry.
	if p.cfg.Parser.Pinnacle888.OddsURL != "" && (p.cfg.Parser.Pinnacle888.IncludeLive || p.cfg.Parser.Pinnacle888.IncludePrematch) {
		if err := p.client.ensureResolved(); err != nil {
			slog.Warn("Pinnacle888: mirror resolve failed at run start, will retry next iteration", "error", err, "error_msg", err.Error())
			// continue anyway — requests will fail; next runOnce() will try resolve again
		}
	}

	// If matchup_ids are provided, run targeted mode.
	if len(p.cfg.Parser.Pinnacle888.MatchupIDs) > 0 {
		for _, matchupID := range p.cfg.Parser.Pinnacle888.MatchupIDs {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			if err := p.processMatchup(ctx, matchupID); err != nil {
				slog.Error("Failed to process matchup", "matchup_id", matchupID, "error", err)
			}
		}
		return nil
	}

	// Process live and pre-match matches asynchronously if configured
	var wg sync.WaitGroup
	var liveMatches []*models.Match
	var prematchMatches []*models.Match
	var liveErr, prematchErr error

	slog.Info("Pinnacle888: runOnce started", "include_live", p.cfg.Parser.Pinnacle888.IncludeLive, "include_prematch", p.cfg.Parser.Pinnacle888.IncludePrematch, "odds_url_set", p.cfg.Parser.Pinnacle888.OddsURL != "")

	// Fetch live matches asynchronously
	if p.cfg.Parser.Pinnacle888.IncludeLive && p.cfg.Parser.Pinnacle888.OddsURL != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("Pinnacle888: starting live matches processing")
			matches, err := p.processLiveMatches(ctx)
			if err != nil {
				liveErr = err
				slog.Error("Pinnacle888: failed to process live matches", "error", err, "error_msg", err.Error())
			} else {
				liveMatches = matches
				slog.Info("Pinnacle888: live matches processed", "count", len(matches))
			}
		}()
	}

	// Fetch pre-match matches asynchronously
	if p.cfg.Parser.Pinnacle888.IncludePrematch && p.cfg.Parser.Pinnacle888.OddsURL != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slog.Info("Pinnacle888: starting pre-match matches processing")
			matches, err := p.processLineMatches(ctx)
			if err != nil {
				prematchErr = err
				slog.Error("Pinnacle888: failed to process pre-match matches", "error", err, "error_msg", err.Error())
			} else {
				prematchMatches = matches
				slog.Info("Pinnacle888: pre-match matches processed", "count", len(matches))
			}
		}()
	}

	// Wait for both to complete
	wg.Wait()

	// Merge matches: combine live and pre-match matches, preferring live data when duplicates exist
	mergedMatches := p.mergeMatches(liveMatches, prematchMatches)

	// Add merged matches to in-memory store
	for _, match := range mergedMatches {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		health.AddMatch(match)
	}

	// If there were errors but we still got some matches, log but don't fail
	if liveErr != nil || prematchErr != nil {
		slog.Warn("Some matches processed successfully despite errors", "live_count", len(liveMatches), "prematch_count", len(prematchMatches), "merged_count", len(mergedMatches))
	}

	// Otherwise, discover and process all matchups for relevant sports (legacy mode)
	if !p.cfg.Parser.Pinnacle888.IncludeLive && !p.cfg.Parser.Pinnacle888.IncludePrematch {
		if err := p.processAll(ctx); err != nil {
			slog.Error("Failed to process all matchups", "error", err)
			return err
		}
	}

	return nil
}

func (p *Parser) Start(ctx context.Context) error {
	slog.Info("Starting Pinnacle888 parser (background mode - periodic parsing runs automatically)...")

	// Run once at startup to have initial data
	if err := p.runOnce(ctx); err != nil {
		return err
	}

	// Wait for context cancellation (periodic parsing is handled by main.go)
	<-ctx.Done()
	return nil
}

// ParseOnce triggers a single parsing run (periodic parsing)
func (p *Parser) ParseOnce(ctx context.Context) error {
	return p.runOnce(ctx)
}

func (p *Parser) Stop() error { return nil }
func (p *Parser) GetName() string {
	return "Pinnacle888"
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
			// Exclude live matches unless include_live is enabled
			if !st.After(now) || st.After(maxStart) {
				if !st.After(now) {
					// Live match - include only if configured
					if !p.cfg.Parser.Pinnacle888.IncludeLive {
						slog.Debug("Pinnacle888: filtered live match", "matchup_id", mu.ID, "start", st.Format(time.RFC3339), "now", now.Format(time.RFC3339))
						filteredByTime++
						continue
					}
					// Include live match
				} else {
					// Future match beyond window
					filteredByTime++
					continue
				}
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

			// Double-check: skip live matches unless include_live is enabled
			// This is a safety check in case the time filter above missed something
			if !m.StartTime.IsZero() {
				matchStartTime := m.StartTime.UTC()
				checkNow := time.Now().UTC()
				if !matchStartTime.After(checkNow) {
					// Match has already started
					if !p.cfg.Parser.Pinnacle888.IncludeLive {
						// Skip live match
						slog.Debug("Pinnacle888: double-check filtered live match", "match_id", m.ID, "start", matchStartTime.Format(time.RFC3339), "now", checkNow.Format(time.RFC3339))
						continue
					}
					// Include live match
				}
			}

			// Add match to in-memory store for fast API access (primary storage)
			// Data is served directly from memory
			health.AddMatch(m)
		}
	}

	return nil
}

// processLiveMatches processes live matches: leagues -> league odds (async) -> event odds (async)
func (p *Parser) processLiveMatches(ctx context.Context) ([]*models.Match, error) {
	return p.processOddsLeaguesFlow(ctx, true)
}

// processLineMatches processes pre-match matches: leagues -> league odds (async) -> event odds (async)
func (p *Parser) processLineMatches(ctx context.Context) ([]*models.Match, error) {
	return p.processOddsLeaguesFlow(ctx, false)
}

// processOddsLeaguesFlow: 1) get leagues 2) process leagues in batches (N concurrent); per league: GetLeagueOdds then events sequentially (GetEventOdds, no per-event goroutines).
func (p *Parser) processOddsLeaguesFlow(ctx context.Context, isLive bool) ([]*models.Match, error) {
	oddsURL := p.cfg.Parser.Pinnacle888.OddsURL
	if oddsURL == "" {
		return nil, fmt.Errorf("odds_url not configured")
	}
	sportID := int64(29) // Soccer

	slog.Info("Pinnacle888: starting leagues flow", "mode", map[bool]string{true: "live", false: "pre-match"}[isLive], "oddsURL", oddsURL)
	leagues, err := p.client.GetLeagues(oddsURL, sportID)
	if err != nil {
		slog.Error("Pinnacle888: failed to get leagues", "error", err)
		return nil, fmt.Errorf("get leagues: %w", err)
	}
	slog.Info("Pinnacle888: fetched leagues", "count", len(leagues))

	// Skip leagues with no events for efficiency
	var leaguesWithEvents []LeagueListItem
	for _, l := range leagues {
		if l.TotalEvents > 0 {
			leaguesWithEvents = append(leaguesWithEvents, l)
		}
	}
	slog.Info("Pinnacle888: filtering leagues with events", "total", len(leagues), "with_events", len(leaguesWithEvents))

	// Concurrency from config; events within a league are processed sequentially
	leagueWorkers := p.cfg.Parser.Pinnacle888.LeagueWorkers
	if leagueWorkers <= 0 {
		leagueWorkers = 5
	}
	leagueSem := make(chan struct{}, leagueWorkers)

	var matchesMu sync.Mutex
	var allMatches []*models.Match
	var wgLeagues sync.WaitGroup

	for _, league := range leaguesWithEvents {
		select {
		case <-ctx.Done():
			return allMatches, ctx.Err()
		default:
		}

		wgLeagues.Add(1)
		league := league
		go func() {
			defer wgLeagues.Done()
			leagueSem <- struct{}{}
			defer func() { <-leagueSem }()

			data, err := p.client.GetLeagueOdds(oddsURL, league.LeagueCode, sportID, isLive)
			if err != nil {
				slog.Debug("Pinnacle888: get league odds", "league", league.LeagueCode, "error", err)
				return
			}

			var leagueResp OddsResponse
			if err := json.Unmarshal(data, &leagueResp); err != nil {
				slog.Debug("Pinnacle888: parse league odds", "league", league.LeagueCode, "error", err)
				return
			}

			// Events sequentially: no goroutines per event to avoid CPU/disk load
			for _, lg := range leagueResp.Leagues {
				for _, ev := range lg.Events {
					select {
					case <-ctx.Done():
						return
					default:
					}
					eventData, err := p.client.GetEventOdds(oddsURL, ev.ID)
					if err != nil {
						slog.Debug("Pinnacle888: get event odds", "eventId", ev.ID, "error", err)
						continue
					}
					match, err := ParseEventOddsResponse(eventData)
					if err != nil || match == nil {
						continue
					}
					matchesMu.Lock()
					allMatches = append(allMatches, match)
					matchesMu.Unlock()
				}
			}
		}()
	}

	wgLeagues.Wait()

	liveLabel := "pre-match"
	if isLive {
		liveLabel = "live"
	}
	slog.Info("Pinnacle888: leagues flow finished", "mode", liveLabel, "matches", len(allMatches), "leagues_processed", len(leaguesWithEvents))
	return allMatches, nil
}

// mergeMatches merges live and pre-match matches, preferring live data when duplicates exist
// Duplicates are identified by CanonicalMatchID
func (p *Parser) mergeMatches(liveMatches, prematchMatches []*models.Match) []*models.Match {
	// Create a map to track matches by ID
	matchMap := make(map[string]*models.Match)

	// First, add all pre-match matches
	for _, match := range prematchMatches {
		matchMap[match.ID] = match
	}

	// Then, add/override with live matches (live takes precedence)
	for _, match := range liveMatches {
		// If match already exists, merge events (live events are more up-to-date)
		if existing, ok := matchMap[match.ID]; ok {
			// Merge events: prefer live events, but keep pre-match events that don't exist in live
			existing.Events = p.mergeEvents(existing.Events, match.Events)
			existing.UpdatedAt = time.Now() // Update timestamp
		} else {
			matchMap[match.ID] = match
		}
	}

	// Convert map to slice
	merged := make([]*models.Match, 0, len(matchMap))
	for _, match := range matchMap {
		merged = append(merged, match)
	}

	return merged
}

// mergeEvents merges two event slices, preferring events from the second slice (live)
func (p *Parser) mergeEvents(prematchEvents, liveEvents []models.Event) []models.Event {
	// Create a map of live events by EventType
	liveMap := make(map[string]models.Event)
	for _, event := range liveEvents {
		liveMap[event.EventType] = event
	}

	// Start with pre-match events
	merged := make([]models.Event, 0, len(prematchEvents)+len(liveEvents))
	seenTypes := make(map[string]bool)

	// Add live events first (they take precedence)
	for _, event := range liveEvents {
		merged = append(merged, event)
		seenTypes[event.EventType] = true
	}

	// Add pre-match events that don't exist in live
	for _, event := range prematchEvents {
		if !seenTypes[event.EventType] {
			merged = append(merged, event)
			seenTypes[event.EventType] = true
		}
	}

	return merged
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

	// Skip live matches unless include_live is enabled
	if !m.StartTime.IsZero() {
		matchStartTime := m.StartTime.UTC()
		checkNow := time.Now().UTC()
		if !matchStartTime.After(checkNow) {
			// Match has already started
			if !p.cfg.Parser.Pinnacle888.IncludeLive {
				// Skip live match
				return nil
			}
			// Include live match
		}
	}

	// Add match to in-memory store for fast API access (primary storage)
	// Data is served directly from memory
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
	bookmakerKey := "pinnacle888"
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
			Bookmaker:  "Pinnacle888",
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
		// In Pinnacle spread market:
		// Based on investigation: API returns Points with the actual handicap value
		// - For home: If UI shows -1.0, API likely returns Points=-1.0 (use directly)
		// - For away: If UI shows +1.0, API likely returns Points=1.0 (use directly)
		// Previous code was doing -(*pr.Points) for home, which would invert -1.0 to +1.0 (wrong!)
		// Fix: Use Points directly for both home and away
		for _, pr := range m.Prices {
			if pr.Points == nil {
				continue
			}
			odds := americanToDecimal(pr.Price)
			switch pr.Designation {
			case "home":
				// Use Points directly - API returns the actual handicap value with correct sign
				ev.Outcomes = append(ev.Outcomes, newOutcome(ev.ID, "handicap_home", formatSignedLine(*pr.Points), odds))
			case "away":
				// Use Points directly - API returns the actual handicap value with correct sign
				ev.Outcomes = append(ev.Outcomes, newOutcome(ev.ID, "handicap_away", formatSignedLine(*pr.Points), odds))
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
	slog.Debug("Related mapping", "main_matchup", matchupID, "related_count", len(rows))
	for _, it := range rows {
		pid := "nil"
		if it.parentID != nil {
			pid = strconv.FormatInt(*it.parentID, 10)
		}
		slog.Debug("Related matchup", "matchup_id", it.id, "parent_id", pid, "units", it.units, "league", it.league, "mapped", it.mapped)
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
		Bookmaker:   "Pinnacle888",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
