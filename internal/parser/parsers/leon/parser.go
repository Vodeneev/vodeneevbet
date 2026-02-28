package leon

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
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

// processSingleLeague fetches one league's events and details, adds matches to health store. Returns match count.
func (p *Parser) processSingleLeague(ctx context.Context, leagueID int64) int {
	eventsResp, err := p.client.GetLeagueEvents(ctx, leagueID)
	if err != nil {
		slog.Warn("Leon: GetLeagueEvents failed", "league_id", leagueID, "error", err)
		return 0
	}
	leagueName := "Leon"
	if len(eventsResp.Events) > 0 && eventsResp.Events[0].League.Name != "" {
		leagueName = eventsResp.Events[0].League.Name
	}

	maxConcurrentEvents := p.cfg.Parser.Leon.MaxConcurrentEventsPerLeague
	if maxConcurrentEvents < 1 {
		maxConcurrentEvents = 1
	}
	delayEvent := p.cfg.Parser.Leon.DelayPerEvent

	var count int
	if maxConcurrentEvents == 1 {
		for _, ev := range eventsResp.Events {
			select {
			case <-ctx.Done():
				return count
			default:
			}
			fullEv, err := p.client.GetEvent(ctx, ev.ID)
			if err != nil {
				slog.Debug("Leon: GetEvent failed", "event_id", ev.ID, "error", err)
				if delayEvent > 0 {
					time.Sleep(delayEvent)
				}
				continue
			}
			match := LeonEventToMatch(fullEv, leagueName)
			if match != nil {
				health.AddMatch(match)
				count++
			}
			if delayEvent > 0 {
				time.Sleep(delayEvent)
			}
		}
		return count
	}

	sem := make(chan struct{}, maxConcurrentEvents)
	var wg sync.WaitGroup
	var countMu sync.Mutex
	for _, ev := range eventsResp.Events {
		if ctx.Err() != nil {
			break
		}
		ev := ev
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fullEv, err := p.client.GetEvent(ctx, ev.ID)
			if err != nil {
				slog.Debug("Leon: GetEvent failed", "event_id", ev.ID, "error", err)
				if delayEvent > 0 {
					time.Sleep(delayEvent)
				}
				return
			}
			match := LeonEventToMatch(fullEv, leagueName)
			if match != nil {
				health.AddMatch(match)
				countMu.Lock()
				count++
				countMu.Unlock()
			}
			if delayEvent > 0 {
				time.Sleep(delayEvent)
			}
		}()
	}
	wg.Wait()
	return count
}

func (p *Parser) runOnce(ctx context.Context) error {
	runOnceMu.Lock()
	defer runOnceMu.Unlock()
	start := time.Now()
	var matchesTotal int64
	defer func() {
		slog.Info("Leon: цикл парсинга завершён", "matches", matchesTotal, "duration", time.Since(start))
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
	totalLeagues := len(leagueIDs)
	slog.Info("Leon: лиги к обработке", "count", totalLeagues)

	maxConcurrentLeagues := p.cfg.Parser.Leon.MaxConcurrentLeagues
	if maxConcurrentLeagues <= 0 {
		maxConcurrentLeagues = 1
	}
	delayLeague := p.cfg.Parser.Leon.DelayPerLeague

	if maxConcurrentLeagues == 1 {
		for li, leagueID := range leagueIDs {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			n := p.processSingleLeague(ctx, leagueID)
			matchesTotal += int64(n)
			if (li+1)%20 == 0 {
				slog.Info("Leon: прогресс лиг", "processed", li+1, "total", totalLeagues, "matches", matchesTotal)
			}
			if delayLeague > 0 {
				time.Sleep(delayLeague)
			}
		}
		return nil
	}

	// Parallel leagues: worker pool (like xbet1 MaxConcurrentChampionships)
	ch := make(chan int64, totalLeagues)
	for _, id := range leagueIDs {
		ch <- id
	}
	close(ch)
	var completed atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < maxConcurrentLeagues; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for leagueID := range ch {
				if ctx.Err() != nil {
					return
				}
				n := p.processSingleLeague(ctx, leagueID)
				atomic.AddInt64(&matchesTotal, int64(n))
				done := completed.Add(1)
				if done%20 == 0 {
					total := atomic.LoadInt64(&matchesTotal)
					slog.Info("Leon: прогресс лиг", "processed", done, "total", totalLeagues, "matches", total)
				}
			}
		}()
	}
	wg.Wait()
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
