package parserutil

import (
	"context"
	"log/slog"
	"sync"

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

	// Start all parsers in parallel (async, non-blocking)
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

	// Return immediately - parsing happens in background
	return nil
}
