package marathonbet

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"regexp"
	"sort"
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

// dateTimeRegex matches date and time in format "12 фев 23:00" in nav-event-date element
var dateTimeRegex = regexp.MustCompile(`<td[^>]*class="[^"]*nav-event-date[^"]*"[^>]*>\s*([^\s<]+\s+[^\s<]+\s+[^\s<]+)\s*</td>`)

// dateWrapperRegex matches date in league/coupon row: <div class="date-wrapper">15 фев 17:00</div>
var dateWrapperRegex = regexp.MustCompile(`<div[^>]*class="[^"]*date-wrapper[^"]*"[^>]*>\s*([^\s<]+\s+[^\s<]+\s+[^\s<]+)\s*</div>`)

// marketTypeRegex matches data-market-type attribute
var marketTypeRegex = regexp.MustCompile(`data-market-type="([^"]+)"`)

// marketOddsRegex matches data-sel with data-market-type in the same or nearby element
// Captures: market type, mutable id, odds JSON
var marketOddsRegex = regexp.MustCompile(`data-market-type="([^"]+)"[^>]*>\s*<[^>]*data-sel=(?:"([^"]*)"|'([^']*)')`)

// handicapParamRegex extracts handicap parameter from context (e.g., "(0)", "(-1.5)")
var handicapParamRegex = regexp.MustCompile(`\(([+-]?\d+\.?\d*)\)`)

// totalParamRegex extracts total parameter from context (e.g., "(2.5)")
var totalParamRegex = regexp.MustCompile(`\((\d+\.?\d*)\)`)

// mutableIdRegex matches data-mutable-id for market identification
var mutableIdRegex = regexp.MustCompile(`data-mutable-id="([^"]+)"`)

// openTdRegex matches opening <td> tag to find cell boundary (space or > after "td")
var openTdRegex = regexp.MustCompile(`<td[\s>]`)

// preferenceIdRegex matches data-preference-id for market identification
var preferenceIdRegex = regexp.MustCompile(`data-preference-id="([^"]+)"`)

// selectionKeyRegex extracts selection key for parameter extraction (e.g., "Total_Corners6.Under_5.5")
var selectionKeyRegex = regexp.MustCompile(`data-selection-key="[^"]*\.(Under|Over)_([0-9.]+)"`)

// totalGoalsSelectionKeyRegex matches Total_Goals in data-selection-key (e.g. Total_Goals2.Under_2.5, Total_Goals.Over_1.5)
var totalGoalsSelectionKeyRegex = regexp.MustCompile(`data-selection-key="[^"]*Total_Goals[^"]*\.(Under|Over)_(\d+\.?\d*)"`)

// matchHandicapSelectionKeyRegex matches To_Win_Match_With_Handicap*.HB_H or HB_A (home/away handicap)
var matchHandicapSelectionKeyRegex = regexp.MustCompile(`data-selection-key="[^"]*To_Win_Match_With_Handicap[^"]*\.(HB_H|HB_A)"`)

// totalCornersSelectionKeyRegex matches Total_Corners in data-selection-key (e.g. Total_Corners6.Under_6.5, Over_6.5)
var totalCornersSelectionKeyRegex = regexp.MustCompile(`data-selection-key="[^"]*Total_Corners[^"]*\.(Under|Over)_(\d+\.?\d*)"`)

// mostCornersSelectionKeyRegex matches Most_Corners.home / .draw / .away (кто подаст больше угловых — 1X2)
var mostCornersSelectionKeyRegex = regexp.MustCompile(`data-selection-key="[^"]*Most_Corners\.(home|draw|away)"`)

// mostCornersHandicapSelectionKeyRegex matches Most_Corners_With_Handicap*.HB_H or HB_A (угловые с учётом форы)
var mostCornersHandicapSelectionKeyRegex = regexp.MustCompile(`data-selection-key="[^"]*Most_Corners_With_Handicap[^"]*\.(HB_H|HB_A)"`)

// resultSelectionKeyRegex matches Result (1X2) in data-selection-key: Result.S_0_1 / .S_0_2 / .S_0_3 or .home / .draw / .away
var resultSelectionKeyRegex = regexp.MustCompile(`data-selection-key="[^"]*Result[^"]*\.(S_0_1|S_0_2|S_0_3|home|draw|away)"`)

// doubleChanceSelectionKeyRegex matches Double_Chance (1X, 12, X2) in data-selection-key
var doubleChanceSelectionKeyRegex = regexp.MustCompile(`data-selection-key="[^"]*Double_Chance[^"]*\.(S_1_1|S_1_2|S_1_3|1X|12|X2)"`)

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
		// Skip corners: main "Тотал угловых" (5.5–13.5) comes only from parseCornersTotalsFromSelectionKey.
		// Fallback here picks up wrong lines from other blocks (1, 1.5, 2, 7, 8, etc.).
		if mkt.eventType == models.StandardEventCorners {
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

// marketOdd represents a single odd with its market information
type marketOdd struct {
	marketType string  // RESULT, DOUBLE_CHANCE, HANDICAP, TOTAL, etc.
	mutableID  string  // e.g., S_0_1, S_1_2, etc.
	odds       float64
	param      string  // parameter for handicap/total (e.g., "0", "2.5")
	context    string  // HTML context around this odd
	position   int     // position in HTML
}

// marketGroup groups odds by market type and parameter
type marketGroup struct {
	marketType string
	param      string
	odds       []marketOdd
}

// preferenceMarket represents a market parsed by data-preference-id
type preferenceMarket struct {
	preferenceID string  // e.g., "MATCH_TOTALS_CORNERS_-1574381410"
	marketType   string  // "corners", "yellow_cards", etc.
	subType      string  // "totals", "handicap", "double_chance"
	param        string  // parameter value (e.g., "5.5", "6.5")
	outcomeType  string  // "over", "under", "home", "away", etc.
	odds         float64
	position     int
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
	start := time.Now()
	var totalMatches int
	defer func() {
		slog.Info("Marathonbet: цикл парсинга завершён", "matches", totalMatches, "duration", time.Since(start))
	}()

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
				// Strictly exclude live matches (matches that have already started)
				if !match.StartTime.IsZero() {
					matchStartTime := match.StartTime.UTC()
					now := time.Now().UTC()
					if !matchStartTime.After(now) {
						// Match has already started, skip it
						slog.Debug("Marathonbet: filtered live match", "match_id", match.ID, "start", matchStartTime.Format(time.RFC3339), "now", now.Format(time.RFC3339))
						continue
					}
				}
				health.AddMatch(match)
				totalMatches++
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

// parseDateTimeFromHTML extracts date and time from HTML page
// Tries nav-event-date (event page) then date-wrapper (league/coupon row)
func parseDateTimeFromHTML(htmlBody string) time.Time {
	matches := dateTimeRegex.FindStringSubmatch(htmlBody)
	if len(matches) < 2 {
		matches = dateWrapperRegex.FindStringSubmatch(htmlBody)
	}
	if len(matches) < 2 {
		return time.Time{}
	}
	dateTimeStr := strings.TrimSpace(matches[1])
	if dateTimeStr == "" {
		return time.Time{}
	}
	
	// Parse format "12 фев 23:00" (day month time)
	// Russian month names
	monthMap := map[string]string{
		"янв": "01", "фев": "02", "мар": "03", "апр": "04",
		"май": "05", "июн": "06", "июл": "07", "авг": "08",
		"сен": "09", "окт": "10", "ноя": "11", "дек": "12",
	}
	
	// Match pattern: "12 фев 23:00" or "12 фев 23:00" (with optional spaces)
	parts := strings.Fields(dateTimeStr)
	if len(parts) < 3 {
		return time.Time{}
	}
	
	day := parts[0]
	monthName := strings.ToLower(parts[1])
	timeStr := parts[2]
	
	month, ok := monthMap[monthName]
	if !ok {
		return time.Time{}
	}
	
	// Get current year (assume matches are in current or next year)
	now := time.Now()
	year := now.Year()
	
	// Parse time
	timeParts := strings.Split(timeStr, ":")
	if len(timeParts) != 2 {
		return time.Time{}
	}
	
	// Build date string in format "2006-01-02 15:04:05"
	// Parse day as integer to handle both "1" and "12" formats
	var dayInt int
	if _, err := fmt.Sscanf(day, "%d", &dayInt); err != nil {
		return time.Time{}
	}
	dateStr := fmt.Sprintf("%d-%s-%02d %s:00", year, month, dayInt, timeStr)
	
	// Parse with Moscow timezone (UTC+3)
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*60*60) // UTC+3
	}
	
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", dateStr, loc); err == nil {
		// If parsed date is in the past, try next year
		if t.Before(now.Add(-24 * time.Hour)) {
			year++
			dateStr = fmt.Sprintf("%d-%s-%02s %s:00", year, month, day, timeStr)
			if t, err := time.ParseInLocation("2006-01-02 15:04:05", dateStr, loc); err == nil {
				return t.UTC()
			}
		}
		return t.UTC()
	}
	
	return time.Time{}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// totalGoalsBySelectionKey returns map: param -> {Under, Over} odds from data-selection-key (block "Популярные" etc).
// These are the correct totals for 2.5, 3, 3.5 when the main table only has 1.5.
func parseTotalsFromSelectionKey(htmlBody string) map[string]struct{ Under, Over float64 } {
	out := make(map[string]struct{ Under, Over float64 })
	type pair struct{ outcome string; odds float64 }
	byParam := make(map[string][]pair)

	for _, sub := range totalGoalsSelectionKeyRegex.FindAllStringSubmatchIndex(htmlBody, -1) {
		outcome := htmlBody[sub[2]:sub[3]]   // Under or Over
		param := htmlBody[sub[4]:sub[5]]     // e.g. 2.5, 3
		keyPos := sub[0]

		cellStart := 0
		if before := htmlBody[:keyPos]; len(before) > 0 {
			tdMatches := openTdRegex.FindAllStringIndex(before, -1)
			if len(tdMatches) > 0 {
				cellStart = tdMatches[len(tdMatches)-1][0]
			}
		}
		cellEnd := min(len(htmlBody), keyPos+50)
		searchArea := htmlBody[cellStart:cellEnd]
		relKeyPos := keyPos - cellStart

		selMatches := dataSelRegex.FindAllStringSubmatchIndex(searchArea, -1)
		if len(selMatches) == 0 {
			continue
		}
		var selMatch []int
		for i := len(selMatches) - 1; i >= 0; i-- {
			if selMatches[i][1] <= relKeyPos {
				selMatch = selMatches[i]
				break
			}
		}
		if selMatch == nil {
			selMatch = selMatches[0]
		}
		raw := ""
		if selMatch[2] != -1 {
			raw = searchArea[selMatch[2]:selMatch[3]]
		} else if selMatch[4] != -1 {
			raw = searchArea[selMatch[4]:selMatch[5]]
		}
		if raw == "" {
			continue
		}
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Epr <= 0 {
			continue
		}
		byParam[param] = append(byParam[param], pair{outcome: outcome, odds: s.Epr})
	}

	for param, pairs := range byParam {
		var under, over float64
		// Use first Under/Over seen per param so we take the main totals block, not "3 исхода" or other sections
		for _, p := range pairs {
			if strings.EqualFold(p.outcome, "Under") && under == 0 {
				under = p.odds
			} else if strings.EqualFold(p.outcome, "Over") && over == 0 {
				over = p.odds
			}
		}
		if under > 0 && over > 0 {
			out[param] = struct{ Under, Over float64 }{Under: under, Over: over}
		}
	}
	return out
}

// parseResultFromSelectionKey returns odds for 1, X, 2 from data-selection-key (Result.S_0_1/.S_0_2/.S_0_3 or .home/.draw/.away).
// Returns (odds1, oddsX, odds2); if any is 0 the result is incomplete.
func parseResultFromSelectionKey(htmlBody string) (odds1, oddsX, odds2 float64) {
	type keyOdds struct{ outcome string; odds float64 }
	var byOutcome []keyOdds
	for _, sub := range resultSelectionKeyRegex.FindAllStringSubmatchIndex(htmlBody, -1) {
		outcome := htmlBody[sub[2]:sub[3]]
		keyPos := sub[0]
		cellStart := 0
		if before := htmlBody[:keyPos]; len(before) > 0 {
			if tdMatches := openTdRegex.FindAllStringIndex(before, -1); len(tdMatches) > 0 {
				cellStart = tdMatches[len(tdMatches)-1][0]
			} else {
				cellStart = max(0, keyPos-200)
			}
		}
		searchArea := htmlBody[cellStart:min(len(htmlBody), keyPos+80)]
		relKeyPos := keyPos - cellStart
		selMatches := dataSelRegex.FindAllStringSubmatchIndex(searchArea, -1)
		if len(selMatches) == 0 {
			continue
		}
		var selMatch []int
		for i := len(selMatches) - 1; i >= 0; i-- {
			if selMatches[i][1] <= relKeyPos {
				selMatch = selMatches[i]
				break
			}
		}
		if selMatch == nil {
			selMatch = selMatches[0]
		}
		raw := ""
		if selMatch[2] != -1 {
			raw = searchArea[selMatch[2]:selMatch[3]]
		} else if selMatch[4] != -1 {
			raw = searchArea[selMatch[4]:selMatch[5]]
		}
		if raw == "" {
			continue
		}
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Epr <= 0 {
			continue
		}
		byOutcome = append(byOutcome, keyOdds{outcome: outcome, odds: s.Epr})
	}
	// Map to 1, X, 2 (order: first outcome = 1, second = X, third = 2)
	is1 := func(s string) bool { return s == "S_0_1" || strings.EqualFold(s, "home") }
	isX := func(s string) bool { return s == "S_0_2" || strings.EqualFold(s, "draw") }
	is2 := func(s string) bool { return s == "S_0_3" || strings.EqualFold(s, "away") }
	for _, p := range byOutcome {
		if is1(p.outcome) && odds1 == 0 {
			odds1 = p.odds
		} else if isX(p.outcome) && oddsX == 0 {
			oddsX = p.odds
		} else if is2(p.outcome) && odds2 == 0 {
			odds2 = p.odds
		}
	}
	return odds1, oddsX, odds2
}

// parseDoubleChanceFromSelectionKey returns odds for 1X, 12, X2 from data-selection-key.
// Returns (odds1X, odds12, oddsX2); if any is 0 the result is incomplete.
func parseDoubleChanceFromSelectionKey(htmlBody string) (odds1X, odds12, oddsX2 float64) {
	type keyOdds struct{ outcome string; odds float64 }
	var byOutcome []keyOdds
	for _, sub := range doubleChanceSelectionKeyRegex.FindAllStringSubmatchIndex(htmlBody, -1) {
		outcome := htmlBody[sub[2]:sub[3]]
		keyPos := sub[0]
		cellStart := 0
		if before := htmlBody[:keyPos]; len(before) > 0 {
			if tdMatches := openTdRegex.FindAllStringIndex(before, -1); len(tdMatches) > 0 {
				cellStart = tdMatches[len(tdMatches)-1][0]
			} else {
				cellStart = max(0, keyPos-200)
			}
		}
		searchArea := htmlBody[cellStart:min(len(htmlBody), keyPos+80)]
		relKeyPos := keyPos - cellStart
		selMatches := dataSelRegex.FindAllStringSubmatchIndex(searchArea, -1)
		if len(selMatches) == 0 {
			continue
		}
		var selMatch []int
		for i := len(selMatches) - 1; i >= 0; i-- {
			if selMatches[i][1] <= relKeyPos {
				selMatch = selMatches[i]
				break
			}
		}
		if selMatch == nil {
			selMatch = selMatches[0]
		}
		raw := ""
		if selMatch[2] != -1 {
			raw = searchArea[selMatch[2]:selMatch[3]]
		} else if selMatch[4] != -1 {
			raw = searchArea[selMatch[4]:selMatch[5]]
		}
		if raw == "" {
			continue
		}
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Epr <= 0 {
			continue
		}
		byOutcome = append(byOutcome, keyOdds{outcome: outcome, odds: s.Epr})
	}
	is1X := func(s string) bool { return s == "S_1_1" || s == "1X" }
	is12 := func(s string) bool { return s == "S_1_2" || s == "12" }
	isX2 := func(s string) bool { return s == "S_1_3" || s == "X2" }
	for _, p := range byOutcome {
		if is1X(p.outcome) && odds1X == 0 {
			odds1X = p.odds
		} else if is12(p.outcome) && odds12 == 0 {
			odds12 = p.odds
		} else if isX2(p.outcome) && oddsX2 == 0 {
			oddsX2 = p.odds
		}
	}
	return odds1X, odds12, oddsX2
}

// handicapLine from selection-key: lineKey -> homeParam, awayParam, homeOdds, awayOdds
type handicapLine struct {
	LineKey   string
	HomeParam string
	AwayParam string
	HomeOdds  float64
	AwayOdds  float64
}

// parseHandicapsFromSelectionKey returns handicap lines from To_Win_Match_With_Handicap*.HB_H / HB_A (block "Популярные").
func parseHandicapsFromSelectionKey(htmlBody string) []handicapLine {
	type keyOdds struct {
		side string // HB_H or HB_A
		param string
		odds float64
	}
	byPrefix := make(map[string][]keyOdds) // "To_Win_Match_With_Handicap" or "To_Win_Match_With_Handicap0" etc.

	for _, sub := range matchHandicapSelectionKeyRegex.FindAllStringSubmatchIndex(htmlBody, -1) {
		side := htmlBody[sub[2]:sub[3]]
		keyPos := sub[0]
		cellStart := 0
		if before := htmlBody[:keyPos]; len(before) > 0 {
			tdMatches := openTdRegex.FindAllStringIndex(before, -1)
			if len(tdMatches) > 0 {
				cellStart = tdMatches[len(tdMatches)-1][0]
			}
		}
		cellEnd := min(len(htmlBody), keyPos+50)
		searchArea := htmlBody[cellStart:cellEnd]
		relKeyPos := keyPos - cellStart
		selMatches := dataSelRegex.FindAllStringSubmatchIndex(searchArea, -1)
		if len(selMatches) == 0 {
			continue
		}
		var selMatch []int
		for i := len(selMatches) - 1; i >= 0; i-- {
			if selMatches[i][1] <= relKeyPos {
				selMatch = selMatches[i]
				break
			}
		}
		if selMatch == nil {
			selMatch = selMatches[0]
		}
		raw := ""
		if selMatch[2] != -1 {
			raw = searchArea[selMatch[2]:selMatch[3]]
		} else if selMatch[4] != -1 {
			raw = searchArea[selMatch[4]:selMatch[5]]
		}
		if raw == "" {
			continue
		}
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Epr <= 0 {
			continue
		}
		// Extract key prefix (To_Win_Match_With_Handicap or To_Win_Match_With_Handicap0, ...)
		keyStart := strings.Index(htmlBody[sub[0]:sub[1]], "To_Win_Match_With_Handicap")
		if keyStart < 0 {
			continue
		}
		keyStr := htmlBody[sub[0]+keyStart:sub[1]]
		dot := strings.Index(keyStr, ".")
		if dot > 0 {
			keyStr = keyStr[:dot]
		}
		// Param from context: (-1.5), (0), (+1.5) etc.
		ctxStart := max(0, cellStart-80)
		ctx := htmlBody[ctxStart:cellEnd]
		param := ""
		if matches := handicapParamRegex.FindStringSubmatch(ctx); len(matches) > 1 {
			param = matches[1]
		}
		byPrefix[keyStr] = append(byPrefix[keyStr], keyOdds{side: side, param: param, odds: s.Epr})
	}

	var lines []handicapLine
	for _, pairs := range byPrefix {
		var homeParam, awayParam string
		var homeOdds, awayOdds float64
		for _, p := range pairs {
			if p.side == "HB_H" {
				homeParam = p.param
				if homeOdds == 0 {
					homeOdds = p.odds
				}
			} else {
				awayParam = p.param
				if awayOdds == 0 {
					awayOdds = p.odds
				}
			}
		}
		if homeOdds > 0 && awayOdds > 0 && homeParam != "" && awayParam != "" {
			lineKey := strings.TrimPrefix(strings.TrimPrefix(homeParam, "+"), "-")
			lines = append(lines, handicapLine{
				LineKey:   lineKey,
				HomeParam: homeParam,
				AwayParam: awayParam,
				HomeOdds:  homeOdds,
				AwayOdds:  awayOdds,
			})
		}
	}
	return lines
}

// parseCornersTotalsFromSelectionKey returns Total_Corners Under/Over by param (6.5, 7.5, ... 14.5); first occurrence per param.
func parseCornersTotalsFromSelectionKey(htmlBody string) map[string]struct{ Under, Over float64 } {
	out := make(map[string]struct{ Under, Over float64 })
	type pair struct {
		outcome string
		odds    float64
	}
	byParam := make(map[string][]pair)
	for _, sub := range totalCornersSelectionKeyRegex.FindAllStringSubmatchIndex(htmlBody, -1) {
		outcome := htmlBody[sub[2]:sub[3]]
		param := htmlBody[sub[4]:sub[5]]
		keyPos := sub[0]
		cellStart := 0
		if before := htmlBody[:keyPos]; len(before) > 0 {
			tdMatches := openTdRegex.FindAllStringIndex(before, -1)
			if len(tdMatches) > 0 {
				cellStart = tdMatches[len(tdMatches)-1][0]
			}
		}
		cellEnd := min(len(htmlBody), keyPos+50)
		searchArea := htmlBody[cellStart:cellEnd]
		relKeyPos := keyPos - cellStart
		selMatches := dataSelRegex.FindAllStringSubmatchIndex(searchArea, -1)
		if len(selMatches) == 0 {
			continue
		}
		var selMatch []int
		for i := len(selMatches) - 1; i >= 0; i-- {
			if selMatches[i][1] <= relKeyPos {
				selMatch = selMatches[i]
				break
			}
		}
		if selMatch == nil {
			selMatch = selMatches[0]
		}
		raw := ""
		if selMatch[2] != -1 {
			raw = searchArea[selMatch[2]:selMatch[3]]
		} else if selMatch[4] != -1 {
			raw = searchArea[selMatch[4]:selMatch[5]]
		}
		if raw == "" {
			continue
		}
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Epr <= 0 {
			continue
		}
		byParam[param] = append(byParam[param], pair{outcome: outcome, odds: s.Epr})
	}
	for param, pairs := range byParam {
		// Only main "Тотал угловых" block has lines 5.5–13.5 (Меньше/Больше). Other blocks
		// (3 исхода, Т1/Т2, таймы) add 1, 1.5, 2, 7, 8, 9, 10, etc. — skip them.
		if !strings.Contains(param, ".5") {
			continue
		}
		var pVal float64
		if _, err := fmt.Sscanf(param, "%f", &pVal); err != nil || pVal < 5.5 || pVal > 13.5 {
			continue
		}
		var under, over float64
		for _, p := range pairs {
			if strings.EqualFold(p.outcome, "Under") && under == 0 {
				under = p.odds
			} else if strings.EqualFold(p.outcome, "Over") && over == 0 {
				over = p.odds
			}
		}
		if under > 0 && over > 0 {
			out[param] = struct{ Under, Over float64 }{Under: under, Over: over}
		}
	}
	return out
}

// parseMostCorners1X2FromSelectionKey returns home/draw/away odds for "Кто подаст больше угловых" (Most_Corners.home/.draw/.away).
func parseMostCorners1X2FromSelectionKey(htmlBody string) (home, draw, away float64) {
	byOutcome := make(map[string]float64)
	for _, sub := range mostCornersSelectionKeyRegex.FindAllStringSubmatchIndex(htmlBody, -1) {
		outcome := htmlBody[sub[2]:sub[3]]
		keyPos := sub[0]
		cellStart := 0
		if before := htmlBody[:keyPos]; len(before) > 0 {
			tdMatches := openTdRegex.FindAllStringIndex(before, -1)
			if len(tdMatches) > 0 {
				cellStart = tdMatches[len(tdMatches)-1][0]
			}
		}
		cellEnd := min(len(htmlBody), keyPos+50)
		searchArea := htmlBody[cellStart:cellEnd]
		relKeyPos := keyPos - cellStart
		selMatches := dataSelRegex.FindAllStringSubmatchIndex(searchArea, -1)
		if len(selMatches) == 0 {
			continue
		}
		var selMatch []int
		for i := len(selMatches) - 1; i >= 0; i-- {
			if selMatches[i][1] <= relKeyPos {
				selMatch = selMatches[i]
				break
			}
		}
		if selMatch == nil {
			selMatch = selMatches[0]
		}
		raw := ""
		if selMatch[2] != -1 {
			raw = searchArea[selMatch[2]:selMatch[3]]
		} else if selMatch[4] != -1 {
			raw = searchArea[selMatch[4]:selMatch[5]]
		}
		if raw == "" {
			continue
		}
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Epr <= 0 {
			continue
		}
		if byOutcome[outcome] == 0 {
			byOutcome[outcome] = s.Epr
		}
	}
	return byOutcome["home"], byOutcome["draw"], byOutcome["away"]
}

// parseCornersHandicapsFromSelectionKey returns handicap lines for "Угловые с учётом форы" (Most_Corners_With_Handicap*.HB_H/HB_A), excluding 1st/2nd half.
func parseCornersHandicapsFromSelectionKey(htmlBody string) []handicapLine {
	type keyOdds struct {
		side  string
		param string
		odds  float64
	}
	byPrefix := make(map[string][]keyOdds)
	for _, sub := range mostCornersHandicapSelectionKeyRegex.FindAllStringSubmatchIndex(htmlBody, -1) {
		fullKey := htmlBody[sub[0]:sub[1]]
		if strings.Contains(fullKey, "1st_Half") || strings.Contains(fullKey, "2nd_Half") {
			continue
		}
		side := htmlBody[sub[2]:sub[3]]
		keyPos := sub[0]
		cellStart := 0
		if before := htmlBody[:keyPos]; len(before) > 0 {
			tdMatches := openTdRegex.FindAllStringIndex(before, -1)
			if len(tdMatches) > 0 {
				cellStart = tdMatches[len(tdMatches)-1][0]
			}
		}
		cellEnd := min(len(htmlBody), keyPos+50)
		searchArea := htmlBody[cellStart:cellEnd]
		relKeyPos := keyPos - cellStart
		selMatches := dataSelRegex.FindAllStringSubmatchIndex(searchArea, -1)
		if len(selMatches) == 0 {
			continue
		}
		var selMatch []int
		for i := len(selMatches) - 1; i >= 0; i-- {
			if selMatches[i][1] <= relKeyPos {
				selMatch = selMatches[i]
				break
			}
		}
		if selMatch == nil {
			selMatch = selMatches[0]
		}
		raw := ""
		if selMatch[2] != -1 {
			raw = searchArea[selMatch[2]:selMatch[3]]
		} else if selMatch[4] != -1 {
			raw = searchArea[selMatch[4]:selMatch[5]]
		}
		if raw == "" {
			continue
		}
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil || s.Epr <= 0 {
			continue
		}
		keyStart := strings.Index(fullKey, "Most_Corners_With_Handicap")
		if keyStart < 0 {
			continue
		}
		keyStr := fullKey[keyStart:]
		if dot := strings.Index(keyStr, "."); dot > 0 {
			keyStr = keyStr[:dot]
		}
		ctxStart := max(0, cellStart-80)
		ctx := htmlBody[ctxStart:cellEnd]
		param := ""
		if matches := handicapParamRegex.FindStringSubmatch(ctx); len(matches) > 1 {
			param = matches[1]
		}
		byPrefix[keyStr] = append(byPrefix[keyStr], keyOdds{side: side, param: param, odds: s.Epr})
	}
	var lines []handicapLine
	for _, pairs := range byPrefix {
		var homeParam, awayParam string
		var homeOdds, awayOdds float64
		for _, p := range pairs {
			if p.side == "HB_H" {
				homeParam = p.param
				if homeOdds == 0 {
					homeOdds = p.odds
				}
			} else {
				awayParam = p.param
				if awayOdds == 0 {
					awayOdds = p.odds
				}
			}
		}
		if homeOdds > 0 && awayOdds > 0 && homeParam != "" && awayParam != "" {
			lineKey := strings.TrimPrefix(strings.TrimPrefix(homeParam, "+"), "-")
			lines = append(lines, handicapLine{
				LineKey:   lineKey,
				HomeParam: homeParam,
				AwayParam: awayParam,
				HomeOdds:  homeOdds,
				AwayOdds:  awayOdds,
			})
		}
	}
	return lines
}

// parseMarketsByType extracts all markets from HTML using data-market-type attribute
func parseMarketsByType(htmlBody string) []marketOdd {
	var markets []marketOdd
	
	// Find all elements with data-market-type and nearby data-sel
	// Pattern: look for data-market-type, then find nearest data-sel within reasonable distance
	marketTypeMatches := marketTypeRegex.FindAllStringSubmatchIndex(htmlBody, -1)
	
	for _, mtMatch := range marketTypeMatches {
		marketType := htmlBody[mtMatch[2]:mtMatch[3]]
		startPos := mtMatch[0]
		
		// Find data-mutable-id nearby
		mutableID := ""
		mutableIDMatch := mutableIdRegex.FindStringSubmatchIndex(htmlBody[max(0, startPos-100):startPos+100])
		if len(mutableIDMatch) >= 3 {
			mutableID = htmlBody[max(0, startPos-100)+mutableIDMatch[2]:max(0, startPos-100)+mutableIDMatch[3]]
		}
		
		// Find data-sel in the same cell. On Marathonbet, data-sel is in the same <td> as data-market-type (often before it).
		// If no <td> (e.g. event page "Основные" in divs), search in a window before data-market-type.
		cellStart := 0
		if before := htmlBody[:startPos]; len(before) > 0 {
			tdMatches := openTdRegex.FindAllStringIndex(before, -1)
			if len(tdMatches) > 0 {
				cellStart = tdMatches[len(tdMatches)-1][0]
			} else {
				cellStart = max(0, startPos-450)
			}
		}
		cellEnd := min(len(htmlBody), startPos+200)
		searchArea := htmlBody[cellStart:cellEnd]
		relStartPos := startPos - cellStart
		
		selMatches := dataSelRegex.FindAllStringSubmatchIndex(searchArea, -1)
		if len(selMatches) == 0 {
			continue
		}
		
		// Prefer data-sel that ends before or at relStartPos (same cell); otherwise take first after relStartPos
		var selMatch []int
		for i := len(selMatches) - 1; i >= 0; i-- {
			if selMatches[i][1] <= relStartPos {
				selMatch = selMatches[i]
				break
			}
		}
		if selMatch == nil {
			selMatch = selMatches[0]
		}
		raw := ""
		if selMatch[2] != -1 {
			raw = searchArea[selMatch[2]:selMatch[3]]
		} else if selMatch[4] != -1 {
			raw = searchArea[selMatch[4]:selMatch[5]]
		}
		if raw == "" {
			continue
		}
		
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			continue
		}
		if s.Epr <= 0 {
			continue
		}
		
		// Get context around this market (300 chars before)
		contextStart := max(0, startPos-300)
		context := htmlBody[contextStart:startPos]
		
		// Extract parameter based on market type
		// Also search in the element itself (after data-market-type)
		elementArea := htmlBody[startPos:min(len(htmlBody), startPos+200)]
		fullContext := context + elementArea
		
		param := ""
		if marketType == "HANDICAP" {
			if matches := handicapParamRegex.FindStringSubmatch(fullContext); len(matches) > 1 {
				param = matches[1]
			}
		} else if marketType == "TOTAL" {
			if matches := totalParamRegex.FindStringSubmatch(fullContext); len(matches) > 1 {
				param = matches[1]
			} else {
				// Try to find in data-selection-key if available
				selectionKeyRegex := regexp.MustCompile(`data-selection-key="[^"]*\.(Under|Over)_(\d+\.?\d*)"`)
				if matches := selectionKeyRegex.FindStringSubmatch(fullContext); len(matches) > 2 {
					param = matches[2]
				} else {
					// Default to 2.5 for main match total
					param = "2.5"
				}
			}
		}
		
		markets = append(markets, marketOdd{
			marketType: marketType,
			mutableID:  mutableID,
			odds:       s.Epr,
			param:      param,
			context:    context,
			position:   startPos,
		})
	}
	
	return markets
}

// parseMarketsByPreferenceID extracts markets using data-preference-id (for corners, yellow cards, etc.)
func parseMarketsByPreferenceID(htmlBody string) []preferenceMarket {
	var markets []preferenceMarket
	
	// Find all data-preference-id blocks
	prefMatches := preferenceIdRegex.FindAllStringSubmatchIndex(htmlBody, -1)
	
	for _, prefMatch := range prefMatches {
		preferenceID := htmlBody[prefMatch[2]:prefMatch[3]]
		startPos := prefMatch[0]
		
		// Determine market type from preference ID
		marketType := ""
		subType := ""
		prefLower := strings.ToLower(preferenceID)
		
		if strings.Contains(prefLower, "corner") {
			marketType = "corners"
			if strings.Contains(prefLower, "total") || strings.Contains(prefLower, "totals") {
				subType = "totals"
			} else if strings.Contains(prefLower, "handicap") {
				subType = "handicap"
			} else if strings.Contains(prefLower, "double_chance") || strings.Contains(prefLower, "doble_chance") {
				subType = "double_chance"
			} else {
				subType = "totals" // default for corners
			}
		} else if strings.Contains(prefLower, "yellow") || strings.Contains(prefLower, "card") {
			marketType = "yellow_cards"
			if strings.Contains(prefLower, "total") || strings.Contains(prefLower, "totals") {
				subType = "totals"
			} else if strings.Contains(prefLower, "handicap") {
				subType = "handicap"
			} else {
				subType = "totals" // default
			}
		} else if strings.Contains(prefLower, "foul") {
			marketType = "fouls"
			subType = "totals"
		} else {
			continue // Skip unknown market types
		}
		
		// Find all data-sel within this preference block (within 5000 chars)
		searchStart := startPos
		searchEnd := min(len(htmlBody), startPos+5000)
		searchArea := htmlBody[searchStart:searchEnd]
		
		// Find next preference-id or end of block
		nextPrefMatch := preferenceIdRegex.FindStringSubmatchIndex(searchArea[100:])
		if len(nextPrefMatch) > 0 {
			searchEnd = searchStart + 100 + nextPrefMatch[0]
			searchArea = htmlBody[searchStart:searchEnd]
		}
		
		// Find all data-sel in this block
		selMatches := dataSelRegex.FindAllStringSubmatchIndex(searchArea, -1)
		
		for _, selMatch := range selMatches {
			raw := ""
			if selMatch[2] != -1 {
				raw = searchArea[selMatch[2]:selMatch[3]]
			} else if selMatch[4] != -1 {
				raw = searchArea[selMatch[4]:selMatch[5]]
			}
			if raw == "" {
				continue
			}
			
			raw = html.UnescapeString(raw)
			var s selJSON
			if err := json.Unmarshal([]byte(raw), &s); err != nil {
				continue
			}
			if s.Epr <= 0 {
				continue
			}
			
			// Get context around this selection (200 chars before and after)
			selPos := searchStart + selMatch[0]
			contextStart := max(0, selPos-200)
			contextEnd := min(len(htmlBody), selPos+200)
			context := htmlBody[contextStart:contextEnd]
			
			// Extract parameter and outcome type
			param := ""
			outcomeType := ""
			
			// Try to extract from data-selection-key first
			keyMatch := selectionKeyRegex.FindStringSubmatch(context)
			if len(keyMatch) >= 3 {
				outcomeType = strings.ToLower(keyMatch[1]) // "under" or "over"
				param = keyMatch[2]
			} else {
				// Fallback: extract from context (e.g., "(5.5)")
				paramMatch := totalParamRegex.FindStringSubmatch(context)
				if len(paramMatch) > 1 {
					param = paramMatch[1]
				}
				
				// Determine outcome type from context
				contextLower := strings.ToLower(context)
				if strings.Contains(contextLower, "under") || strings.Contains(contextLower, "меньше") {
					outcomeType = "under"
				} else if strings.Contains(contextLower, "over") || strings.Contains(contextLower, "больше") {
					outcomeType = "over"
				}
			}
			
			if param == "" {
				continue // Skip if we can't determine parameter
			}
			
			markets = append(markets, preferenceMarket{
				preferenceID: preferenceID,
				marketType:   marketType,
				subType:      subType,
				param:        param,
				outcomeType:  outcomeType,
				odds:         s.Epr,
				position:     selPos,
			})
		}
	}
	
	return markets
}

// parseEventPage extracts event info and odds from event HTML, builds Match.
// Parses: Основные (результат 1X2, двойной шанс 1X/12/X2), Форы (все линии), Тоталы (все линии голов),
// при наличии вкладки угловые — тотал угловых, кто больше угловых, угловые с учётом форы.
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
	
	// If time not found in JSON, try to parse from HTML
	if startTime.IsZero() {
		startTime = parseDateTimeFromHTML(bodyStr)
		if !startTime.IsZero() {
			slog.Debug("Marathonbet: parsed start time from HTML", "time", startTime.Format(time.RFC3339))
		} else {
			slog.Warn("Marathonbet: could not parse start time from JSON or HTML", "event_path", eventPath)
		}
	}

	// Parse markets by type using data-market-type attribute
	markets := parseMarketsByType(bodyStr)
	
	if len(markets) == 0 {
		return nil, fmt.Errorf("no markets found")
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

	// Group markets by type and parameter
	marketsByType := make(map[string][]marketOdd)
	for _, m := range markets {
		key := m.marketType + ":" + m.param + ":" + m.mutableID
		marketsByType[key] = append(marketsByType[key], m)
	}

	// Parse RESULT market (1X2) — Основные: результат
	resultMarkets := []marketOdd{}
	for _, m := range markets {
		if m.marketType == "RESULT" {
			resultMarkets = append(resultMarkets, m)
		}
	}
	var odds1, oddsX, odds2 float64
	if len(resultMarkets) >= 3 {
		sort.Slice(resultMarkets, func(i, j int) bool { return resultMarkets[i].mutableID < resultMarkets[j].mutableID })
		odds1, oddsX, odds2 = resultMarkets[0].odds, resultMarkets[1].odds, resultMarkets[2].odds
	} else if o1, oX, o2 := parseResultFromSelectionKey(bodyStr); o1 > 0 && oX > 0 && o2 > 0 {
		odds1, oddsX, odds2 = o1, oX, o2
	}
	if odds1 > 0 && oddsX > 0 && odds2 > 0 {
		mainEventID := matchID + "_" + bookmakerKey + "_" + string(models.StandardEventMainMatch)
		match.Events = append(match.Events, models.Event{
			ID:         mainEventID,
			MatchID:    matchID,
			EventType:  string(models.StandardEventMainMatch),
			MarketName: models.GetMarketName(models.StandardEventMainMatch),
			Bookmaker:  bookmakerName,
			Outcomes: []models.Outcome{
				{ID: mainEventID + "_1", EventID: mainEventID, OutcomeType: string(models.OutcomeTypeHomeWin), Odds: odds1, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: mainEventID + "_X", EventID: mainEventID, OutcomeType: string(models.OutcomeTypeDraw), Odds: oddsX, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: mainEventID + "_2", EventID: mainEventID, OutcomeType: string(models.OutcomeTypeAwayWin), Odds: odds2, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	// Parse DOUBLE_CHANCE market (1X, 12, X2) — Основные: двойной шанс
	doubleChanceMarkets := []marketOdd{}
	for _, m := range markets {
		if m.marketType == "DOUBLE_CHANCE" {
			doubleChanceMarkets = append(doubleChanceMarkets, m)
		}
	}
	var dc1X, dc12, dcX2 float64
	if len(doubleChanceMarkets) >= 3 {
		sort.Slice(doubleChanceMarkets, func(i, j int) bool { return doubleChanceMarkets[i].mutableID < doubleChanceMarkets[j].mutableID })
		dc1X, dc12, dcX2 = doubleChanceMarkets[0].odds, doubleChanceMarkets[1].odds, doubleChanceMarkets[2].odds
	}
	if dc1X == 0 || dc12 == 0 || dcX2 == 0 {
		dc1X, dc12, dcX2 = parseDoubleChanceFromSelectionKey(bodyStr)
	}
	if dc1X > 0 && dc12 > 0 && dcX2 > 0 {
		dcEventID := matchID + "_" + bookmakerKey + "_double_chance"
		match.Events = append(match.Events, models.Event{
			ID:         dcEventID,
			MatchID:    matchID,
			EventType:  "double_chance",
			MarketName: "Double Chance",
			Bookmaker:  bookmakerName,
			Outcomes: []models.Outcome{
				{ID: dcEventID + "_1X", EventID: dcEventID, OutcomeType: "double_chance_1x", Odds: dc1X, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: dcEventID + "_12", EventID: dcEventID, OutcomeType: "double_chance_12", Odds: dc12, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: dcEventID + "_X2", EventID: dcEventID, OutcomeType: "double_chance_x2", Odds: dcX2, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	// Parse HANDICAP markets: each line is home (-X) vs away (+X), group by line key (abs value)
	handicapMarkets := []marketOdd{}
	for _, m := range markets {
		if m.marketType == "HANDICAP" {
			handicapMarkets = append(handicapMarkets, m)
		}
	}
	addedHandicapLines := make(map[string]bool)
	if len(handicapMarkets) >= 2 {
		sort.Slice(handicapMarkets, func(i, j int) bool {
			return handicapMarkets[i].mutableID < handicapMarkets[j].mutableID
		})
		lineKey := func(param string) string {
			s := strings.TrimPrefix(strings.TrimSpace(param), "+")
			s = strings.TrimPrefix(s, "-")
			return s
		}
		handicapsByLine := make(map[string][]marketOdd)
		for _, m := range handicapMarkets {
			key := lineKey(m.param)
			handicapsByLine[key] = append(handicapsByLine[key], m)
		}
		for line, hMarkets := range handicapsByLine {
			if len(hMarkets) < 2 {
				continue
			}
			sort.Slice(hMarkets, func(i, j int) bool { return hMarkets[i].mutableID < hMarkets[j].mutableID })
			homeParam := hMarkets[0].param
			awayParam := hMarkets[1].param
			paramLabel := homeParam + " / " + awayParam
			handicapEventID := matchID + "_" + bookmakerKey + "_handicap_" + strings.ReplaceAll(line, ".", "_")
			handicapEvent := models.Event{
				ID:         handicapEventID,
				MatchID:    matchID,
				EventType:  "handicap",
				MarketName: "Handicap " + paramLabel,
				Bookmaker:  bookmakerName,
				Outcomes: []models.Outcome{
					{ID: handicapEventID + "_home", EventID: handicapEventID, OutcomeType: "handicap_home", Parameter: homeParam, Odds: hMarkets[0].odds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
					{ID: handicapEventID + "_away", EventID: handicapEventID, OutcomeType: "handicap_away", Parameter: awayParam, Odds: hMarkets[1].odds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				},
				CreatedAt: now,
				UpdatedAt: now,
			}
			match.Events = append(match.Events, handicapEvent)
			addedHandicapLines[line] = true
		}
	}
	for _, h := range parseHandicapsFromSelectionKey(bodyStr) {
		if addedHandicapLines[h.LineKey] {
			continue
		}
		addedHandicapLines[h.LineKey] = true
		paramLabel := h.HomeParam + " / " + h.AwayParam
		handicapEventID := matchID + "_" + bookmakerKey + "_handicap_sk_" + strings.ReplaceAll(h.LineKey, ".", "_")
		handicapEvent := models.Event{
			ID:         handicapEventID,
			MatchID:    matchID,
			EventType:  "handicap",
			MarketName: "Handicap " + paramLabel,
			Bookmaker:  bookmakerName,
			Outcomes: []models.Outcome{
				{ID: handicapEventID + "_home", EventID: handicapEventID, OutcomeType: "handicap_home", Parameter: h.HomeParam, Odds: h.HomeOdds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: handicapEventID + "_away", EventID: handicapEventID, OutcomeType: "handicap_away", Parameter: h.AwayParam, Odds: h.AwayOdds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		match.Events = append(match.Events, handicapEvent)
	}

	// Parse TOTAL markets: prefer totals from data-selection-key (block "Популярные") — correct 2.5, 3, 3.5; merge with main table
	selectionKeyTotals := parseTotalsFromSelectionKey(bodyStr)
	totalsByParam := make(map[string][]marketOdd)
	for param, odds := range selectionKeyTotals {
		totalsByParam[param] = []marketOdd{
			{param: param, odds: odds.Under},
			{param: param, odds: odds.Over},
		}
	}
	totalMarkets := []marketOdd{}
	for _, m := range markets {
		if m.marketType == "TOTAL" {
			totalMarkets = append(totalMarkets, m)
		}
	}
	for _, m := range totalMarkets {
		param := m.param
		if param == "" {
			param = "2.5"
		}
		if _, has := totalsByParam[param]; !has {
			totalsByParam[param] = append(totalsByParam[param], m)
		}
	}
	if len(totalsByParam) > 0 {
		for param, tMarkets := range totalsByParam {
			if len(tMarkets) < 2 {
				continue
			}
			// For main-table (mutableID set), sort by mutableID to get Under then Over; don't swap selection-key order
			underOdds := tMarkets[0].odds
			overOdds := tMarkets[1].odds
			if len(tMarkets) > 2 || (len(tMarkets) == 2 && tMarkets[0].mutableID != "") {
				sort.Slice(tMarkets, func(i, j int) bool { return tMarkets[i].mutableID < tMarkets[j].mutableID })
				underOdds, overOdds = tMarkets[0].odds, tMarkets[1].odds
			}
			totalEventID := matchID + "_" + bookmakerKey + "_total_" + strings.ReplaceAll(param, ".", "_")
			totalEvent := models.Event{
				ID:         totalEventID,
				MatchID:    matchID,
				EventType:  string(models.StandardEventMainMatch),
				MarketName: "Total " + param,
				Bookmaker:  bookmakerName,
				Outcomes: []models.Outcome{
					{ID: totalEventID + "_under", EventID: totalEventID, OutcomeType: string(models.OutcomeTypeTotalUnder), Parameter: param, Odds: underOdds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
					{ID: totalEventID + "_over", EventID: totalEventID, OutcomeType: string(models.OutcomeTypeTotalOver), Parameter: param, Odds: overOdds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				},
				CreatedAt: now,
				UpdatedAt: now,
			}
			match.Events = append(match.Events, totalEvent)
		}
	}

	// Parse markets by preference-id (corners, yellow cards, etc.)
	prefMarkets := parseMarketsByPreferenceID(bodyStr)
	
	// Group preference markets by type, subtype, and parameter
	prefMarketsByKey := make(map[string][]preferenceMarket)
	for _, pm := range prefMarkets {
		key := pm.marketType + ":" + pm.subType + ":" + pm.param
		prefMarketsByKey[key] = append(prefMarketsByKey[key], pm)
	}
	
	// Process preference markets
	for _, pMarkets := range prefMarketsByKey {
		if len(pMarkets) < 2 {
			continue
		}
		// Skip corners totals from preference-id: main "Тотал угловых" (5.5–13.5) comes only from
		// parseCornersTotalsFromSelectionKey. Preference blocks include "3 исхода", T1/T2, halves,
		// which add wrong lines (1, 1.5, 2, 7, 8, 9, 10, etc.).
		if pMarkets[0].marketType == "corners" && pMarkets[0].subType == "totals" {
			continue
		}
		
		// Group by outcome type (over/under pairs)
		overMarkets := []preferenceMarket{}
		underMarkets := []preferenceMarket{}
		for _, pm := range pMarkets {
			if pm.outcomeType == "over" {
				overMarkets = append(overMarkets, pm)
			} else if pm.outcomeType == "under" {
				underMarkets = append(underMarkets, pm)
			}
		}
		
		// Create events for over/under pairs
		if len(overMarkets) > 0 && len(underMarkets) > 0 {
			// Take first over and under for this parameter
			overMarket := overMarkets[0]
			underMarket := underMarkets[0]
			
			// Determine event type
			var eventType models.StandardEventType
			switch overMarket.marketType {
			case "corners":
				eventType = models.StandardEventCorners
			case "yellow_cards":
				eventType = models.StandardEventYellowCards
			case "fouls":
				eventType = models.StandardEventFouls
			default:
				continue
			}
			
			eventID := matchID + "_" + bookmakerKey + "_" + string(eventType) + "_" + strings.ReplaceAll(overMarket.param, ".", "_")
			event := models.Event{
				ID:         eventID,
				MatchID:    matchID,
				EventType:  string(eventType),
				MarketName: models.GetMarketName(eventType) + " " + overMarket.param,
				Bookmaker:  bookmakerName,
				Outcomes: []models.Outcome{
					{
						ID:          eventID + "_over",
						EventID:     eventID,
						OutcomeType: string(models.OutcomeTypeTotalOver),
						Parameter:   overMarket.param,
						Odds:        overMarket.odds,
						Bookmaker:   bookmakerName,
						CreatedAt:   now,
						UpdatedAt:   now,
					},
					{
						ID:          eventID + "_under",
						EventID:     eventID,
						OutcomeType: string(models.OutcomeTypeTotalUnder),
						Parameter:   underMarket.param,
						Odds:        underMarket.odds,
						Bookmaker:   bookmakerName,
						CreatedAt:   now,
						UpdatedAt:   now,
					},
				},
				CreatedAt: now,
				UpdatedAt: now,
			}
			match.Events = append(match.Events, event)
		}
	}

	// Corners totals from selection-key (Тотал угловых 6.5, 7.5, ... 14.5)
	for param, odds := range parseCornersTotalsFromSelectionKey(bodyStr) {
		eventID := matchID + "_" + bookmakerKey + "_corners_total_" + strings.ReplaceAll(param, ".", "_")
		cornersEvent := models.Event{
			ID:         eventID,
			MatchID:    matchID,
			EventType:  string(models.StandardEventCorners),
			MarketName: "Corners Total " + param,
			Bookmaker:  bookmakerName,
			Outcomes: []models.Outcome{
				{ID: eventID + "_under", EventID: eventID, OutcomeType: string(models.OutcomeTypeTotalUnder), Parameter: param, Odds: odds.Under, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: eventID + "_over", EventID: eventID, OutcomeType: string(models.OutcomeTypeTotalOver), Parameter: param, Odds: odds.Over, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		match.Events = append(match.Events, cornersEvent)
	}

	// Most Corners (Кто подаст больше угловых) — 1X2 по угловым
	if h, d, a := parseMostCorners1X2FromSelectionKey(bodyStr); h > 0 && d > 0 && a > 0 {
		eventID := matchID + "_" + bookmakerKey + "_most_corners"
		match.Events = append(match.Events, models.Event{
			ID:         eventID,
			MatchID:    matchID,
			EventType:  string(models.StandardEventCorners),
			MarketName: "Most Corners",
			Bookmaker:  bookmakerName,
			Outcomes: []models.Outcome{
				{ID: eventID + "_1", EventID: eventID, OutcomeType: string(models.OutcomeTypeHomeWin), Odds: h, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: eventID + "_X", EventID: eventID, OutcomeType: string(models.OutcomeTypeDraw), Odds: d, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: eventID + "_2", EventID: eventID, OutcomeType: string(models.OutcomeTypeAwayWin), Odds: a, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	// Corners handicap (Угловые с учётом форы)
	for _, h := range parseCornersHandicapsFromSelectionKey(bodyStr) {
		paramLabel := h.HomeParam + " / " + h.AwayParam
		eventID := matchID + "_" + bookmakerKey + "_corners_handicap_" + strings.ReplaceAll(h.LineKey, ".", "_")
		match.Events = append(match.Events, models.Event{
			ID:         eventID,
			MatchID:    matchID,
			EventType:  string(models.StandardEventCorners),
			MarketName: "Corners Handicap " + paramLabel,
			Bookmaker:  bookmakerName,
			Outcomes: []models.Outcome{
				{ID: eventID + "_home", EventID: eventID, OutcomeType: "handicap_home", Parameter: h.HomeParam, Odds: h.HomeOdds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: eventID + "_away", EventID: eventID, OutcomeType: "handicap_away", Parameter: h.AwayParam, Odds: h.AwayOdds, Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	
	// Parse remaining markets using old method as fallback (for markets without preference-id)
	// Find all remaining data-sel that weren't processed
	var remainingOdds []oddWithContext
	allMatches := dataSelRegex.FindAllStringSubmatchIndex(bodyStr, -1)
	processedPositions := make(map[int]bool)
	for _, m := range markets {
		processedPositions[m.position] = true
	}
	for _, pm := range prefMarkets {
		processedPositions[pm.position] = true
	}
	
	for _, match := range allMatches {
		if processedPositions[match[0]] {
			continue // Skip already processed markets
		}
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
			start := match[0] - 200
			if start < 0 {
				start = 0
			}
			context := bodyStr[start:match[0]]
			
			remainingOdds = append(remainingOdds, oddWithContext{
				odds:     s.Epr,
				position: match[0],
				context:  context,
			})
		}
	}
	
	// Only use fallback for markets that are NOT corners, yellow cards, or fouls
	if len(remainingOdds) > 0 {
		parseAdditionalMarkets(match, matchID, bookmakerKey, remainingOdds, now)
	}

	return match, nil
}
