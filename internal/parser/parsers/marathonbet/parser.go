package marathonbet

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/parserutil"
)

const bookmakerName = "Marathonbet"

// leagueLinkRegex matches league URLs on all-events page: /su/betting/Football/.../+-+123
var leagueLinkRegex = regexp.MustCompile(`href="(/su/betting/Football/[^"]*\+-\+\d+)"`)

// eventLinkRegex matches event URLs on league page: .../Team1+vs+Team2+-+26807525
var eventLinkRegex = regexp.MustCompile(`href="(/su/betting/Football/[^"]*\+vs\+[^"]*\+-\+\d+)"`)

// eventJSONRegex extracts data-json value (event info); value may be HTML-encoded
var eventJSONRegex = regexp.MustCompile(`data-json="([^"]+)"`)

// dataSelRegex extracts data-sel JSON (odds); single or double quotes
var dataSelRegex = regexp.MustCompile(`data-sel=(?:"([^"]*)"|'([^']*)')`)

// marketTitleRegex matches market titles/headers before odds (Russian and English)
var marketTitleRegex = regexp.MustCompile(`(?i)(?:угл|corner|фол|foul|карт|card|желт|yellow|красн|red|тотал|total|гол|goal|удар|shot)`)

// parseAdditionalMarkets parses corners, fouls, and other markets from remaining odds
func parseAdditionalMarkets(match *models.Match, matchID, bookmakerKey string, oddsWithContexts []oddWithContext, now time.Time) {
	type marketGroup struct {
		eventType models.StandardEventType
		odds      []float64
		param     string
	}
	
	var currentMarket *marketGroup
	var markets []*marketGroup
	
	for idx, oc := range oddsWithContexts {
		// Detect market type from context
		contextLower := strings.ToLower(oc.context)
		var detectedType models.StandardEventType
		var param string
		
		// Check for corners (угловые) - look for "угл" or "corner" followed by a number
		if strings.Contains(contextLower, "угл") || strings.Contains(contextLower, "corner") {
			detectedType = models.StandardEventCorners
			// Try to extract parameter near the keyword (e.g., "10.5", "11")
			paramRegex := regexp.MustCompile(`(?:угл|corner)[^0-9]*(\d+\.?\d*)`)
			if matches := paramRegex.FindStringSubmatch(contextLower); len(matches) > 1 {
				param = matches[1]
			} else {
				// Try to find any number in the last 50 chars
				if numMatches := regexp.MustCompile(`(\d+\.?\d*)`).FindAllString(contextLower[len(contextLower)-50:], -1); len(numMatches) > 0 {
					param = numMatches[len(numMatches)-1]
				} else {
					param = "10.5" // default
				}
			}
		} else if strings.Contains(contextLower, "фол") || strings.Contains(contextLower, "foul") {
			detectedType = models.StandardEventFouls
			paramRegex := regexp.MustCompile(`(?:фол|foul)[^0-9]*(\d+\.?\d*)`)
			if matches := paramRegex.FindStringSubmatch(contextLower); len(matches) > 1 {
				param = matches[1]
			} else {
				if numMatches := regexp.MustCompile(`(\d+\.?\d*)`).FindAllString(contextLower[len(contextLower)-50:], -1); len(numMatches) > 0 {
					param = numMatches[len(numMatches)-1]
				} else {
					param = "10.5" // default
				}
			}
		} else if strings.Contains(contextLower, "карт") || strings.Contains(contextLower, "card") || strings.Contains(contextLower, "желт") || strings.Contains(contextLower, "yellow") {
			detectedType = models.StandardEventYellowCards
			paramRegex := regexp.MustCompile(`(?:карт|card|желт|yellow)[^0-9]*(\d+\.?\d*)`)
			if matches := paramRegex.FindStringSubmatch(contextLower); len(matches) > 1 {
				param = matches[1]
			} else {
				if numMatches := regexp.MustCompile(`(\d+\.?\d*)`).FindAllString(contextLower[len(contextLower)-50:], -1); len(numMatches) > 0 {
					param = numMatches[len(numMatches)-1]
				} else {
					param = "4.5" // default
				}
			}
		} else if strings.Contains(contextLower, "тотал") || strings.Contains(contextLower, "total") {
			// Total goals or other totals
			paramRegex := regexp.MustCompile(`(?:тотал|total)[^0-9]*(\d+\.?\d*)`)
			if matches := paramRegex.FindStringSubmatch(contextLower); len(matches) > 1 {
				param = matches[1]
			} else {
				if numMatches := regexp.MustCompile(`(\d+\.?\d*)`).FindAllString(contextLower[len(contextLower)-50:], -1); len(numMatches) > 0 {
					param = numMatches[len(numMatches)-1]
				} else {
					param = "2.5"
				}
			}
			// Check if it's a total for main match (already parsed) or something else
			if idx < 2 && len(oddsWithContexts) >= 5 {
				// Likely total 2.5 for goals
				continue
			}
		}
		
		// If we detected a market type, start or continue grouping
		if detectedType != "" {
			if currentMarket != nil && currentMarket.eventType == detectedType && currentMarket.param == param {
				// Continue current market
				currentMarket.odds = append(currentMarket.odds, oc.odds)
			} else {
				// Start new market
				if currentMarket != nil {
					markets = append(markets, currentMarket)
				}
				currentMarket = &marketGroup{
					eventType: detectedType,
					odds:      []float64{oc.odds},
					param:     param,
				}
			}
		} else if currentMarket != nil {
			// No market detected, but we have a current market - try to add if it makes sense
			if len(currentMarket.odds) < 2 && oc.odds >= 1.2 && oc.odds <= 5 {
				currentMarket.odds = append(currentMarket.odds, oc.odds)
			} else {
				// Finish current market
				markets = append(markets, currentMarket)
				currentMarket = nil
			}
		}
	}
	
	// Add last market if exists
	if currentMarket != nil {
		markets = append(markets, currentMarket)
	}
	
	// Create events from detected markets
	for _, mkt := range markets {
		if len(mkt.odds) < 2 {
			continue
		}
		
		eventID := matchID + "_" + bookmakerKey + "_" + string(mkt.eventType) + "_" + strings.ReplaceAll(mkt.param, ".", "_")
		event := models.Event{
			ID:         eventID,
			MatchID:    matchID,
			EventType:  string(mkt.eventType),
			MarketName: models.GetMarketName(mkt.eventType) + " " + mkt.param,
			Bookmaker:  bookmakerName,
			Outcomes:   []models.Outcome{},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		
		// Add outcomes (usually over/under pairs)
		if len(mkt.odds) >= 2 {
			event.Outcomes = append(event.Outcomes, models.Outcome{
				ID:          eventID + "_over",
				EventID:     eventID,
				OutcomeType: string(models.OutcomeTypeTotalOver),
				Parameter:   mkt.param,
				Odds:        mkt.odds[0],
				Bookmaker:   bookmakerName,
				CreatedAt:   now,
				UpdatedAt:   now,
			})
			event.Outcomes = append(event.Outcomes, models.Outcome{
				ID:          eventID + "_under",
				EventID:     eventID,
				OutcomeType: string(models.OutcomeTypeTotalUnder),
				Parameter:   mkt.param,
				Odds:        mkt.odds[1],
				Bookmaker:   bookmakerName,
				CreatedAt:   now,
				UpdatedAt:   now,
			})
		}
		
		if len(event.Outcomes) > 0 {
			match.Events = append(match.Events, event)
		}
	}
	
	// Also add Total 2.5 if we have enough odds and it wasn't detected as another market
	if len(oddsWithContexts) >= 5 {
		hasTotal := false
		for _, evt := range match.Events {
			if evt.MarketName == "Total 2.5" {
				hasTotal = true
				break
			}
		}
		if !hasTotal && oddsWithContexts[3].odds >= 1.2 && oddsWithContexts[3].odds <= 5 && oddsWithContexts[4].odds >= 1.2 && oddsWithContexts[4].odds <= 5 {
			totalParam := "2.5"
			totalEventID := matchID + "_" + bookmakerKey + "_total_2.5"
			totalEvent := models.Event{
				ID:         totalEventID,
				MatchID:    matchID,
				EventType:  string(models.StandardEventMainMatch),
				MarketName: "Total 2.5",
				Bookmaker:  bookmakerName,
				Outcomes: []models.Outcome{
					{ID: totalEventID + "_over", EventID: totalEventID, OutcomeType: string(models.OutcomeTypeTotalOver), Parameter: totalParam, Odds: oddsWithContexts[3].odds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
					{ID: totalEventID + "_under", EventID: totalEventID, OutcomeType: string(models.OutcomeTypeTotalUnder), Parameter: totalParam, Odds: oddsWithContexts[4].odds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				},
				CreatedAt: now,
				UpdatedAt: now,
			}
			match.Events = append(match.Events, totalEvent)
		}
	}
}

type eventJSON struct {
	TreeID          int64    `json:"treeId"`
	MarathonEventID int64    `json:"marathonEventId"`
	TeamNames       []string `json:"teamNames"`
	StartTime       string   `json:"startTime,omitempty"`
}

type oddWithContext struct {
	odds     float64
	position int
	context  string // HTML context before this odd
}

type selJSON struct {
	Epr float64 `json:"epr"` // decimal odds (may come as string)
	Prt string  `json:"prt"` // market/price type (comes as string like "CP")
}

// UnmarshalJSON handles epr field that can be either string or number
func (s *selJSON) UnmarshalJSON(data []byte) error {
	type Alias selJSON
	aux := &struct {
		Epr interface{} `json:"epr"` // accept both string and number
		Prt interface{} `json:"prt"` // accept both string and number (not used, but may vary)
		*Alias
	}{
		Alias: (*Alias)(s),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	// Convert epr to float64 if it's a string
	switch v := aux.Epr.(type) {
	case float64:
		s.Epr = v
	case string:
		var f float64
		if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
			s.Epr = f
		}
	}
	// Convert prt to string (not used, but parse anyway)
	if aux.Prt != nil {
		switch v := aux.Prt.(type) {
		case string:
			s.Prt = v
		case float64:
			s.Prt = fmt.Sprintf("%.0f", v)
		}
	}
	return nil
}

// Parser parses Marathonbet HTML: all-events → leagues → event pages (full data per match).
type Parser struct {
	cfg      *config.Config
	client   *Client
	incState *parserutil.IncrementalParserState
}

// NewParser creates a Marathonbet parser.
func NewParser(cfg *config.Config) *Parser {
	mc := cfg.Parser.Marathonbet
	baseURL := mc.BaseURL
	if baseURL == "" {
		baseURL = "https://www.marathonbet.ru"
	}
	sportID := mc.SportID
	if sportID <= 0 {
		sportID = 11 // Football
	}
	timeout := mc.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	userAgent := mc.UserAgent
	if userAgent == "" {
		userAgent = cfg.Parser.UserAgent
	}
	proxyList := mc.ProxyList
	if len(proxyList) > 0 {
		slog.Info("Marathonbet: Using proxy list from config", "proxy_count", len(proxyList))
	}
	client := NewClient(baseURL, userAgent, timeout, proxyList)
	return &Parser{cfg: cfg, client: client}
}

// Start runs one ParseOnce then blocks until context is done.
func (p *Parser) Start(ctx context.Context) error {
	slog.Info("Starting Marathonbet parser...")
	if err := p.ParseOnce(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

// Stop stops incremental state if any.
func (p *Parser) Stop() error {
	if p.incState != nil {
		p.incState.Stop(bookmakerName)
	}
	return nil
}

// StartIncremental starts background loop: each cycle runs ParseOnce.
func (p *Parser) StartIncremental(ctx context.Context, timeout time.Duration) error {
	if p.incState != nil && p.incState.IsRunning() {
		slog.Warn("Marathonbet: incremental parsing already started, skipping")
		return nil
	}
	p.incState = parserutil.NewIncrementalParserState(ctx)
	if err := p.incState.Start(bookmakerName); err != nil {
		return err
	}
	go parserutil.RunIncrementalLoop(p.incState.Ctx, timeout, bookmakerName, p.incState, p.runIncrementalCycle)
	slog.Info("Marathonbet: incremental parsing loop started")
	return nil
}

// runIncrementalCycle runs one full parse cycle.
func (p *Parser) runIncrementalCycle(ctx context.Context, timeout time.Duration) {
	cycleCtx, cancel := parserutil.CreateCycleContext(ctx, timeout)
	defer cancel()
	err := p.ParseOnce(cycleCtx)
	if err != nil {
		slog.Error("Marathonbet: parse cycle failed", "error", err)
	}
}

// TriggerNewCycle signals start of a new parsing cycle.
func (p *Parser) TriggerNewCycle() error {
	if p.incState == nil {
		return nil
	}
	return p.incState.TriggerNewCycle(bookmakerName)
}

// ParseOnce runs one full parse: all-events → leagues → event pages → AddMatch.
func (p *Parser) ParseOnce(ctx context.Context) error {
	sportID := p.cfg.Parser.Marathonbet.SportID
	if sportID <= 0 {
		sportID = 11
	}
	path := fmt.Sprintf("/su/all-events/%d", sportID)
	body, err := p.client.Get(ctx, path)
	if err != nil {
		return fmt.Errorf("marathonbet all-events: %w", err)
	}
	leaguePaths := extractLeaguePaths(body)
	slog.Info("Marathonbet: found leagues", "count", len(leaguePaths), "sport_id", sportID)

	// Rate limiting is handled globally in http_client.go (500ms minimum delay between all requests)
	// No need for additional delays here - the global mutex ensures proper spacing
	for _, leaguePath := range leaguePaths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		events, err := p.fetchLeagueEvents(ctx, leaguePath)
		if err != nil {
			slog.Warn("Marathonbet: league failed", "path", leaguePath, "error", err)
			continue
		}
		slog.Info("Marathonbet: found events in league", "league", leaguePath, "count", len(events))
		for _, eventPath := range events {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			match, err := p.fetchEventMatch(ctx, eventPath)
			if err != nil {
				slog.Warn("Marathonbet: event failed", "path", eventPath, "error", err)
				continue
			}
			if match != nil {
				health.AddMatch(match)
				slog.Info("Marathonbet: match added", "match", match.Name, "home", match.HomeTeam, "away", match.AwayTeam, "events", len(match.Events))
			}
		}
	}
	return nil
}

func extractLeaguePaths(htmlBody []byte) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range leagueLinkRegex.FindAllSubmatch(htmlBody, -1) {
		path := string(m[1])
		path = html.UnescapeString(path)
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func (p *Parser) fetchLeagueEvents(ctx context.Context, leaguePath string) ([]string, error) {
	body, err := p.client.Get(ctx, leaguePath)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var eventPaths []string
	for _, m := range eventLinkRegex.FindAllSubmatch(body, -1) {
		path := string(m[1])
		path = html.UnescapeString(path)
		if !seen[path] {
			seen[path] = true
			eventPaths = append(eventPaths, path)
		}
	}
	if len(eventPaths) == 0 {
		slog.Debug("Marathonbet: no events found in league", "path", leaguePath)
	}
	return eventPaths, nil
}

func (p *Parser) fetchEventMatch(ctx context.Context, eventPath string) (*models.Match, error) {
	body, err := p.client.Get(ctx, eventPath)
	if err != nil {
		return nil, err
	}
	return parseEventPage(body, eventPath)
}

// parseEventPage extracts event info and odds from event HTML, builds Match.
func parseEventPage(htmlBody []byte, eventPath string) (*models.Match, error) {
	bodyStr := string(htmlBody)

	// Event info from data-json (may be HTML-encoded)
	var ej eventJSON
	jsonMatch := eventJSONRegex.FindStringSubmatch(bodyStr)
	if len(jsonMatch) < 2 {
		return nil, fmt.Errorf("no event data-json")
	}
	decoded := html.UnescapeString(jsonMatch[1])
	if err := json.Unmarshal([]byte(decoded), &ej); err != nil {
		return nil, fmt.Errorf("parse event json: %w", err)
	}
	if len(ej.TeamNames) < 2 {
		return nil, fmt.Errorf("event has no teamNames")
	}
	homeRaw := strings.TrimSpace(ej.TeamNames[0])
	awayRaw := strings.TrimSpace(ej.TeamNames[1])
	home := Transliterate(homeRaw)
	away := Transliterate(awayRaw)
	if home == "" {
		home = homeRaw
	}
	if away == "" {
		away = awayRaw
	}

	var startTime time.Time
	if ej.StartTime != "" {
		// Marathonbet may use milliseconds or ISO
		if t, err := time.Parse(time.RFC3339, ej.StartTime); err == nil {
			startTime = t.UTC()
		} else if t, err := time.Parse("02.01.2006 15:04", ej.StartTime); err == nil {
			startTime = t.UTC()
		}
	}

	// Find all data-sel with their positions and context
	var oddsWithContexts []oddWithContext
	allMatches := dataSelRegex.FindAllStringSubmatchIndex(bodyStr, -1)
	
	for _, match := range allMatches {
		raw := ""
		if match[2] != -1 {
			raw = bodyStr[match[2]:match[3]]
		} else if match[4] != -1 {
			raw = bodyStr[match[4]:match[5]]
		}
		if raw == "" {
			continue
		}
		
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			continue
		}
		if s.Epr > 0 {
			// Get context (200 chars before this data-sel)
			start := match[0] - 200
			if start < 0 {
				start = 0
			}
			context := bodyStr[start:match[0]]
			
			oddsWithContexts = append(oddsWithContexts, oddWithContext{
				odds:     s.Epr,
				position: match[0],
				context:  context,
			})
		}
	}

	if len(oddsWithContexts) < 3 {
		return nil, fmt.Errorf("event has fewer than 3 odds")
	}

	matchID := models.CanonicalMatchID(home, away, startTime)
	now := time.Now()
	bookmakerKey := strings.ToLower(bookmakerName)

	match := &models.Match{
		ID:         matchID,
		Name:       fmt.Sprintf("%s vs %s", home, away),
		HomeTeam:   home,
		AwayTeam:   away,
		StartTime:  startTime,
		Sport:      "football",
		Bookmaker:  bookmakerName,
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Group odds by market type based on context
	// Main match (1X2): first three odds (default)
	mainOdds := oddsWithContexts[:3]
	if len(mainOdds) >= 3 {
		mainEventID := matchID + "_" + bookmakerKey + "_" + string(models.StandardEventMainMatch)
		mainEvent := models.Event{
			ID:         mainEventID,
			MatchID:    matchID,
			EventType:  string(models.StandardEventMainMatch),
			MarketName: models.GetMarketName(models.StandardEventMainMatch),
			Bookmaker:  bookmakerName,
			Outcomes: []models.Outcome{
				{ID: mainEventID + "_1", EventID: mainEventID, OutcomeType: string(models.OutcomeTypeHomeWin), Odds: mainOdds[0].odds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: mainEventID + "_X", EventID: mainEventID, OutcomeType: string(models.OutcomeTypeDraw), Odds: mainOdds[1].odds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: mainEventID + "_2", EventID: mainEventID, OutcomeType: string(models.OutcomeTypeAwayWin), Odds: mainOdds[2].odds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		match.Events = append(match.Events, mainEvent)
	}

	// Parse additional markets from remaining odds
	remainingOdds := oddsWithContexts[3:]
	parseAdditionalMarkets(match, matchID, bookmakerKey, remainingOdds, now)

	return match, nil
}
