package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/calculator/calculator"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
)

const (
	defaultConfigPath = "configs/production.yaml"
)

func main() {
	fmt.Println("Starting Value Bet Calculator...")

	var configPath string
	var healthAddr string

	// Get default config path from environment or use default
	defaultConfig := os.Getenv("CONFIG_PATH")
	if defaultConfig == "" {
		defaultConfig = defaultConfigPath
	}

	flag.StringVar(&configPath, "config", defaultConfig, "Path to config file (can be set via CONFIG_PATH env var)")
	flag.StringVar(&healthAddr, "health-addr", ":8080", "Health server listen address (e.g. :8080)")
	flag.Parse()

	fmt.Printf("Loading config from: %s\n", configPath)

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("Config loaded successfully")

	if cfg.ValueCalculator.ParserURL == "" {
		log.Fatalf("calculator: parser_url is required in config")
	}
	log.Printf("calculator: using parser URL %s", cfg.ValueCalculator.ParserURL)

	valueCalculator := calculator.NewValueCalculator(&cfg.ValueCalculator)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping calculator...")
		cancel()
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("pong\n"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	valueCalculator.RegisterHTTP(mux)

	srv := &http.Server{
		Addr:              healthAddr,
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
		log.Printf("calculator: http server listening on %s", healthAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("calculator: http server error: %v", err)
		}
	}()

	log.Println("Starting Value Bet Calculator...")
	if err := valueCalculator.Start(ctx); err != nil {
		log.Fatalf("Calculator failed: %v", err)
	}

	log.Println("Value Bet Calculator stopped")
}
