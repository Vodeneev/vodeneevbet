package health

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/health/handlers"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
)

// RegisterParsers registers parsers for on-demand parsing
// This is a convenience wrapper that delegates to handlers.RegisterParsers
func RegisterParsers(parsers []interfaces.Parser) {
	handlers.RegisterParsers(parsers)
}

func init() {
	// Set the GetMatches function for handlers
	handlers.SetGetMatchesFunc(GetMatches)
}

// Run starts a small HTTP server with /ping, /health, /metrics, and /matches endpoints.
// It stops gracefully when ctx is canceled.
// storage parameter is kept for backward compatibility but not used (matches come from in-memory store)
func Run(ctx context.Context, addr string, service string, storage interfaces.Storage) {
	mux := http.NewServeMux()
	
	// Health endpoints
	mux.HandleFunc("/ping", handlers.HandlePing)
	mux.HandleFunc("/health", handlers.HandleHealth)
	
	// Metrics endpoint
	mux.HandleFunc("/metrics", handlers.HandleMetrics)
	
	// Matches endpoint
	mux.HandleFunc("/matches", handlers.HandleMatches)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	go func() {
		log.Printf("%s: health server listening on %s", service, addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("%s: health server error: %v", service, err)
		}
	}()
}

// AddrFor returns a consistent default health listen address.
func AddrFor(service string) string {
	// Keep as :8080 inside container; publishing is handled by docker-compose.
	_ = service
	return fmt.Sprintf(":%d", 8080)
}
