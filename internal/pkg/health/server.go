package health

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/health/handlers"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
)

func init() {
	// Set the GetMatches function for handlers
	handlers.SetGetMatchesFunc(GetMatches)
	handlers.SetGetMatchesByNameFunc(GetMatchesByName)
	// Set the GetParsers function for handlers
	handlers.SetGetParsersFunc(GetParsers)
}

func Run(ctx context.Context, addr string, service string, storage interfaces.Storage, readHeaderTimeout time.Duration, parsingTimeout time.Duration) {
	// parsingTimeout parameter kept for backward compatibility but not used
	// (parsing now runs continuously in background, not triggered by requests)
	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/ping", handlers.HandlePing)
	mux.HandleFunc("/health", handlers.HandleHealth)

	// Metrics endpoint
	mux.HandleFunc("/metrics", handlers.HandleMetrics)

	// Matches endpoint
	mux.HandleFunc("/matches", handlers.HandleMatches)

	// Match by name (for testing): returns matches with full events and coefficients
	mux.HandleFunc("/match-by-name", handlers.HandleMatchByName)

	// Manual parse endpoint
	mux.HandleFunc("/parse", handlers.HandleParse)

	if readHeaderTimeout <= 0 {
		slog.Error("read_header_timeout must be specified in config")
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	go func() {
		slog.Info("Health server listening", "service", service, "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Health server error", "service", service, "error", err)
		}
	}()
}

func AddrFor(port int) string {
	if port <= 0 {
		slog.Error("port must be greater than 0")
		os.Exit(1)
	}
	return fmt.Sprintf(":%d", port)
}
