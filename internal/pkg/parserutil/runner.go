package parserutil

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
)

// ParserFunc is a function that runs a parser and returns an error
type ParserFunc func(ctx context.Context, p interfaces.Parser) error

// RunOptions configures how parsers should be run (always async/non-blocking)
type RunOptions struct {
	// LogStart logs when each parser starts (e.g., "Starting %s parser...")
	LogStart bool
	// OnError is called when a parser returns an error. If nil, errors are logged.
	OnError func(p interfaces.Parser, err error)
	// WaitForCompletion when true blocks until all parsers finish (so the passed context stays valid for the full run).
	// When false, RunParsers returns immediately and the caller must not cancel the context until parsers are done.
	WaitForCompletion bool
}

// AsyncRunOptions returns default options for async (non-blocking) execution
func AsyncRunOptions() RunOptions {
	return RunOptions{
		LogStart: false,
		OnError:  nil, // Will use default logging
	}
}

// RunParsers runs all parsers in parallel using the provided function
func RunParsers(ctx context.Context, parsers []interfaces.Parser, parserFunc ParserFunc, opts RunOptions) error {
	if len(parsers) == 0 {
		return nil
	}

	var wg sync.WaitGroup

	// Default error handler logs errors
	onError := opts.OnError
	if onError == nil {
		onError = func(p interfaces.Parser, err error) {
			slog.Error("Parser failed", "parser", p.GetName(), "error", err)
		}
	}

	// Start all parsers in parallel
	for _, p := range parsers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()

			if opts.LogStart {
				slog.Info("Starting parser", "parser", p.GetName())
			}

			err := parserFunc(ctx, p)
			if err != nil && ctx.Err() == nil {
				// Error occurred but context is still valid
				onError(p, err)
			}
		}()
	}

	if opts.WaitForCompletion {
		wg.Wait()
	}
	return nil
}

// ClearMatchesBeforeCycle clears matches from in-memory store before starting a new parsing cycle
// Should be called at the start of each incremental parsing cycle
func ClearMatchesBeforeCycle(parserName string) {
	health.ClearMatches()
	slog.Info("Cleared matches from memory at cycle start", "parser", parserName)
}

// IncrementalParserState holds common state for incremental parsing
type IncrementalParserState struct {
	Mu            sync.Mutex
	Ctx           context.Context
	Cancel        context.CancelFunc
	CycleTrigger  chan struct{}
}

// NewIncrementalParserState creates a new incremental parser state
func NewIncrementalParserState(ctx context.Context) *IncrementalParserState {
	incCtx, cancel := context.WithCancel(ctx)
	return &IncrementalParserState{
		Ctx:          incCtx,
		Cancel:       cancel,
		CycleTrigger: make(chan struct{}, 1),
	}
}

// IsRunning checks if incremental parsing is already running
func (s *IncrementalParserState) IsRunning() bool {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	return s.Cancel != nil
}

// Start initializes incremental parsing state and triggers first cycle
func (s *IncrementalParserState) Start(parserName string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	
	if s.Cancel != nil {
		slog.Warn("Incremental parsing already started, skipping", "parser", parserName)
		return nil
	}
	
	// This should be called after NewIncrementalParserState, so state is already initialized
	// Trigger first cycle immediately
	select {
	case s.CycleTrigger <- struct{}{}:
		slog.Info("Triggered initial incremental parsing cycle", "parser", parserName)
	default:
	}
	
	return nil
}

// Stop stops incremental parsing
func (s *IncrementalParserState) Stop(parserName string) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	
	if s.Cancel != nil {
		s.Cancel()
		s.Cancel = nil
	}
}

// TriggerNewCycle signals the parser to start a new parsing cycle (non-blocking)
func (s *IncrementalParserState) TriggerNewCycle(parserName string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	
	if s.CycleTrigger == nil {
		slog.Error("Cannot trigger cycle - incremental parsing not started", "parser", parserName)
		return fmt.Errorf("incremental parsing not started")
	}
	
	// Non-blocking trigger
	select {
	case s.CycleTrigger <- struct{}{}:
		slog.Info("Triggered new incremental parsing cycle", "parser", parserName)
		return nil
	default:
		// Cycle already triggered, skip
		slog.Debug("Cycle already triggered, skipping duplicate trigger", "parser", parserName)
		return nil
	}
}

// CreateCycleContext creates a context for a parsing cycle with optional timeout
func CreateCycleContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return ctx, func() {} // No-op cancel function
}

// LogCycleStart logs the start of an incremental cycle
func LogCycleStart(parserName string, cycleID int64, timeout time.Duration) {
	if timeout > 0 {
		slog.Info("Starting incremental cycle", "parser", parserName, "cycle_id", cycleID, "timeout", timeout)
	} else {
		slog.Info("Starting incremental cycle", "parser", parserName, "cycle_id", cycleID, "timeout", "unlimited")
	}
}

// LogCycleFinish logs the finish of an incremental cycle
func LogCycleFinish(parserName string, cycleID int64, duration time.Duration) {
	slog.Info("Incremental cycle finished", "parser", parserName, "cycle_id", cycleID, "duration", duration, "duration_sec", duration.Seconds())
}

// LogIncrementalLoopStart logs the start of incremental parsing loop
func LogIncrementalLoopStart(parserName string, timeout time.Duration) {
	if timeout > 0 {
		slog.Info("Incremental parsing loop started", "parser", parserName, "timeout", timeout)
	} else {
		slog.Info("Incremental parsing loop started", "parser", parserName, "timeout", "unlimited")
	}
}

// LogIncrementalLoopStop logs the stop of incremental parsing loop
func LogIncrementalLoopStop(parserName string, totalCycles int) {
	slog.Info("Incremental parsing loop stopped", "parser", parserName, "total_cycles", totalCycles)
}

// RunIncrementalLoop runs the incremental parsing loop
// cycleFunc is called for each cycle and should implement the actual parsing logic
func RunIncrementalLoop(ctx context.Context, timeout time.Duration, parserName string, state *IncrementalParserState, cycleFunc func(context.Context, time.Duration)) {
	LogIncrementalLoopStart(parserName, timeout)
	cycleCount := 0
	
	for {
		select {
		case <-ctx.Done():
			LogIncrementalLoopStop(parserName, cycleCount)
			return
		case <-state.CycleTrigger:
			cycleCount++
			slog.Info("Received cycle trigger", "parser", parserName, "cycle_number", cycleCount)
			// Clear matches from memory before starting new cycle
			ClearMatchesBeforeCycle(parserName)
			// Run the cycle
			cycleFunc(ctx, timeout)
			slog.Info("Cycle completed, waiting for next trigger", "parser", parserName, "cycle_number", cycleCount)
		}
	}
}
