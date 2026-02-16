package olimp

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/parserutil"
)

const delayPerLeague = 400 * time.Millisecond
const delayPerMatch = 250 * time.Millisecond

var runOnceMu sync.Mutex

type Parser struct {
	cfg      *config.Config
	client   *Client
	incState *parserutil.IncrementalParserState
}

func NewParser(cfg *config.Config) *Parser {
	o := &cfg.Parser.Olimp
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = cfg.Parser.Timeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := NewClient(o.BaseURL, o.SportID, timeout, o.Referer, o.ProxyList)
	return &Parser{cfg: cfg, client: client}
}

func (p *Parser) runOnce(ctx context.Context) error {
	if p.cfg.Parser.Olimp.Referer == "" {
		slog.Warn("olimp: referer not set, skipping (set parser.olimp.referer)")
		return nil
	}
	runOnceMu.Lock()
	defer runOnceMu.Unlock()
	start := time.Now()
	var totalMatches int
	defer func() {
		slog.Info("olimp: cycle finished", "matches", totalMatches, "duration", time.Since(start))
	}()

	sports, err := p.client.GetSportsWithCompetitions(ctx)
	if err != nil {
		return fmt.Errorf("sports-with-competitions: %w", err)
	}
	competitionIDs := extractCompetitionIDs(sports, p.cfg.Parser.Olimp.SportID)
	if len(competitionIDs) == 0 {
		slog.Info("olimp: no football competitions")
		return nil
	}
	slog.Info("olimp: leagues to process", "count", len(competitionIDs))

	for _, compID := range competitionIDs {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		resp, err := p.client.GetCompetitionsWithEvents(ctx, compID)
		if err != nil {
			slog.Warn("olimp: competitions-with-events failed", "competition_id", compID, "error", err)
			time.Sleep(delayPerLeague)
			continue
		}
		leagueName := ""
		if len(resp) > 0 && resp[0].Payload != nil && resp[0].Payload.Competition != nil {
			leagueName = resp[0].Payload.Competition.Name
		}
		for i := range resp {
			if resp[i].Payload == nil {
				continue
			}
			for _, ev := range resp[i].Payload.Events {
				select {
				case <-ctx.Done():
					return nil
				default:
				}
				// Step 3: full line per match (corners, fouls, yellow cards, offsides, etc.)
				fullEvent, err := p.client.GetEventLine(ctx, ev.ID)
				if err != nil {
					slog.Warn("olimp: get event line failed", "event_id", ev.ID, "error", err)
					time.Sleep(delayPerMatch)
					continue
				}
				match := ParseEvent(fullEvent, leagueName)
				if match != nil {
					health.AddMatch(match)
					totalMatches++
				}
				time.Sleep(delayPerMatch)
			}
		}
		time.Sleep(delayPerLeague)
	}
	return nil
}

func extractCompetitionIDs(sports SportsWithCompetitionsResponse, sportID int) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, item := range sports {
		if item.Payload == nil {
			continue
		}
		if item.Payload.ID != "" {
			sid, ok := parseInt(item.Payload.ID)
			if !ok || sid != sportID {
				continue
			}
		}
		for _, cat := range item.Payload.CategoriesWithCompetitions {
			for _, c := range cat.Competitions {
				if c.ID != "" && !seen[c.ID] {
					seen[c.ID] = true
					ids = append(ids, c.ID)
				}
			}
		}
	}
	return ids
}

func parseInt(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func (p *Parser) Start(ctx context.Context) error {
	slog.Info("Starting Olimp parser (background mode)...")
	if err := p.runOnce(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func (p *Parser) ParseOnce(ctx context.Context) error {
	return p.runOnce(ctx)
}

func (p *Parser) Stop() error {
	if p.incState != nil {
		p.incState.Stop("olimp")
	}
	return nil
}

func (p *Parser) GetName() string {
	return bookmakerName
}

func (p *Parser) StartIncremental(ctx context.Context, timeout time.Duration) error {
	if p.incState != nil && p.incState.IsRunning() {
		slog.Warn("olimp: incremental parsing already started")
		return nil
	}
	p.incState = parserutil.NewIncrementalParserState(ctx)
	if err := p.incState.Start("olimp"); err != nil {
		return err
	}
	go parserutil.RunIncrementalLoop(p.incState.Ctx, timeout, "olimp", p.incState, p.runIncrementalCycle)
	slog.Info("olimp: incremental parsing loop started")
	return nil
}

func (p *Parser) TriggerNewCycle() error {
	if p.incState == nil {
		return fmt.Errorf("incremental parsing not started")
	}
	return p.incState.TriggerNewCycle("olimp")
}

func (p *Parser) runIncrementalCycle(ctx context.Context, timeout time.Duration) {
	cycleID := time.Now().Unix()
	parserutil.LogCycleStart("olimp", cycleID, timeout)
	cycleCtx, cancel := parserutil.CreateCycleContext(ctx, timeout)
	defer cancel()
	start := time.Now()
	defer func() { parserutil.LogCycleFinish("olimp", cycleID, time.Since(start)) }()
	_ = p.runOnce(cycleCtx)
}
