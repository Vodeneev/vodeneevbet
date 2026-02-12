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
	"github.com/Vodeneev/vodeneevbet/internal/pkg/parserutil"
)

var runOnceMu sync.Mutex

type Parser struct {
	cfg     *config.Config
	client  *Client
	storage interfaces.Storage
	
	// Incremental parsing state
	incState *parserutil.IncrementalParserState
}

func NewParser(cfg *config.Config) *Parser {
	// Data is served directly from in-memory store

	baseURL := cfg.Parser.Pinnacle888.BaseURL
	if baseURL == "" {
		baseURL = "https://guest.api.arcadia.pinnacle.com"
	}

	mirrorURL := cfg.Parser.Pinnacle888.MirrorURL

	// Prepare auth headers if configured
	var authHeaders *AuthHeaders
	if cfg.Parser.Pinnacle888.UseAuthHeaders {
		authHeaders = &AuthHeaders{
			Cookies:         cfg.Parser.Pinnacle888.Cookies,
			XAppData:        cfg.Parser.Pinnacle888.XAppData,
			XCustID:         cfg.Parser.Pinnacle888.XCustID,
			UseAuthHeaders:  cfg.Parser.Pinnacle888.UseAuthHeaders,
		}
	}

	client := NewClient(baseURL, mirrorURL, cfg.Parser.Pinnacle888.APIKey, cfg.Parser.Pinnacle888.DeviceUUID, cfg.Parser.Timeout, cfg.Parser.Pinnacle888.ProxyList, authHeaders)

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
	if p.cfg.Parser.Pinnacle888.OddsURL != "" && p.cfg.Parser.Pinnacle888.IncludePrematch {
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

	slog.Info("Pinnacle888: runOnce started", "include_prematch", p.cfg.Parser.Pinnacle888.IncludePrematch, "odds_url_set", p.cfg.Parser.Pinnacle888.OddsURL != "")

	// Process pre-match matches
	if p.cfg.Parser.Pinnacle888.IncludePrematch && p.cfg.Parser.Pinnacle888.OddsURL != "" {
		slog.Info("Pinnacle888: starting pre-match matches processing")
		matches, err := p.processLineMatches(ctx)
		if err != nil {
			if ctx.Err() != nil {
				slog.Warn("Pinnacle888: pre-match matches processing stopped (time limit or context canceled)", "error_msg", err.Error())
			} else {
				slog.Error("Pinnacle888: failed to process pre-match matches", "error", err, "error_msg", err.Error())
			}
		} else {
			// Add matches to in-memory store
			for _, match := range matches {
				select {
				case <-ctx.Done():
					return nil
				default:
				}
				health.AddMatch(match)
			}
			slog.Info("Pinnacle888: pre-match matches processed", "count", len(matches))
		}
	}

	// Otherwise, discover and process all matchups for relevant sports (legacy mode)
	if !p.cfg.Parser.Pinnacle888.IncludePrematch {
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

func (p *Parser) Stop() error {
	if p.incState != nil {
		p.incState.Stop("Pinnacle888")
	}
	return nil
}

func (p *Parser) GetName() string {
	return "Pinnacle888"
}

// StartIncremental starts continuous incremental parsing in background
// It parses leagues one by one with the specified interval and updates storage incrementally
func (p *Parser) StartIncremental(ctx context.Context, timeout time.Duration) error {
	if p.incState != nil && p.incState.IsRunning() {
		slog.Warn("Pinnacle888: incremental parsing already started, skipping")
		return nil
	}
	
	if timeout > 0 {
		slog.Info("Pinnacle888: initializing incremental parsing", "timeout", timeout)
	} else {
		slog.Info("Pinnacle888: initializing incremental parsing", "timeout", "unlimited")
	}
	
	p.incState = parserutil.NewIncrementalParserState(ctx)
	if err := p.incState.Start("Pinnacle888"); err != nil {
		return err
	}
	
	// Start background incremental parsing loop
	go parserutil.RunIncrementalLoop(p.incState.Ctx, timeout, "Pinnacle888", p.incState, p.runIncrementalCycle)
	slog.Info("Pinnacle888: incremental parsing loop started in background")
	
	return nil
}

// TriggerNewCycle signals the parser to start a new parsing cycle
func (p *Parser) TriggerNewCycle() error {
	if p.incState == nil {
		return fmt.Errorf("incremental parsing not started")
	}
	return p.incState.TriggerNewCycle("Pinnacle888")
}

// incrementalLoop is now handled by parserutil.RunIncrementalLoop

// runIncrementalCycle runs one full parsing cycle incrementally (by leagues)
func (p *Parser) runIncrementalCycle(ctx context.Context, timeout time.Duration) {
	start := time.Now()
	cycleID := time.Now().Unix()
	parserutil.LogCycleStart("Pinnacle888", cycleID, timeout)
	
	// Create context with timeout for this cycle (if timeout > 0)
	// If timeout is 0, use original context without timeout to process all leagues
	cycleCtx, cancel := parserutil.CreateCycleContext(ctx, timeout)
	defer cancel()
	defer func() {
		duration := time.Since(start)
		parserutil.LogCycleFinish("Pinnacle888", cycleID, duration)
	}()
	
	// Resolve mirror once at the start of each cycle
	if p.cfg.Parser.Pinnacle888.OddsURL != "" && p.cfg.Parser.Pinnacle888.IncludePrematch {
		slog.Info("Pinnacle888: resolving mirror URL", "cycle_id", cycleID)
		if err := p.client.ensureResolved(); err != nil {
			slog.Warn("Pinnacle888: mirror resolve failed at cycle start", "cycle_id", cycleID, "error", err)
		} else {
			slog.Info("Pinnacle888: mirror URL resolved successfully", "cycle_id", cycleID)
		}
	}
	
	// Process pre-match matches incrementally (continuously, no pauses)
	if p.cfg.Parser.Pinnacle888.IncludePrematch && p.cfg.Parser.Pinnacle888.OddsURL != "" {
		slog.Info("Pinnacle888: starting pre-match incremental processing", "cycle_id", cycleID)
		p.processOddsLeaguesFlowIncremental(cycleCtx, false)
		slog.Info("Pinnacle888: pre-match incremental processing completed", "cycle_id", cycleID)
	}
}

// processOddsLeaguesFlowIncremental processes leagues incrementally, updating storage after each league
// Processes leagues continuously without pauses between them
func (p *Parser) processOddsLeaguesFlowIncremental(ctx context.Context, isLive bool) {
	oddsURL := p.cfg.Parser.Pinnacle888.OddsURL
	if oddsURL == "" {
		return
	}
	sportID := int64(29) // Soccer
	
	mode := "pre-match"
	if isLive {
		mode = "live"
	}
	slog.Info("Pinnacle888: starting incremental leagues flow", "mode", mode, "oddsURL", oddsURL)
	
	leagues, err := p.client.GetLeagues(oddsURL, sportID)
	if err != nil {
		slog.Error("Pinnacle888: failed to get leagues", "mode", mode, "error", err)
		return
	}
	slog.Info("Pinnacle888: fetched leagues", "mode", mode, "count", len(leagues))
	
	// Filter leagues with events
	var leaguesWithEvents []LeagueListItem
	for _, l := range leagues {
		if l.TotalEvents > 0 {
			leaguesWithEvents = append(leaguesWithEvents, l)
		}
	}
	slog.Info("Pinnacle888: filtering leagues with events", "mode", mode, "total", len(leagues), "with_events", len(leaguesWithEvents))
	
	totalLeagues := len(leaguesWithEvents)
	
	// Process leagues one by one continuously, updating storage incrementally
	// No pauses between leagues - just continuous parsing until timeout or all leagues processed
	matchesTotal := 0
	for idx, league := range leaguesWithEvents {
		select {
		case <-ctx.Done():
			slog.Warn("Pinnacle888: incremental processing interrupted", "mode", mode, "leagues_processed", idx, "leagues_total", totalLeagues)
			return
		default:
		}
		
		leagueIdx := idx + 1
		leagueStart := time.Now()
		slog.Info("Pinnacle888: processing league incrementally", 
			"mode", mode,
			"league", league.Name, 
			"league_code", league.LeagueCode,
			"progress", fmt.Sprintf("%d/%d", leagueIdx, totalLeagues),
			"percent", fmt.Sprintf("%.1f%%", float64(leagueIdx)/float64(totalLeagues)*100))
		
		// Process single league and update storage immediately
		matches := p.processSingleLeague(ctx, oddsURL, league, sportID, isLive)
		
		// Update storage incrementally after each league
		// These matches are immediately available via /matches endpoint
		for _, match := range matches {
			health.AddMatch(match)
		}
		slog.Debug("Pinnacle888: matches saved to store", "mode", mode, "league", league.Name, "matches_count", len(matches))
		
		matchesTotal += len(matches)
		leagueDuration := time.Since(leagueStart)
		slog.Info("Pinnacle888: league processed incrementally", 
			"mode", mode,
			"league", league.Name,
			"matches", len(matches),
			"matches_total", matchesTotal,
			"duration", leagueDuration,
			"progress", fmt.Sprintf("%d/%d", leagueIdx, totalLeagues),
			"percent", fmt.Sprintf("%.1f%%", float64(leagueIdx)/float64(totalLeagues)*100))
	}
	
	slog.Info("Pinnacle888: incremental leagues flow finished", 
		"mode", mode, 
		"leagues_processed", len(leaguesWithEvents),
		"matches_total", matchesTotal)
}

// processSingleLeague processes a single league and returns matches
func (p *Parser) processSingleLeague(ctx context.Context, oddsURL string, league LeagueListItem, sportID int64, isLive bool) []*models.Match {
	var matches []*models.Match
	leagueStart := time.Now()
	
	slog.Debug("Pinnacle888: fetching league odds", "league", league.Name, "league_code", league.LeagueCode, "total_events", league.TotalEvents)
	data, err := p.client.GetLeagueOdds(oddsURL, league.LeagueCode, sportID, isLive)
	if err != nil {
		slog.Warn("Pinnacle888: failed to get league odds", "league", league.LeagueCode, "error", err)
		return matches
	}
	
	var leagueResp OddsResponse
	if err := json.Unmarshal(data, &leagueResp); err != nil {
		slog.Warn("Pinnacle888: failed to parse league odds", "league", league.LeagueCode, "error", err)
		return matches
	}
	
	eventsProcessed := 0
	eventsSkipped := 0
	eventsError := 0
	
	for _, lg := range leagueResp.Leagues {
		leagueName := lg.Name
		// Build referer path for this league
		refererPath := fmt.Sprintf("/en/standard/soccer/%s", league.LeagueCode)
		for _, ev := range lg.Events {
			select {
			case <-ctx.Done():
				slog.Warn("Pinnacle888: league processing interrupted", "league", league.Name, "events_processed", eventsProcessed)
				return matches
			default:
			}
			
			eventData, err := p.client.GetEventOdds(oddsURL, ev.ID, refererPath)
			if err != nil {
				eventsError++
				slog.Debug("Pinnacle888: get event odds failed", "eventId", ev.ID, "error", err)
				continue
			}
			
			match, err := ParseEventOddsResponse(eventData)
			if err != nil {
				eventsError++
				slog.Debug("Pinnacle888: parse event odds failed", "eventId", ev.ID, "error", err)
				continue
			}
			
			if match == nil {
				eventsSkipped++
				continue
			}
			
			eventsProcessed++
			matchName := match.HomeTeam + " vs " + match.AwayTeam
			if matchName == " vs " {
				matchName = match.Name
			}
			slog.Debug("Pinnacle888: parsed match", "league", leagueName, "match", matchName)
			matches = append(matches, match)
		}
	}
	
	leagueDuration := time.Since(leagueStart)
	slog.Debug("Pinnacle888: league processing completed", 
		"league", league.Name,
		"matches", len(matches),
		"events_processed", eventsProcessed,
		"events_skipped", eventsSkipped,
		"events_error", eventsError,
		"duration", leagueDuration)
	
	return matches
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
			// Strictly exclude live matches (matches that have already started)
			if !st.After(now) || st.After(maxStart) {
				if !st.After(now) {
					// Live match - skip it
					slog.Debug("Pinnacle888: filtered live match", "matchup_id", mu.ID, "start", st.Format(time.RFC3339), "now", now.Format(time.RFC3339))
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

		// Process each main matchup as a match.
		for mainID, related := range group {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			// Log related matchups info for debugging statistical events
			statisticalEventsFound := 0
			for _, r := range related {
				if r.ID == mainID {
					continue
				}
				if et, ok := inferStandardEventType(r); ok && et != models.StandardEventMainMatch {
					statisticalEventsFound++
				}
			}
			// Always log related matchups info for debugging
			if len(related) > 1 {
				slog.Info("Pinnacle888: related matchups found (incremental)", 
					"matchup_id", mainID, 
					"total_related", len(related)-1, 
					"statistical_events", statisticalEventsFound)
			} else if len(related) == 1 {
				slog.Info("Pinnacle888: no related matchups (only main) (incremental)", "matchup_id", mainID)
			} else {
				slog.Warn("Pinnacle888: no related matchups at all (incremental)", "matchup_id", mainID)
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
					slog.Debug("Pinnacle888: double-check filtered live match", "match_id", m.ID, "start", matchStartTime.Format(time.RFC3339), "now", checkNow.Format(time.RFC3339))
					continue
				}
			}

			// Add match to in-memory store for fast API access (primary storage)
			// Data is served directly from memory
			health.AddMatch(m)
		}
	}

	return nil
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

	var allMatches []*models.Match
	totalLeagues := len(leaguesWithEvents)

	for idx, league := range leaguesWithEvents {
		select {
		case <-ctx.Done():
			return allMatches, ctx.Err()
		default:
		}

		leagueIdx := idx + 1
		slog.Info(fmt.Sprintf("Pinnacle888: processing league: %s (%d/%d)", league.Name, leagueIdx, totalLeagues))

		data, err := p.client.GetLeagueOdds(oddsURL, league.LeagueCode, sportID, isLive)
		if err != nil {
			slog.Debug("Pinnacle888: get league odds", "league", league.LeagueCode, "error", err)
			continue
		}

		var leagueResp OddsResponse
		if err := json.Unmarshal(data, &leagueResp); err != nil {
			slog.Debug("Pinnacle888: parse league odds", "league", league.LeagueCode, "error", err)
			continue
		}

		var eventsTotal, getEventErr, parseErr, skipped, matchesAdded int
		var firstGetErrMsg string
		for _, lg := range leagueResp.Leagues {
			leagueName := lg.Name
			// Build referer path for this league
			refererPath := fmt.Sprintf("/en/standard/soccer/%s", league.LeagueCode)
			for _, ev := range lg.Events {
				eventsTotal++
				select {
				case <-ctx.Done():
					return allMatches, ctx.Err()
				default:
				}
				eventData, err := p.client.GetEventOdds(oddsURL, ev.ID, refererPath)
				if err != nil {
					getEventErr++
					if firstGetErrMsg == "" {
						firstGetErrMsg = err.Error()
						if len(firstGetErrMsg) > 120 {
							firstGetErrMsg = firstGetErrMsg[:117] + "..."
						}
					}
					slog.Debug("Pinnacle888: get event odds", "eventId", ev.ID, "error", err)
					continue
				}
				match, err := ParseEventOddsResponse(eventData)
				if err != nil {
					parseErr++
					slog.Debug("Pinnacle888: parse event odds", "eventId", ev.ID, "error", err)
					continue
				}
				if match == nil {
					skipped++
					continue
				}
				matchesAdded++
				matchName := match.HomeTeam + " vs " + match.AwayTeam
				if matchName == " vs " {
					matchName = match.Name
				}
				slog.Info(fmt.Sprintf("Pinnacle888: parsed match: %s | %s", leagueName, matchName))
				allMatches = append(allMatches, match)
			}
		}
		finishMsg := fmt.Sprintf("Pinnacle888: league finished: %s | events=%d get_err=%d parse_err=%d skipped=%d matches=%d", league.Name, eventsTotal, getEventErr, parseErr, skipped, matchesAdded)
		if getEventErr > 0 && firstGetErrMsg != "" {
			finishMsg += " | first_get_err=" + firstGetErrMsg
		}
		slog.Info(finishMsg)
	}

	liveLabel := "pre-match"
	if isLive {
		liveLabel = "live"
	}
	slog.Info("Pinnacle888: leagues flow finished", "mode", liveLabel, "matches", len(allMatches), "leagues_processed", len(leaguesWithEvents))
	return allMatches, nil
}


func (p *Parser) processMatchup(ctx context.Context, matchupID int64) error {
	related, err := p.client.GetRelatedMatchups(matchupID)
	if err != nil {
		return err
	}
	logRelatedMapping(matchupID, related)
	
	// Log INFO level summary for debugging statistical events
	statisticalEventsFound := 0
	for _, r := range related {
		if r.ID == matchupID {
			continue
		}
		if et, ok := inferStandardEventType(r); ok && et != models.StandardEventMainMatch {
			statisticalEventsFound++
		}
	}
	// Always log related matchups info for debugging
	if len(related) > 1 {
		slog.Info("Pinnacle888: related matchups found", 
			"matchup_id", matchupID, 
			"total_related", len(related)-1, 
			"statistical_events", statisticalEventsFound)
	} else if len(related) == 1 {
		slog.Info("Pinnacle888: no related matchups (only main)", "matchup_id", matchupID)
	} else {
		slog.Warn("Pinnacle888: no related matchups at all", "matchup_id", matchupID)
	}
	
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
	statisticalEventsCount := 0
	for _, r := range related {
		if r.ParentID == nil || *r.ParentID != matchupID {
			continue
		}
		if et, ok := inferStandardEventType(r); ok {
			matchupEventType[r.ID] = et
			foundByParent = true
			if et != models.StandardEventMainMatch {
				statisticalEventsCount++
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
				if et != models.StandardEventMainMatch {
					statisticalEventsCount++
				}
			}
		}
	}
	
	// Log statistical events found for this match (always log for debugging)
	slog.Info("Pinnacle888: statistical events mapping result", 
		"matchup_id", matchupID, 
		"statistical_events_count", statisticalEventsCount,
		"home_team", home,
		"away_team", away,
		"total_related_matchups", len(related)-1)

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

	// Log final event count for debugging (always log for matches with multiple events or when expecting statistical events)
	statisticalEventsInMatch := 0
	eventTypes := make([]string, 0, len(match.Events))
	for _, ev := range match.Events {
		eventTypes = append(eventTypes, ev.EventType)
		if ev.EventType != string(models.StandardEventMainMatch) {
			statisticalEventsInMatch++
		}
	}
	if statisticalEventsInMatch > 0 || statisticalEventsCount > 0 {
		slog.Info("Pinnacle888: match built with events", 
			"matchup_id", matchupID,
			"match_id", matchID,
			"total_events", len(match.Events),
			"statistical_events", statisticalEventsInMatch,
			"statistical_events_mapped", statisticalEventsCount,
			"event_types", eventTypes)
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
		slog.Debug("Pinnacle888: no related matchups found", "matchup_id", matchupID)
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
