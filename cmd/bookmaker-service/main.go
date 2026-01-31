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

	_ "github.com/Vodeneev/vodeneevbet/internal/parser/parsers/all"
)

const (
	defaultConfigPath = "configs/production.yaml"
)

type config struct {
	configPath string
	runFor     time.Duration
	parser     string // Required: single parser name (e.g. "fonbet", "pinnacle", "pinnacle888")
}

func main() {
	if err := run(); err != nil {
		slog.Error("Bookmaker service failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	slog.Info("Starting bookmaker service...")

	cfg := parseFlags()
	if cfg.parser == "" {
		cfg.parser = os.Getenv("BOOKMAKER_PARSER")
	}
	if cfg.parser == "" {
		return fmt.Errorf("parser name is required: use -parser=<name> or BOOKMAKER_PARSER env (e.g. fonbet, pinnacle, pinnacle888)")
	}
	cfg.parser = strings.ToLower(strings.TrimSpace(cfg.parser))

	slog.Info("Loading config", "path", cfg.configPath)
	appConfig, err := pkgconfig.Load(cfg.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	_, err = logging.SetupLogger(&appConfig.Logging, "bookmaker-service")
	if err != nil {
		slog.Warn("Failed to setup logging, continuing with default logger", "error", err)
	} else {
		slog.Info("Logging initialized", "service", "bookmaker-service", "parser", cfg.parser)
	}

	// Run only this parser (ignore bookmaker_services and enabled_parsers)
	appConfig.Parser.BookmakerServices = nil
	appConfig.Parser.EnabledParsers = []string{cfg.parser}

	ps, err := selectParsers(appConfig)
	if err != nil {
		return err
	}
	if len(ps) != 1 {
		return fmt.Errorf("expected exactly one parser for %q, got %d (available: %v)", cfg.parser, len(ps), parsers.AvailableNames())
	}
	slog.Info("Using parser", "parser", ps[0].GetName())

	ctx, cancel := createContext(cfg.runFor)
	defer cancel()
	setupSignalHandler(ctx, cancel)

	interfaceParsers := []interfaces.Parser{ps[0]}
	health.RegisterParsers(interfaceParsers)

	port := appConfig.Health.Port
	if port <= 0 {
		slog.Error("health.port must be specified in config")
		os.Exit(1)
	}
	healthAddr := health.AddrFor(port)

	asyncParsingTimeout := appConfig.Health.AsyncParsingTimeout
	if asyncParsingTimeout <= 0 {
		asyncParsingTimeout = 60 * time.Second
	}

	health.Run(ctx, healthAddr, "bookmaker-service-"+cfg.parser, nil, appConfig.Health.ReadHeaderTimeout, asyncParsingTimeout)

	slog.Info("Starting parser...")
	return runParsers(ctx, interfaceParsers, appConfig, asyncParsingTimeout)
}

func parseFlags() config {
	var cfg config
	defaultConfig := os.Getenv("CONFIG_PATH")
	if defaultConfig == "" {
		defaultConfig = defaultConfigPath
	}
	flag.StringVar(&cfg.configPath, "config", defaultConfig, "Path to config file")
	flag.DurationVar(&cfg.runFor, "run-for", 0, "Auto-stop after duration. 0 = run until SIGINT/SIGTERM")
	flag.StringVar(&cfg.parser, "parser", "", "Parser name (e.g. fonbet, pinnacle, pinnacle888). Can also set BOOKMAKER_PARSER")
	flag.Parse()
	return cfg
}

func selectParsers(cfg *pkgconfig.Config) ([]parsers.Parser, error) {
	available := parsers.Available()
	enabledSet := make(map[string]bool)
	for _, name := range cfg.Parser.EnabledParsers {
		n := strings.ToLower(strings.TrimSpace(name))
		if n != "" {
			enabledSet[n] = true
		}
	}
	if len(enabledSet) == 0 {
		return nil, fmt.Errorf("no parser enabled")
	}
	var unknown []string
	for name := range enabledSet {
		if _, ok := available[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("unknown parsers: %v (available: %v)", unknown, parsers.AvailableNames())
	}
	var ps []parsers.Parser
	for key, ctor := range available {
		if enabledSet[key] {
			ps = append(ps, ctor(cfg))
		}
	}
	return ps, nil
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
			slog.Info("Received shutdown signal", "signal", sig.String())
			cancel()
		case <-ctx.Done():
			signal.Stop(sigChan)
			close(sigChan)
		}
	}()
}

func runParsers(ctx context.Context, interfaceParsers []interfaces.Parser, appConfig *pkgconfig.Config, asyncParsingTimeout time.Duration) error {
	opts := parserutil.AsyncRunOptions()
	opts.LogStart = true
	opts.OnError = func(p interfaces.Parser, err error) {
		slog.Error("Parser failed", "parser", p.GetName(), "error", err)
	}
	_ = parserutil.RunParsers(ctx, interfaceParsers, func(ctx context.Context, p interfaces.Parser) error {
		return p.Start(ctx)
	}, opts)

	parseInterval := appConfig.Parser.Interval
	if parseInterval <= 0 {
		parseInterval = 2 * time.Minute
		slog.Info("parser.interval not set, using default", "interval", parseInterval)
	}
	startPeriodicParsing(ctx, interfaceParsers, parseInterval, asyncParsingTimeout)

	<-ctx.Done()
	slog.Info("Bookmaker service stopped gracefully")
	return nil
}

func startPeriodicParsing(ctx context.Context, parsers []interfaces.Parser, interval time.Duration, timeout time.Duration) {
	opts := parserutil.AsyncRunOptions()
	opts.OnError = func(p interfaces.Parser, err error) {
		slog.Error("Periodic parsing failed", "parser", p.GetName(), "error", err)
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				parseCtx, cancel := context.WithTimeout(context.Background(), timeout)
				opts.WaitForCompletion = true
				_ = parserutil.RunParsers(parseCtx, parsers, func(ctx context.Context, p interfaces.Parser) error {
					return p.ParseOnce(ctx)
				}, opts)
				cancel()
			}
		}
	}()
}
