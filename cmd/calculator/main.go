package main

import (
	"context"
	"flag"
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
	slog.Info("Starting Value Bet Calculator...")

	var configPath string
	var healthAddr string

	defaultConfig := os.Getenv("CONFIG_PATH")
	if defaultConfig == "" {
		defaultConfig = defaultConfigPath
	}

	flag.StringVar(&configPath, "config", defaultConfig, "Path to config file (can be set via CONFIG_PATH env var)")
	flag.StringVar(&healthAddr, "health-addr", ":8080", "Health server listen address (e.g. :8080)")
	flag.Parse()

	slog.Info("Loading config", "path", configPath)

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// Настраиваем логирование с поддержкой Yandex Cloud Logging
	_, err = logging.SetupLogger(&cfg.Logging, "calculator")
	if err != nil {
		slog.Warn("Failed to setup logging, continuing with default logger", "error", err)
	} else {
		slog.Info("Logging initialized", "service", "calculator")
	}

	slog.Info("Config loaded successfully")

	if cfg.ValueCalculator.ParserURL == "" {
		slog.Error("parser_url is required in config")
		os.Exit(1)
	}
	slog.Info("Using parser URL", "url", cfg.ValueCalculator.ParserURL)

	if token := os.Getenv("TELEGRAM_BOT_TOKEN"); token != "" {
		cfg.ValueCalculator.TelegramBotToken = token
		slog.Info("Using Telegram bot token from environment")
	}
	if chatIDStr := os.Getenv("TELEGRAM_CHAT_ID"); chatIDStr != "" {
		if chatID, err := strconv.ParseInt(chatIDStr, 10, 64); err == nil {
			cfg.ValueCalculator.TelegramChatID = chatID
			slog.Info("Using Telegram chat ID from environment", "chat_id", chatID)
		}
	}

	// Initialize PostgreSQL storage for diffs if async is enabled
	var diffStorage storage.DiffBetStorage
	var oddsSnapshotStorage storage.OddsSnapshotStorage
	if cfg.ValueCalculator.AsyncEnabled {
		// Allow DSN override via environment variable
		postgresDSN := cfg.Postgres.DSN
		if envDSN := os.Getenv("POSTGRES_DSN"); envDSN != "" {
			postgresDSN = envDSN
			slog.Info("Using PostgreSQL DSN from POSTGRES_DSN environment variable")
		}

		if postgresDSN == "" {
			slog.Error("postgres DSN is required when async is enabled. Set it in config or POSTGRES_DSN env var")
			os.Exit(1)
		}

		pgConfig := cfg.Postgres
		pgConfig.DSN = postgresDSN

		slog.Info("Initializing PostgreSQL diff storage...")
		pgStorage, err := storage.NewPostgresDiffStorage(&pgConfig)
		if err != nil {
			slog.Error("Failed to initialize PostgreSQL storage", "error", err)
			os.Exit(1)
		}
		diffStorage = pgStorage
		defer func() {
			if err := pgStorage.Close(); err != nil {
				slog.Error("Error closing PostgreSQL storage", "error", err)
			}
		}()
		slog.Info("PostgreSQL diff storage initialized")

		// Clean diff_bets table on startup to prevent stale data from blocking alerts
		slog.Info("Cleaning diff_bets table on startup...")
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		if err := pgStorage.CleanDiffBets(cleanCtx); err != nil {
			slog.Warn("Failed to clean diff_bets table", "error", err)
		} else {
			slog.Info("diff_bets table cleaned successfully")
		}

		// Odds snapshot storage for line movement (прогрузы) tracking
		if cfg.ValueCalculator.LineMovementEnabled {
			slog.Info("Initializing PostgreSQL odds snapshot storage for line movement...")
			oddsPg, err := storage.NewPostgresOddsSnapshotStorage(&pgConfig)
			if err != nil {
				slog.Error("Failed to initialize odds snapshot storage", "error", err)
				os.Exit(1)
			}
			oddsSnapshotStorage = oddsPg
			defer func() {
				_ = oddsPg.Close()
			}()
			slog.Info("PostgreSQL odds snapshot storage initialized")
		}
	}

	valueCalculator := calculator.NewValueCalculator(&cfg.ValueCalculator, diffStorage, oddsSnapshotStorage)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		slog.Info("Received shutdown signal, stopping calculator...")
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
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP server error", "error", err)
		}
	}()

	slog.Info("Starting Value Bet Calculator...")
	if err := valueCalculator.Start(ctx); err != nil {
		slog.Error("Calculator failed", "error", err)
		os.Exit(1)
	}

	slog.Info("Value Bet Calculator stopped")
}
