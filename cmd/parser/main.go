package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
		log.Fatalf("Parser failed: %v", err)
	}
}

func run() error {
	fmt.Println("Starting parser...")

	cfg := parseFlags()
	fmt.Printf("Loading config from: %s\n", cfg.configPath)

	appConfig, err := pkgconfig.Load(cfg.configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println("Config loaded successfully")

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
		log.Fatalf("parser: health.port must be specified in config")
	}
	healthAddr := health.AddrFor(port)
	
	// Use async_parsing_timeout from config, default to 60s if not specified
	asyncParsingTimeout := appConfig.Health.AsyncParsingTimeout
	if asyncParsingTimeout <= 0 {
		asyncParsingTimeout = 60 * time.Second
	}
	
	health.Run(ctx, healthAddr, "parser", nil, appConfig.Health.ReadHeaderTimeout, asyncParsingTimeout)

	log.Println("Starting parsers...")
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
	fmt.Printf("Using parsers: %s\n", strings.Join(names, ", "))
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
			log.Printf("Received shutdown signal (%s), stopping parser...", sig)
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
		log.Printf("Parser %s failed: %v", p.GetName(), err)
	}
	err := parserutil.RunParsers(ctx, interfaceParsers, func(ctx context.Context, p interfaces.Parser) error {
		return p.Start(ctx)
	}, opts)

	if err != nil {
		log.Printf("Parser error detected: %v", err)
		return err
	}

	// Start periodic background parsing
	parseInterval := appConfig.Parser.Interval
	if parseInterval <= 0 {
		parseInterval = 10 * time.Second
		log.Printf("parser.interval not set, using default: %v", parseInterval)
	} else {
		log.Printf("Starting periodic parsing with interval: %v", parseInterval)
	}

	startPeriodicParsing(ctx, interfaceParsers, parseInterval, asyncParsingTimeout)

	// Wait for context cancellation
	<-ctx.Done()
	log.Println("Parser stopped gracefully")
	return nil
}

func startPeriodicParsing(ctx context.Context, parsers []interfaces.Parser, interval time.Duration, timeout time.Duration) {
	// Helper function to create async parsing options with error handling
	createAsyncOpts := func() parserutil.RunOptions {
		opts := parserutil.AsyncRunOptions()
		opts.OnError = func(p interfaces.Parser, err error) {
			log.Printf("Periodic parsing failed for %s: %v", p.GetName(), err)
		}
		return opts
	}

	// Start periodic parsing loop
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Println("Stopping periodic parsing...")
				return
			case <-ticker.C:
				log.Printf("Running periodic parsing (interval: %v)...", interval)
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
