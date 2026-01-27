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

func init() {
	// Set the GetMatches function for handlers
	handlers.SetGetMatchesFunc(GetMatches)
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

	if readHeaderTimeout <= 0 {
		log.Fatalf("health: read_header_timeout must be specified in config")
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
		log.Printf("%s: health server listening on %s", service, addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("%s: health server error: %v", service, err)
		}
	}()
}

func AddrFor(port int) string {
	if port <= 0 {
		log.Fatalf("health: port must be greater than 0")
	}
	return fmt.Sprintf(":%d", port)
}
