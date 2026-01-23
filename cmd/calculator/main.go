package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/calculator/calculator"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
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

	// Override Telegram settings from environment if provided
	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		cfg.ValueCalculator.TelegramBotToken = token
		log.Println("calculator: using Telegram bot token from environment")
	}
	if chatIDStr := os.Getenv("TELEGRAM_CHAT_ID"); chatIDStr != "" {
		if chatID, err := strconv.ParseInt(chatIDStr, 10, 64); err == nil {
			cfg.ValueCalculator.TelegramChatID = chatID
			log.Printf("calculator: using Telegram chat ID from environment: %d", chatID)
		}
	}

	// Initialize PostgreSQL storage for diffs if async is enabled
	var diffStorage storage.DiffBetStorage
	if cfg.ValueCalculator.AsyncEnabled {
		// Allow DSN override via environment variable
		postgresDSN := cfg.Postgres.DSN
		if envDSN := os.Getenv("POSTGRES_DSN"); envDSN != "" {
			postgresDSN = envDSN
			log.Println("calculator: using PostgreSQL DSN from POSTGRES_DSN environment variable")
		}
		
		if postgresDSN == "" {
			log.Fatalf("calculator: postgres DSN is required when async is enabled. Set it in config or POSTGRES_DSN env var")
		}
		
		log.Println("calculator: initializing PostgreSQL diff storage...")
		pgConfig := cfg.Postgres
		pgConfig.DSN = postgresDSN
		pgStorage, err := storage.NewPostgresDiffStorage(&pgConfig)
		if err != nil {
			log.Fatalf("calculator: failed to initialize PostgreSQL storage: %v", err)
		}
		diffStorage = pgStorage
		defer func() {
			if err := pgStorage.Close(); err != nil {
				log.Printf("calculator: error closing PostgreSQL storage: %v", err)
			}
		}()
		log.Println("calculator: PostgreSQL diff storage initialized")
	}

	valueCalculator := calculator.NewValueCalculator(&cfg.ValueCalculator, diffStorage)

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
