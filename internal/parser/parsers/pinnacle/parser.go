package pinnacle

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
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

func (p *Parser) processMatchup(ctx context.Context, matchupID int64) error {
	related, err := p.client.GetRelatedMatchups(matchupID)
	if err != nil {
		return err
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

	fmt.Printf("Pinnacle: built match %s (%s vs %s), events=%d\n", m.ID, m.HomeTeam, m.AwayTeam, len(m.Events))

	// Storage is optional.
	if p.storage == nil {
		return nil
	}
	return p.storage.StoreMatch(ctx, m)
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

	matchID := "pinnacle_" + strconv.FormatInt(matchupID, 10)
	now := time.Now()

	match := &models.Match{
		ID:         matchID,
		Name:       fmt.Sprintf("%s vs %s", home, away),
		HomeTeam:   home,
		AwayTeam:   away,
		StartTime:  startTime,
		Sport:      "football",
		Tournament: rm.League.Name,
		Bookmaker:  "Pinnacle",
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	event := models.Event{
		ID:         matchID + "_main_match",
		MatchID:    matchID,
		EventType:  string(models.StandardEventMainMatch),
		MarketName: models.GetMarketName(models.StandardEventMainMatch),
		Bookmaker:  "Pinnacle",
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Period 0 only for now.
	for _, m := range markets {
		if m.MatchupID != matchupID || m.Period != 0 || m.Status != "open" {
			continue
		}
		switch m.Type {
		case "moneyline":
			for _, pr := range m.Prices {
				odds := americanToDecimal(pr.Price)
				switch pr.Designation {
				case "home":
					event.Outcomes = append(event.Outcomes, newOutcome(event.ID, "home_win", "", odds))
				case "away":
					event.Outcomes = append(event.Outcomes, newOutcome(event.ID, "away_win", "", odds))
				case "draw":
					event.Outcomes = append(event.Outcomes, newOutcome(event.ID, "draw", "", odds))
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
					event.Outcomes = append(event.Outcomes, newOutcome(event.ID, "total_over", line, odds))
				case "under":
					event.Outcomes = append(event.Outcomes, newOutcome(event.ID, "total_under", line, odds))
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
					event.Outcomes = append(event.Outcomes, newOutcome(event.ID, "handicap_home", formatSignedLine(-*pr.Points), odds))
				case "away":
					event.Outcomes = append(event.Outcomes, newOutcome(event.ID, "handicap_away", formatSignedLine(+*pr.Points), odds))
				}
			}
		}
	}

	if len(event.Outcomes) > 0 {
		match.Events = append(match.Events, event)
	}

	return match, nil
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

