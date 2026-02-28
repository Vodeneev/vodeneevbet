package leon

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/parserutil"
)

var runOnceMu sync.Mutex

type Parser struct {
	cfg      *config.Config
	client   *Client
	incState *parserutil.IncrementalParserState
}

func NewParser(cfg *config.Config) *Parser {
	c := &cfg.Parser.Leon
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = cfg.Parser.Timeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := NewClient(c.BaseURL, timeout)
	return &Parser{cfg: cfg, client: client}
}

func (p *Parser) runOnce(ctx context.Context) error {
	runOnceMu.Lock()
	defer runOnceMu.Unlock()
	start := time.Now()
	var totalMatches int
	defer func() {
		slog.Info("Leon: цикл парсинга завершён", "matches", totalMatches, "duration", time.Since(start))
	}()

	sports, err := p.client.GetSports(ctx)
	if err != nil {
		return fmt.Errorf("GetSports: %w", err)
	}
	family := p.cfg.Parser.Leon.SportFamily
	if family == "" {
		family = "Soccer"
	}
	leagueIDs := CollectLeagueIDs(sports, family)
	maxLeagues := p.cfg.Parser.Leon.MaxLeagues
	if maxLeagues > 0 && len(leagueIDs) > maxLeagues {
		leagueIDs = leagueIDs[:maxLeagues]
	}
	slog.Info("Leon: лиги к обработке", "count", len(leagueIDs))

	for li, leagueID := range leagueIDs {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		eventsResp, err := p.client.GetLeagueEvents(ctx, leagueID)
		if err != nil {
			slog.Warn("Leon: GetLeagueEvents failed", "league_id", leagueID, "error", err)
			if d := p.cfg.Parser.Leon.DelayPerLeague; d > 0 {
				time.Sleep(d)
			}
			continue
		}
		leagueName := "Leon"
		if len(eventsResp.Events) > 0 && eventsResp.Events[0].League.Name != "" {
			leagueName = eventsResp.Events[0].League.Name
		}
		for _, ev := range eventsResp.Events {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			fullEv, err := p.client.GetEvent(ctx, ev.ID)
			if err != nil {
				slog.Debug("Leon: GetEvent failed", "event_id", ev.ID, "error", err)
				if d := p.cfg.Parser.Leon.DelayPerEvent; d > 0 {
					time.Sleep(d)
				}
				continue
			}
			match := LeonEventToMatch(fullEv, leagueName)
			if match != nil {
				health.AddMatch(match)
				totalMatches++
			}
			if d := p.cfg.Parser.Leon.DelayPerEvent; d > 0 {
				time.Sleep(d)
			}
		}
		if (li+1)%20 == 0 {
			slog.Info("Leon: прогресс лиг", "processed", li+1, "total", len(leagueIDs), "matches", totalMatches)
		}
		if d := p.cfg.Parser.Leon.DelayPerLeague; d > 0 {
			time.Sleep(d)
		}
	}
	return nil
}

func (p *Parser) Start(ctx context.Context) error {
	slog.Info("Starting Leon parser (background mode)...")
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
		p.incState.Stop("Leon")
	}
	return nil
}

func (p *Parser) GetName() string {
	return bookmakerName
}

func (p *Parser) StartIncremental(ctx context.Context, timeout time.Duration) error {
	if p.incState != nil && p.incState.IsRunning() {
		slog.Warn("Leon: incremental parsing already started")
		return nil
	}
	p.incState = parserutil.NewIncrementalParserState(ctx)
	if err := p.incState.Start("Leon"); err != nil {
		return err
	}
	go parserutil.RunIncrementalLoop(p.incState.Ctx, timeout, "Leon", p.incState, p.runIncrementalCycle)
	slog.Info("Leon: incremental parsing loop started")
	return nil
}

func (p *Parser) TriggerNewCycle() error {
	if p.incState == nil {
		return fmt.Errorf("incremental parsing not started")
	}
	return p.incState.TriggerNewCycle("Leon")
}

func (p *Parser) runIncrementalCycle(ctx context.Context, timeout time.Duration) {
	cycleID := time.Now().Unix()
	parserutil.LogCycleStart("Leon", cycleID, timeout)
	cycleCtx, cancel := parserutil.CreateCycleContext(ctx, timeout)
	defer cancel()
	start := time.Now()
	defer func() { parserutil.LogCycleFinish("Leon", cycleID, time.Since(start)) }()
	_ = p.runOnce(cycleCtx)
}
