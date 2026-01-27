package parserutil

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
)

// ParserFunc is a function that runs a parser and returns an error
type ParserFunc func(ctx context.Context, p interfaces.Parser) error

// RunOptions configures how parsers should be run
type RunOptions struct {
	// LogStart logs when each parser starts (e.g., "Starting %s parser...")
	LogStart bool
	// OnError is called when a parser returns an error. If nil, errors are logged.
	OnError func(p interfaces.Parser, err error)
	// OnComplete is called when all parsers have completed (only if WaitForCompletion is false).
	// Useful for cleanup tasks like canceling contexts.
	OnComplete func()
	// WaitForCompletion if true, waits for all parsers to complete before returning.
	// If false, returns immediately after starting all parsers.
	WaitForCompletion bool
	// ReturnFirstError if true and WaitForCompletion is true, returns the first error encountered.
	// If false, all errors are handled via OnError callback.
	ReturnFirstError bool
}

// DefaultRunOptions returns default options for running parsers
func DefaultRunOptions() RunOptions {
	return RunOptions{
		LogStart:          true,
		OnError:           nil, // Will use default logging
		WaitForCompletion: true,
		ReturnFirstError:  true,
	}
}

// AsyncRunOptions returns options for async (non-blocking) execution
func AsyncRunOptions() RunOptions {
	return RunOptions{
		LogStart:          false,
		OnError:           nil, // Will use default logging
		WaitForCompletion: false,
		ReturnFirstError:  false,
	}
}

// RunParsers runs all parsers in parallel using the provided function
func RunParsers(ctx context.Context, parsers []interfaces.Parser, parserFunc ParserFunc, opts RunOptions) error {
	if len(parsers) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	errCh := make(chan error, 1)

	// Default error handler logs errors
	onError := opts.OnError
	if onError == nil {
		onError = func(p interfaces.Parser, err error) {
			log.Printf("%s parser failed: %v", p.GetName(), err)
		}
	}

	// Start all parsers in parallel
	for _, p := range parsers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()

			if opts.LogStart {
				log.Printf("Starting %s parser...", p.GetName())
			}

			if err := parserFunc(ctx, p); err != nil && ctx.Err() == nil {
				onError(p, err)

				// If we need to return first error, capture it
				if opts.ReturnFirstError {
					once.Do(func() {
						firstErr = fmt.Errorf("%s parser failed: %w", p.GetName(), err)
						errCh <- firstErr
					})
				}
			}
		}()
	}

	// If not waiting for completion, set up completion callback and return immediately
	if !opts.WaitForCompletion {
		if opts.OnComplete != nil {
			go func() {
				wg.Wait()
				opts.OnComplete()
			}()
		}
		return nil
	}

	// Wait for all parsers to complete in separate goroutine
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	// Wait for either an error or all parsers to complete
	select {
	case err := <-errCh:
		// Parser failed - wait for graceful shutdown of all parsers
		if opts.ReturnFirstError {
			<-doneCh
			return err
		}
		// If not returning errors, just wait for completion
		<-doneCh
		return nil
	case <-doneCh:
		// All parsers completed successfully or via context cancellation
		return nil
	}
}
