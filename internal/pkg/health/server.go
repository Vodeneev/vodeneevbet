package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/performance"
)

// Run starts a small HTTP server with /ping, /health, and /metrics endpoints.
// It stops gracefully when ctx is canceled.
func Run(ctx context.Context, addr string, service string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("pong\n"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		tracker := performance.GetTracker()
		metrics := tracker.GetMetrics()
		
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(metrics); err != nil {
			http.Error(w, fmt.Sprintf("failed to encode metrics: %v", err), http.StatusInternalServerError)
			return
		}
	})

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
