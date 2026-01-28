package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/calculator/calculator"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/logging"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

const (
	defaultConfigPath = "configs/production.yaml"
)

func main() {
	fmt.Println("Starting Value Bet Calculator...")

	var configPath string
	var healthAddr string

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

	// Настраиваем логирование с поддержкой Yandex Cloud Logging
	_, err = logging.SetupLogger(&cfg.Logging, "calculator")
	if err != nil {
		log.Printf("Warning: failed to setup logging: %v, continuing with default logger", err)
	} else {
		slog.Info("Logging initialized", "service", "calculator")
	}

	fmt.Println("Config loaded successfully")

	if cfg.ValueCalculator.ParserURL == "" {
		slog.Error("parser_url is required in config")
		log.Fatalf("calculator: parser_url is required in config")
	}
	slog.Info("Using parser URL", "url", cfg.ValueCalculator.ParserURL)
	log.Printf("calculator: using parser URL %s", cfg.ValueCalculator.ParserURL)

	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		cfg.ValueCalculator.TelegramBotToken = token
		slog.Info("Using Telegram bot token from environment")
		log.Println("calculator: using Telegram bot token from environment")
	}
	if chatIDStr := os.Getenv("TELEGRAM_CHAT_ID"); chatIDStr != "" {
		if chatID, err := strconv.ParseInt(chatIDStr, 10, 64); err == nil {
			cfg.ValueCalculator.TelegramChatID = chatID
			slog.Info("Using Telegram chat ID from environment", "chat_id", chatID)
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

		// Clean diff_bets table on startup to prevent stale data from blocking alerts
		log.Println("calculator: cleaning diff_bets table on startup...")
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		if err := pgStorage.CleanDiffBets(cleanCtx); err != nil {
			log.Printf("calculator: warning: failed to clean diff_bets table: %v", err)
			// Don't fail startup if cleanup fails, but log warning
		} else {
			log.Println("calculator: diff_bets table cleaned successfully")
		}
	}

	valueCalculator := calculator.NewValueCalculator(&cfg.ValueCalculator, diffStorage)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received shutdown signal, stopping calculator...")
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
		slog.Info("HTTP server listening", "addr", healthAddr)
		log.Printf("calculator: http server listening on %s", healthAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
			log.Printf("calculator: http server error: %v", err)
		}
	}()

	slog.Info("Starting Value Bet Calculator...")
	log.Println("Starting Value Bet Calculator...")
	if err := valueCalculator.Start(ctx); err != nil {
		slog.Error("Calculator failed", "error", err)
		log.Fatalf("Calculator failed: %v", err)
	}

	slog.Info("Value Bet Calculator stopped")
	log.Println("Value Bet Calculator stopped")
}
