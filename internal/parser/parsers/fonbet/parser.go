package fonbet

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/parserutil"
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
	incState *parserutil.IncrementalParserState
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
	if p.incState != nil {
		p.incState.Stop("Fonbet")
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
	if p.incState != nil && p.incState.IsRunning() {
		slog.Warn("Fonbet: incremental parsing already started, skipping")
		return nil
	}
	
	if timeout > 0 {
		slog.Info("Fonbet: initializing incremental parsing", "timeout", timeout)
	} else {
		slog.Info("Fonbet: initializing incremental parsing", "timeout", "unlimited")
	}
	
	p.incState = parserutil.NewIncrementalParserState(ctx)
	if err := p.incState.Start("Fonbet"); err != nil {
		return err
	}
	
	// Start background incremental parsing loop
	go parserutil.RunIncrementalLoop(p.incState.Ctx, timeout, "Fonbet", p.incState, p.runIncrementalCycle)
	slog.Info("Fonbet: incremental parsing loop started in background")
	
	return nil
}

// TriggerNewCycle signals the parser to start a new parsing cycle
func (p *Parser) TriggerNewCycle() error {
	if p.incState == nil {
		return fmt.Errorf("incremental parsing not started")
	}
	return p.incState.TriggerNewCycle("Fonbet")
}

// incrementalLoop is now handled by parserutil.RunIncrementalLoop

// runIncrementalCycle runs one full incremental parsing cycle
func (p *Parser) runIncrementalCycle(ctx context.Context, timeout time.Duration) {
	start := time.Now()
	cycleID := time.Now().Unix()
	parserutil.LogCycleStart("Fonbet", cycleID, timeout)
	
	// Create context with timeout for this cycle (if timeout > 0)
	cycleCtx, cancel := parserutil.CreateCycleContext(ctx, timeout)
	defer cancel()
	defer func() {
		duration := time.Since(start)
		parserutil.LogCycleFinish("Fonbet", cycleID, duration)
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
