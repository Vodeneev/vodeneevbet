package fonbet

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/performance"
)

// Parser is the main parser that coordinates all components
type Parser struct {
	eventFetcher   interfaces.EventFetcher
	oddsParser     interfaces.OddsParser
	matchBuilder   interfaces.MatchBuilder
	eventProcessor interfaces.EventProcessor
	storage        interfaces.Storage
	config         *config.Config
	
	// Incremental parsing state
	incMu            sync.Mutex
	incrementalCtx   context.Context
	incrementalCancel context.CancelFunc
	cycleTrigger     chan struct{}
}

func NewParser(config *config.Config) *Parser {
	// Create components
	eventFetcher := NewEventFetcher(config)
	oddsParser := NewOddsParser()
	matchBuilder := NewMatchBuilder("Fonbet")
	eventProcessor := NewBatchProcessor(nil, eventFetcher, oddsParser, matchBuilder)

	return &Parser{
		eventFetcher:   eventFetcher,
		oddsParser:     oddsParser,
		matchBuilder:   matchBuilder,
		eventProcessor: eventProcessor,
		storage:        nil, // No external storage - data served from memory
		config:         config,
	}
}

// runOnce performs a single parsing run for all configured sports
func (p *Parser) runOnce(ctx context.Context) error {
	for _, sportStr := range p.config.ValueCalculator.Sports {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		sport, valid := enums.ParseSport(sportStr)
		if !valid {
			slog.Warn("Unsupported sport", "sport", sportStr)
			continue
		}

		if err := p.eventProcessor.ProcessSportEvents(sportStr); err != nil {
			slog.Error("Failed to parse events", "sport", sport, "error", err)
			continue
		}

		// Print performance summary after each run
		performance.GetTracker().PrintSummary()
	}
	return nil
}

func (p *Parser) Start(ctx context.Context) error {
	slog.Info("Starting Fonbet parser (background mode - periodic parsing runs automatically)...")

	// Run once at startup to have initial data
	if err := p.runOnce(ctx); err != nil {
		return err
	}

	<-ctx.Done()

	// Print final summary before shutdown
	slog.Info("Final Performance Summary")
	performance.GetTracker().PrintSummary()
	return nil
}

// ParseOnce triggers a single parsing run (on-demand parsing)
func (p *Parser) ParseOnce(ctx context.Context) error {
	return p.runOnce(ctx)
}

func (p *Parser) Stop() error {
	p.incMu.Lock()
	defer p.incMu.Unlock()
	if p.incrementalCancel != nil {
		p.incrementalCancel()
		p.incrementalCancel = nil
	}
	slog.Info("Stopping Fonbet parser...")
	return nil
}

func (p *Parser) GetName() string {
	return "Fonbet"
}

// StartIncremental starts continuous incremental parsing in background
// It parses matches in batches and updates storage incrementally after each batch
func (p *Parser) StartIncremental(ctx context.Context, timeout time.Duration) error {
	p.incMu.Lock()
	defer p.incMu.Unlock()
	
	if p.incrementalCancel != nil {
		// Already running
		slog.Warn("Fonbet: incremental parsing already started, skipping")
		return nil
	}
	
	if timeout > 0 {
		slog.Info("Fonbet: initializing incremental parsing", "timeout", timeout)
	} else {
		slog.Info("Fonbet: initializing incremental parsing", "timeout", "unlimited")
	}
	incCtx, cancel := context.WithCancel(ctx)
	p.incrementalCtx = incCtx
	p.incrementalCancel = cancel
	p.cycleTrigger = make(chan struct{}, 1)
	
	// Trigger first cycle immediately
	p.cycleTrigger <- struct{}{}
	slog.Info("Fonbet: triggered initial incremental parsing cycle")
	
	// Start background incremental parsing loop
	go p.incrementalLoop(incCtx, timeout)
	slog.Info("Fonbet: incremental parsing loop started in background")
	
	return nil
}

// TriggerNewCycle signals the parser to start a new parsing cycle
func (p *Parser) TriggerNewCycle() error {
	p.incMu.Lock()
	defer p.incMu.Unlock()
	
	if p.cycleTrigger == nil {
		slog.Error("Fonbet: cannot trigger cycle - incremental parsing not started")
		return fmt.Errorf("incremental parsing not started")
	}
	
	// Non-blocking trigger
	select {
	case p.cycleTrigger <- struct{}{}:
		slog.Info("Fonbet: triggered new incremental parsing cycle")
		return nil
	default:
		// Cycle already triggered, skip
		slog.Debug("Fonbet: cycle already triggered, skipping duplicate trigger")
		return nil
	}
}

// incrementalLoop runs continuous incremental parsing
func (p *Parser) incrementalLoop(ctx context.Context, timeout time.Duration) {
	if timeout > 0 {
		slog.Info("Fonbet: incremental parsing loop started", "timeout", timeout)
	} else {
		slog.Info("Fonbet: incremental parsing loop started", "timeout", "unlimited")
	}
	cycleCount := 0
	
	for {
		select {
		case <-ctx.Done():
			slog.Info("Fonbet: incremental parsing loop stopped", "total_cycles", cycleCount)
			return
		case <-p.cycleTrigger:
			cycleCount++
			slog.Info("Fonbet: received cycle trigger", "cycle_number", cycleCount)
			// Start new parsing cycle with timeout
			p.runIncrementalCycle(ctx, timeout)
			slog.Info("Fonbet: cycle completed, waiting for next trigger", "cycle_number", cycleCount)
		}
	}
}

// runIncrementalCycle runs one full incremental parsing cycle
func (p *Parser) runIncrementalCycle(ctx context.Context, timeout time.Duration) {
	start := time.Now()
	cycleID := time.Now().Unix()
	if timeout > 0 {
		slog.Info("Fonbet: starting incremental cycle", "cycle_id", cycleID, "timeout", timeout)
	} else {
		slog.Info("Fonbet: starting incremental cycle", "cycle_id", cycleID, "timeout", "unlimited")
	}
	
	// Create context with timeout for this cycle (if timeout > 0)
	var cycleCtx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		cycleCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	} else {
		cycleCtx = ctx
	}
	defer func() {
		duration := time.Since(start)
		slog.Info("Fonbet: incremental cycle finished", "cycle_id", cycleID, "duration", duration, "duration_sec", duration.Seconds())
	}()
	
	// Process all configured sports incrementally
	// Data is saved incrementally after each batch in BatchProcessor
	for _, sportStr := range p.config.ValueCalculator.Sports {
		select {
		case <-cycleCtx.Done():
			slog.Warn("Fonbet: incremental cycle interrupted", "sport", sportStr, "cycle_id", cycleID)
			return
		default:
		}
		
		sport, valid := enums.ParseSport(sportStr)
		if !valid {
			slog.Warn("Unsupported sport", "sport", sportStr)
			continue
		}
		
		slog.Info("Fonbet: processing sport incrementally", "sport", sportStr, "cycle_id", cycleID)
		if err := p.eventProcessor.ProcessSportEvents(sportStr); err != nil {
			slog.Error("Failed to parse events", "sport", sport, "error", err)
			continue
		}
		slog.Info("Fonbet: sport processed incrementally", "sport", sportStr, "cycle_id", cycleID)
		
		// Print performance summary after each sport
		performance.GetTracker().PrintSummary()
	}
}
