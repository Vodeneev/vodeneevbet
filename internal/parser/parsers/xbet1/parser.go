package xbet1

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/parserutil"
)

var runOnceMu sync.Mutex

type Parser struct {
	cfg     *config.Config
	client  *Client
	storage interface{} // No external storage - data served from memory
	
	// Incremental parsing state
	incState *parserutil.IncrementalParserState
}

func NewParser(cfg *config.Config) *Parser {
	const defaultMirror = "https://1xbet-skwu.top/link"
	baseURL := cfg.Parser.Xbet1.BaseURL
	mirrorURL := cfg.Parser.Xbet1.MirrorURL

	// Like pinnacle888: explicit base_url => use it, no mirror. Empty base_url => use mirror (resolve at runtime).
	if baseURL != "" {
		mirrorURL = ""
		slog.Info("1xbet: using fixed base URL, mirror disabled", "base_url", baseURL)
	} else {
		baseURL = "" // will use getResolvedBaseURL() after ensureResolved()
		if mirrorURL == "" {
			mirrorURL = defaultMirror
		}
		slog.Info("1xbet: using mirror (resolve at runtime)", "mirror_url", mirrorURL)
	}

	client := NewClient(baseURL, mirrorURL, cfg.Parser.Timeout, cfg.Parser.Xbet1.ProxyList)
	slog.Info("1xbet: parser init", "base_url", baseURL, "mirror_url", mirrorURL)

	return &Parser{
		cfg:     cfg,
		client:  client,
		storage: nil,
	}
}

// runOnce performs a single parsing run
func (p *Parser) runOnce(ctx context.Context) error {
	runOnceMu.Lock()
	defer runOnceMu.Unlock()
	start := time.Now()
	var totalMatches int
	defer func() {
		slog.Info("1xbet: цикл парсинга завершён", "matches", totalMatches, "duration", time.Since(start))
	}()

	// Resolve mirror once at the start of each run
	if p.cfg.Parser.Xbet1.MirrorURL != "" {
		if err := p.client.ensureResolved(); err != nil {
			slog.Warn("1xbet: mirror resolve failed at run start, will retry next iteration", "error", err)
		}
	}

	sportIDs := p.getSportIDsToProcess()
	slog.Info("1xbet: runOnce started", "include_prematch", p.cfg.Parser.Xbet1.IncludePrematch, "sport_ids", sportIDs)

	// Process pre-match matches (по каждому sport_id из списка)
	if p.cfg.Parser.Xbet1.IncludePrematch {
		for _, sportID := range sportIDs {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			slog.Info("1xbet: starting pre-match matches processing", "sport_id", sportID)
			matches, err := p.processLeaguesFlowWithSportID(ctx, sportID)
			if err != nil {
				if ctx.Err() != nil {
					slog.Warn("1xbet: pre-match matches processing stopped (time limit or context canceled)", "error", err)
				} else {
					slog.Error("1xbet: failed to process pre-match matches", "sport_id", sportID, "error", err)
				}
				continue
			}
			for _, match := range matches {
				select {
				case <-ctx.Done():
					return nil
				default:
				}
				health.AddMatch(match)
			}
			totalMatches += len(matches)
			slog.Info("1xbet: pre-match matches processed", "sport_id", sportID, "count", len(matches))
		}
		if totalMatches > 0 {
			slog.Info("1xbet: pre-match total matches", "total", totalMatches)
		}
	}

	return nil
}

// getSportIDsToProcess returns list of sport IDs to parse (SportIDs if set, else [SportID] or [1])
func (p *Parser) getSportIDsToProcess() []int {
	if len(p.cfg.Parser.Xbet1.SportIDs) > 0 {
		return p.cfg.Parser.Xbet1.SportIDs
	}
	sportID := p.cfg.Parser.Xbet1.SportID
	if sportID == 0 {
		sportID = 1
	}
	return []int{sportID}
}

// processLeaguesFlowWithSportID processes all leagues for one sport and returns matches
func (p *Parser) processLeaguesFlowWithSportID(ctx context.Context, sportID int) ([]*models.Match, error) {
	countryID := p.cfg.Parser.Xbet1.CountryID
	if countryID == 0 {
		countryID = 1
	}
	virtualSports := p.cfg.Parser.Xbet1.VirtualSports

	slog.Info("1xbet: starting leagues flow", "sport_id", sportID, "country_id", countryID)

	champs, err := p.client.GetChamps(sportID, countryID, virtualSports)
	if err != nil {
		slog.Error("1xbet: failed to get championships", "error", err)
		return nil, fmt.Errorf("get championships: %w", err)
	}
	slog.Info("1xbet: fetched championships", "count", len(champs))

	var champsWithMatches []ChampItem
	for _, champ := range champs {
		if champ.T == 1000 || champ.T == 0 {
			champsWithMatches = append(champsWithMatches, champ)
		}
	}
	slog.Info("1xbet: filtering championships with matches", "total", len(champs), "with_matches", len(champsWithMatches))

	var allMatches []*models.Match
	for _, champ := range champsWithMatches {
		select {
		case <-ctx.Done():
			return allMatches, ctx.Err()
		default:
		}
		matches := p.processSingleChampionship(ctx, champ)
		allMatches = append(allMatches, matches...)
	}
	slog.Info("1xbet: leagues flow finished", "sport_id", sportID, "matches", len(allMatches))
	return allMatches, nil
}

func (p *Parser) Start(ctx context.Context) error {
	slog.Info("Starting 1xbet parser (background mode - periodic parsing runs automatically)...")

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
		p.incState.Stop("1xbet")
	}
	return nil
}

func (p *Parser) GetName() string {
	return "1xbet"
}

// StartIncremental starts continuous incremental parsing in background
func (p *Parser) StartIncremental(ctx context.Context, timeout time.Duration) error {
	if p.incState != nil && p.incState.IsRunning() {
		slog.Warn("1xbet: incremental parsing already started, skipping")
		return nil
	}
	
	if timeout > 0 {
		slog.Info("1xbet: initializing incremental parsing", "timeout", timeout)
	} else {
		slog.Info("1xbet: initializing incremental parsing", "timeout", "unlimited")
	}
	
	p.incState = parserutil.NewIncrementalParserState(ctx)
	if err := p.incState.Start("1xbet"); err != nil {
		return err
	}
	
	// Start background incremental parsing loop
	go parserutil.RunIncrementalLoop(p.incState.Ctx, timeout, "1xbet", p.incState, p.runIncrementalCycle)
	slog.Info("1xbet: incremental parsing loop started in background")
	
	return nil
}

// TriggerNewCycle signals the parser to start a new parsing cycle
func (p *Parser) TriggerNewCycle() error {
	if p.incState == nil {
		return fmt.Errorf("incremental parsing not started")
	}
	return p.incState.TriggerNewCycle("1xbet")
}

// runIncrementalCycle runs one full parsing cycle incrementally (by leagues)
func (p *Parser) runIncrementalCycle(ctx context.Context, timeout time.Duration) {
	start := time.Now()
	cycleID := time.Now().Unix()
	parserutil.LogCycleStart("1xbet", cycleID, timeout)
	
	cycleCtx, cancel := parserutil.CreateCycleContext(ctx, timeout)
	defer cancel()
	defer func() {
		duration := time.Since(start)
		parserutil.LogCycleFinish("1xbet", cycleID, duration)
	}()
	
	// Resolve mirror only when not using fixed base URL (we use fixed when base_url set or default)
	useMirror := p.cfg.Parser.Xbet1.MirrorURL != "" && p.cfg.Parser.Xbet1.BaseURL == ""
	if useMirror {
		slog.Info("1xbet: resolving mirror URL", "cycle_id", cycleID)
		if err := p.client.ensureResolved(); err != nil {
			slog.Warn("1xbet: mirror resolve failed at cycle start", "cycle_id", cycleID, "error", err)
		} else {
			slog.Info("1xbet: mirror URL resolved successfully", "cycle_id", cycleID)
		}
	}
	
	// Process pre-match matches incrementally
	if p.cfg.Parser.Xbet1.IncludePrematch {
		slog.Info("1xbet: starting pre-match incremental processing", "cycle_id", cycleID)
		p.processLeaguesFlowIncremental(cycleCtx)
		slog.Info("1xbet: pre-match incremental processing completed", "cycle_id", cycleID)
	}
}

// processLeaguesFlowIncremental processes leagues incrementally, updating storage after each league (all sport_ids)
func (p *Parser) processLeaguesFlowIncremental(ctx context.Context) {
	sportIDs := p.getSportIDsToProcess()
	countryID := p.cfg.Parser.Xbet1.CountryID
	if countryID == 0 {
		countryID = 1
	}
	virtualSports := p.cfg.Parser.Xbet1.VirtualSports

	for _, sportID := range sportIDs {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if sportID == 40 {
			slog.Info("1xbet: starting esports flow (sport_id=40)", "country_id", countryID)
		}
		slog.Info("1xbet: starting incremental leagues flow", "sport_id", sportID, "country_id", countryID)

		champs, err := p.client.GetChamps(sportID, countryID, virtualSports)
		if err != nil {
			slog.Error("1xbet: failed to get championships", "sport_id", sportID, "error", err)
			if sportID == 40 {
				slog.Warn("1xbet: esports (sport_id=40) GetChamps failed — no esports from xbet", "error", err)
			}
			continue
		}
		slog.Info("1xbet: fetched championships", "sport_id", sportID, "count", len(champs))
		if sportID == 40 && len(champs) == 0 {
			slog.Warn("1xbet: esports (sport_id=40) GetChamps returned 0 champs — check API or config")
		}

		var champsWithMatches []ChampItem
		for _, champ := range champs {
			// Football: only main leagues (T=1000 or T=0). Esports (sport_id=40): API uses other T values — take all.
			if sportID == 40 {
				champsWithMatches = append(champsWithMatches, champ)
			} else if champ.T == 1000 || champ.T == 0 {
				champsWithMatches = append(champsWithMatches, champ)
			}
		}
		slog.Info("1xbet: filtering championships with matches", "sport_id", sportID, "total", len(champs), "with_matches", len(champsWithMatches))

		totalChamps := len(champsWithMatches)
		matchesTotal := 0

		for idx, champ := range champsWithMatches {
			select {
			case <-ctx.Done():
				slog.Warn("1xbet: incremental processing interrupted", "champs_processed", idx, "champs_total", totalChamps)
				return
			default:
			}

			champIdx := idx + 1
			champStart := time.Now()
			slog.Info("1xbet: processing championship incrementally",
				"championship", champ.LE,
				"championship_id", champ.LI,
				"progress", fmt.Sprintf("%d/%d", champIdx, totalChamps),
				"percent", fmt.Sprintf("%.1f%%", float64(champIdx)/float64(totalChamps)*100))

			matches := p.processSingleChampionship(ctx, champ)
			for _, match := range matches {
				health.AddMatch(match)
			}
			slog.Debug("1xbet: matches saved to store", "championship", champ.LE, "matches_count", len(matches))

			matchesTotal += len(matches)
			champDuration := time.Since(champStart)
			slog.Info("1xbet: championship processed incrementally",
				"championship", champ.LE,
				"matches", len(matches),
				"matches_total", matchesTotal,
				"duration", champDuration,
				"progress", fmt.Sprintf("%d/%d", champIdx, totalChamps),
				"percent", fmt.Sprintf("%.1f%%", float64(champIdx)/float64(totalChamps)*100))
		}

		slog.Info("1xbet: incremental leagues flow finished for sport",
			"sport_id", sportID,
			"championships_processed", len(champsWithMatches),
			"matches_total", matchesTotal)
		if sportID == 40 {
			slog.Info("1xbet: esports (sport_id=40) flow finished", "championships", len(champsWithMatches), "football_matches_in_run", matchesTotal)
		}
	}
}

// processSingleChampionship processes a single championship and returns matches
func (p *Parser) processSingleChampionship(ctx context.Context, champ ChampItem) []*models.Match {
	var matches []*models.Match
	champStart := time.Now()
	
	slog.Debug("1xbet: fetching championship matches", "championship", champ.LE, "championship_id", champ.LI)

	// Use sport ID from championship (from GetChamps(sportID)) when set; else config/default
	sportID := champ.SI
	if sportID == 0 {
		sportID = p.cfg.Parser.Xbet1.SportID
	}
	if sportID == 0 {
		sportID = 1
	}
	countryID := p.cfg.Parser.Xbet1.CountryID
	if countryID == 0 {
		countryID = 1
	}
	virtualSports := p.cfg.Parser.Xbet1.VirtualSports
	
	// Get matches for this championship
	matchList, err := p.client.GetMatches(sportID, champ.LI, 40, 4, countryID, virtualSports)
	if err != nil {
		slog.Warn("1xbet: failed to get championship matches", "championship", champ.LE, "sport_id", sportID, "error", err)
		if sportID == 40 {
			slog.Warn("1xbet: esports GetMatches failed", "champ_id", champ.LI, "error", err)
		}
		return matches
	}

	slog.Info("1xbet: fetched championship matches", "championship", champ.LE, "sport_id", sportID, "matches_count", len(matchList))

	// Process each match
	var esportsAdded int
	for idx, matchData := range matchList {
		select {
		case <-ctx.Done():
			slog.Warn("1xbet: championship processing interrupted", "championship", champ.LE)
			return matches
		default:
		}

		// Log match processing start (skip per-match Info for non-esports to reduce noise; esports log below)
		if sportID != 40 {
			slog.Info("1xbet: processing match from championship", "championship", champ.LE, "championship_id", champ.LI, "match_index", idx+1, "total_matches", len(matchList), "match_id", matchData.I)
		}

		// Get detailed game information
		gameDetails, err := p.client.GetGame(matchData.I, true, true, 250, 4, "", countryID, 1, true)
		if err != nil {
			if sportID == 40 {
				slog.Info("1xbet: esports GetGame failed (first failures logged)", "match_id", matchData.I, "champ", champ.LE, "error", err)
			} else {
				slog.Debug("1xbet: failed to get game details", "championship", champ.LE, "match_id", matchData.I, "error", err)
			}
			continue
		}

		// Киберспорт (sport_id=40) → отдельная модель EsportsMatch
		if sportID == 40 {
			lineMatch := BuildLineMatchFromGameDetails(gameDetails, champ.LE, "esports", "1xbet")
			if lineMatch != nil {
				em := lineMatch.ToEsportsMatch()
				if em != nil {
					health.AddEsportsMatch(em)
					esportsAdded++
				} else {
					slog.Debug("1xbet: esports ToEsportsMatch returned nil", "match_id", matchData.I)
				}
			} else {
				slog.Debug("1xbet: esports BuildLineMatchFromGameDetails returned nil", "match_id", matchData.I)
			}
			continue
		}

		// Футбол: текущий путь
		match := ParseGameDetailsWithClient(gameDetails, champ.LE, p.client)
		if match != nil {
			matches = append(matches, match)
		}
	}

	champDuration := time.Since(champStart)
	if sportID == 40 && esportsAdded > 0 {
		slog.Info("1xbet: championship esports added to store", "championship", champ.LE, "champ_id", champ.LI, "esports_added", esportsAdded, "duration", champDuration)
	}
	slog.Debug("1xbet: championship processing completed",
		"championship", champ.LE,
		"matches", len(matches),
		"duration", champDuration)

	return matches
}

// processLeaguesFlow processes all leagues for the first configured sport (backward compatibility).
func (p *Parser) processLeaguesFlow(ctx context.Context) ([]*models.Match, error) {
	ids := p.getSportIDsToProcess()
	return p.processLeaguesFlowWithSportID(ctx, ids[0])
}
