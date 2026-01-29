package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers"
	pkgconfig "github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/logging"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/parserutil"

	// Register all supported parsers via init().
	_ "github.com/Vodeneev/vodeneevbet/internal/parser/parsers/all"
)

const (
	defaultConfigPath = "configs/production.yaml"
)

type config struct {
	configPath string
	runFor     time.Duration
	parser     string // Override enabled_parsers from config (e.g. "fonbet" or "pinnacle")
}

func main() {
	if err := run(); err != nil {
		slog.Error("Parser failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	slog.Info("Starting parser...")

	cfg := parseFlags()
	slog.Info("Loading config", "path", cfg.configPath)

	appConfig, err := pkgconfig.Load(cfg.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Настраиваем логирование с поддержкой Yandex Cloud Logging
	_, err = logging.SetupLogger(&appConfig.Logging, "parser")
	if err != nil {
		slog.Warn("Failed to setup logging, continuing with default logger", "error", err)
	} else {
		slog.Info("Logging initialized", "service", "parser")
	}

	slog.Info("Config loaded successfully")

	// Override enabled_parsers from command line if specified
	if cfg.parser != "" {
		appConfig.Parser.EnabledParsers = []string{cfg.parser}
	}
	ps, err := selectParsers(appConfig)
	if err != nil {
		return err
	}
	printSelectedParsers(ps)

	ctx, cancel := createContext(cfg.runFor)
	defer cancel()

	setupSignalHandler(ctx, cancel)

	interfaceParsers := make([]interfaces.Parser, len(ps))
	for i, p := range ps {
		interfaceParsers[i] = p
	}
	health.RegisterParsers(interfaceParsers)

	port := appConfig.Health.Port
	if port <= 0 {
		slog.Error("health.port must be specified in config")
		os.Exit(1)
	}
	healthAddr := health.AddrFor(port)

	// Use async_parsing_timeout from config, default to 60s if not specified
	asyncParsingTimeout := appConfig.Health.AsyncParsingTimeout
	if asyncParsingTimeout <= 0 {
		asyncParsingTimeout = 60 * time.Second
	}

	health.Run(ctx, healthAddr, "parser", nil, appConfig.Health.ReadHeaderTimeout, asyncParsingTimeout)

	slog.Info("Starting parsers...")
	return runParsers(ctx, ps, appConfig, asyncParsingTimeout)
}

func parseFlags() config {
	var cfg config

	defaultConfig := os.Getenv("CONFIG_PATH")
	if defaultConfig == "" {
		defaultConfig = defaultConfigPath
	}

	flag.StringVar(&cfg.configPath, "config", defaultConfig, "Path to config file (can be set via CONFIG_PATH env var)")
	flag.DurationVar(&cfg.runFor, "run-for", 0, "Auto-stop after duration (e.g. 10s, 1m). 0 = run until SIGINT/SIGTERM")
	flag.StringVar(&cfg.parser, "parser", "", "Override enabled_parsers: specify parser name (e.g. 'fonbet' or 'pinnacle'). Empty = use config")
	flag.Parse()
	return cfg
}

func selectParsers(cfg *pkgconfig.Config) ([]parsers.Parser, error) {
	available := parsers.Available()

	// If enabled_parsers is not specified in config, run all available parsers
	enabledSet := buildEnabledSet(cfg.Parser.EnabledParsers)

	if err := validateEnabledParsers(enabledSet, available); err != nil {
		return nil, err
	}

	ps := createParsers(available, enabledSet, cfg)

	if len(ps) == 0 {
		return nil, fmt.Errorf("no parsers selected to run (parser.enabled_parsers=%v)", cfg.Parser.EnabledParsers)
	}

	return ps, nil
}

func buildEnabledSet(enabledParsers []string) map[string]bool {
	enabledSet := make(map[string]bool)
	for _, name := range enabledParsers {
		n := strings.ToLower(strings.TrimSpace(name))
		if n != "" {
			enabledSet[n] = true
		}
	}
	return enabledSet
}

func validateEnabledParsers(enabledSet map[string]bool, available map[string]parsers.Factory) error {
	if len(enabledSet) == 0 {
		return nil
	}

	var unknown []string
	for name := range enabledSet {
		if _, ok := available[name]; !ok {
			unknown = append(unknown, name)
		}
	}

	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown parsers in parser.enabled_parsers: %v (available: %v)", unknown, parsers.AvailableNames())
	}

	return nil
}

func createParsers(available map[string]parsers.Factory, enabledSet map[string]bool, cfg *pkgconfig.Config) []parsers.Parser {
	var ps []parsers.Parser
	for key, ctor := range available {
		if len(enabledSet) == 0 || enabledSet[key] {
			ps = append(ps, ctor(cfg))
		}
	}
	return ps
}

func printSelectedParsers(ps []parsers.Parser) {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.GetName())
	}
	sort.Strings(names)
	slog.Info("Using parsers", "parsers", strings.Join(names, ", "))
}

func createContext(runFor time.Duration) (context.Context, context.CancelFunc) {
	if runFor > 0 {
		return context.WithTimeout(context.Background(), runFor)
	}
	return context.WithCancel(context.Background())
}

func setupSignalHandler(ctx context.Context, cancel context.CancelFunc) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case sig := <-sigChan:
			slog.Info("Received shutdown signal, stopping parser...", "signal", sig.String())
			cancel()
		case <-ctx.Done():
			// Context already cancelled (timeout or parent cancellation)
			signal.Stop(sigChan)
			close(sigChan)
		}
	}()
}

func runParsers(ctx context.Context, ps []parsers.Parser, appConfig *pkgconfig.Config, asyncParsingTimeout time.Duration) error {
	interfaceParsers := make([]interfaces.Parser, len(ps))
	for i, p := range ps {
		interfaceParsers[i] = p
	}

	// Start parsers in background (they will wait for context cancellation)
	// Use async options because Start() is a long-running process that waits for context cancellation
	opts := parserutil.AsyncRunOptions()
	opts.LogStart = true // Log when parsers start
	opts.OnError = func(p interfaces.Parser, err error) {
		slog.Error("Parser failed", "parser", p.GetName(), "error", err)
	}
	err := parserutil.RunParsers(ctx, interfaceParsers, func(ctx context.Context, p interfaces.Parser) error {
		return p.Start(ctx)
	}, opts)

	if err != nil {
		slog.Error("Parser error detected", "error", err)
		return err
	}

	// Start periodic background parsing
	parseInterval := appConfig.Parser.Interval
	if parseInterval <= 0 {
		parseInterval = 2 * time.Minute
		slog.Info("parser.interval not set, using default", "interval", parseInterval)
	} else {
		slog.Info("Starting periodic parsing", "interval", parseInterval)
	}

	startPeriodicParsing(ctx, interfaceParsers, parseInterval, asyncParsingTimeout)

	// Wait for context cancellation
	<-ctx.Done()
	slog.Info("Parser stopped gracefully")
	return nil
}

func startPeriodicParsing(ctx context.Context, parsers []interfaces.Parser, interval time.Duration, timeout time.Duration) {
	// Helper function to create async parsing options with error handling
	createAsyncOpts := func() parserutil.RunOptions {
		opts := parserutil.AsyncRunOptions()
		opts.OnError = func(p interfaces.Parser, err error) {
			slog.Error("Periodic parsing failed", "parser", p.GetName(), "error", err)
		}
		return opts
	}

	// Start periodic parsing loop
	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("Stopping periodic parsing...")
				return
			case <-ticker.C:
				runParsingOnce(parsers, timeout, createAsyncOpts())
			}
		}
	}()
}

func runParsingOnce(parsers []interfaces.Parser, timeout time.Duration, opts parserutil.RunOptions) {
	parseCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_ = parserutil.RunParsers(parseCtx, parsers, func(ctx context.Context, p interfaces.Parser) error {
		return p.ParseOnce(ctx)
	}, opts)
}
