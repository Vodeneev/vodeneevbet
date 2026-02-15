package zenit

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

const delayPerMatch = 300 * time.Millisecond

var runOnceMu sync.Mutex

type Parser struct {
	cfg     *config.Config
	client  *Client
	incState *parserutil.IncrementalParserState
}

func NewParser(cfg *config.Config) *Parser {
	z := &cfg.Parser.Zenit
	timeout := z.Timeout
	if timeout <= 0 {
		timeout = cfg.Parser.Timeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client := NewClient(z.BaseURL, z.ImprintHash, z.FrontVersion, z.SportID, timeout, z.ProxyList)
	return &Parser{
		cfg:    cfg,
		client: client,
	}
}

func (p *Parser) runOnce(ctx context.Context) error {
	if p.cfg.Parser.Zenit.ImprintHash == "" {
		slog.Warn("zenit: imprint_hash not set, skipping (set parser.zenit.imprint_hash from browser DevTools)")
		return nil
	}
	runOnceMu.Lock()
	defer runOnceMu.Unlock()
	start := time.Now()
	var totalMatches int
	defer func() {
		slog.Info("zenit: цикл парсинга завершён", "matches", totalMatches, "duration", time.Since(start))
	}()

	slog.Info("zenit: runOnce started")

	offset := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		page, err := p.client.GetLinePage(ctx, offset)
		if err != nil {
			return fmt.Errorf("get line page offset %d: %w", offset, err)
		}

		// Collect (gameID, lid, rid, tid) from league -> games
		var gameIDs []gameRef
		for _, league := range page.League {
			for _, gid := range league.Games {
				gameIDs = append(gameIDs, gameRef{
					gameID: gid,
					lid:    league.ID,
					rid:    league.Rid,
					tid:    league.Tid,
				})
			}
		}

		if len(gameIDs) == 0 {
			break
		}

		slog.Info("zenit: processing page", "offset", offset, "games", len(gameIDs))

		for _, ref := range gameIDs {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			// Full line (with t_b) per match
			matchResp, err := p.client.GetMatch(ctx, ref.rid, ref.tid, ref.lid, ref.gameID)
			if err != nil {
				slog.Warn("zenit: get match failed", "game_id", ref.gameID, "error", err)
				time.Sleep(delayPerMatch)
				continue
			}

			match := ParseMatch(matchResp, ref.gameID)
			if match != nil {
				health.AddMatch(match)
				totalMatches++
			}

			time.Sleep(delayPerMatch)
		}

		if len(gameIDs) < 50 {
			break
		}
		offset += 50
	}

	return nil
}

type gameRef struct {
	gameID int
	lid    int
	rid    int
	tid    int
}

func (p *Parser) Start(ctx context.Context) error {
	slog.Info("Starting Zenit parser (background mode)...")
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
		p.incState.Stop("zenit")
	}
	return nil
}

func (p *Parser) GetName() string {
	return bookmakerName
}

func (p *Parser) StartIncremental(ctx context.Context, timeout time.Duration) error {
	if p.incState != nil && p.incState.IsRunning() {
		slog.Warn("zenit: incremental parsing already started")
		return nil
	}
	p.incState = parserutil.NewIncrementalParserState(ctx)
	if err := p.incState.Start("zenit"); err != nil {
		return err
	}
	go parserutil.RunIncrementalLoop(p.incState.Ctx, timeout, "zenit", p.incState, p.runIncrementalCycle)
	slog.Info("zenit: incremental parsing loop started")
	return nil
}

func (p *Parser) TriggerNewCycle() error {
	if p.incState == nil {
		return fmt.Errorf("incremental parsing not started")
	}
	return p.incState.TriggerNewCycle("zenit")
}

func (p *Parser) runIncrementalCycle(ctx context.Context, timeout time.Duration) {
	cycleID := time.Now().Unix()
	parserutil.LogCycleStart("zenit", cycleID, timeout)
	cycleCtx, cancel := parserutil.CreateCycleContext(ctx, timeout)
	defer cancel()
	start := time.Now()
	defer func() { parserutil.LogCycleFinish("zenit", cycleID, time.Since(start)) }()

	_ = p.runOnce(cycleCtx)
}
