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

type eventJSON struct {
	TreeID          int64    `json:"treeId"`
	MarathonEventID int64    `json:"marathonEventId"`
	TeamNames       []string `json:"teamNames"`
	StartTime       string   `json:"startTime,omitempty"`
}

type selJSON struct {
	Epr float64 `json:"epr"` // decimal odds
	Prt int     `json:"prt"` // market/price type, optional
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
	client := NewClient(baseURL, userAgent, timeout)
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
	_ = p.ParseOnce(cycleCtx)
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

	// Rate limiting: delay between league requests (300ms) and event requests (150ms)
	leagueDelay := 300 * time.Millisecond
	eventDelay := 150 * time.Millisecond

	for i, leaguePath := range leaguePaths {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		// Delay before each league request (except first)
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(leagueDelay):
			}
		}
		events, err := p.fetchLeagueEvents(ctx, leaguePath)
		if err != nil {
			slog.Warn("Marathonbet: league failed", "path", leaguePath, "error", err)
			continue
		}
		for j, eventPath := range events {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			// Delay before each event request (except first in league)
			if j > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(eventDelay):
				}
			}
			match, err := p.fetchEventMatch(ctx, eventPath)
			if err != nil {
				slog.Debug("Marathonbet: event failed", "path", eventPath, "error", err)
				continue
			}
			if match != nil {
				health.AddMatch(match)
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
func parseEventPage(htmlBody []byte, _ string) (*models.Match, error) {
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

	// All odds from data-sel in order
	var odds []float64
	for _, m := range dataSelRegex.FindAllStringSubmatch(bodyStr, -1) {
		raw := m[1]
		if raw == "" {
			raw = m[2]
		}
		raw = html.UnescapeString(raw)
		var s selJSON
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			continue
		}
		if s.Epr > 0 {
			odds = append(odds, s.Epr)
		}
	}

	if len(odds) < 3 {
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

	// Main match (1X2): first three odds
	mainEventID := matchID + "_" + bookmakerKey + "_" + string(models.StandardEventMainMatch)
	mainEvent := models.Event{
		ID:         mainEventID,
		MatchID:    matchID,
		EventType:  string(models.StandardEventMainMatch),
		MarketName: models.GetMarketName(models.StandardEventMainMatch),
		Bookmaker:  bookmakerName,
		Outcomes: []models.Outcome{
			{ID: mainEventID + "_1", EventID: mainEventID, OutcomeType: string(models.OutcomeTypeHomeWin), Odds: odds[0], Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			{ID: mainEventID + "_X", EventID: mainEventID, OutcomeType: string(models.OutcomeTypeDraw), Odds: odds[1], Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			{ID: mainEventID + "_2", EventID: mainEventID, OutcomeType: string(models.OutcomeTypeAwayWin), Odds: odds[2], Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	match.Events = append(match.Events, mainEvent)

	// Total 2.5: if we have at least 5 odds, 4th and 5th are often total over/under 2.5
	if len(odds) >= 5 && odds[3] >= 1.2 && odds[3] <= 5 && odds[4] >= 1.2 && odds[4] <= 5 {
		totalParam := "2.5"
		totalEventID := matchID + "_" + bookmakerKey + "_total_2.5"
		totalEvent := models.Event{
			ID:         totalEventID,
			MatchID:    matchID,
			EventType:  string(models.StandardEventMainMatch),
			MarketName: "Total 2.5",
			Bookmaker:  bookmakerName,
			Outcomes: []models.Outcome{
				{ID: totalEventID + "_over", EventID: totalEventID, OutcomeType: string(models.OutcomeTypeTotalOver), Parameter: totalParam, Odds: odds[3], Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
				{ID: totalEventID + "_under", EventID: totalEventID, OutcomeType: string(models.OutcomeTypeTotalUnder), Parameter: totalParam, Odds: odds[4], Bookmaker: bookmakerName, CreatedAt: now, UpdatedAt: now},
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		match.Events = append(match.Events, totalEvent)
	}

	return match, nil
}
