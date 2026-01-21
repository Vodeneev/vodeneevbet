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
	"sync"
	"syscall"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers"
	pkgconfig "github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"

	// Register all supported parsers via init().
	_ "github.com/Vodeneev/vodeneevbet/internal/parser/parsers/all"
)

const (
	defaultConfigPath = "configs/production.yaml"
)

type config struct {
	configPath string
	healthAddr string
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

	health.Run(ctx, cfg.healthAddr, "parser")

	log.Println("Starting parsers...")
	return runParsers(ctx, ps)
}

func parseFlags() config {
	var cfg config
	
	// Get default config path from environment or use default
	defaultConfig := os.Getenv("CONFIG_PATH")
	if defaultConfig == "" {
		defaultConfig = defaultConfigPath
	}
	
	flag.StringVar(&cfg.configPath, "config", defaultConfig, "Path to config file (can be set via CONFIG_PATH env var)")
	flag.StringVar(&cfg.healthAddr, "health-addr", ":8080", "Health server listen address (e.g. :8080)")
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

func runParsers(ctx context.Context, ps []parsers.Parser) error {
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error

	errCh := make(chan error, 1)

	for _, p := range ps {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("Starting %s parser...", p.GetName())
			if err := p.Start(ctx); err != nil && ctx.Err() == nil {
				// Only record the first error and notify via channel once
				once.Do(func() {
					firstErr = fmt.Errorf("%s parser failed: %w", p.GetName(), err)
					errCh <- firstErr
				})
			}
		}()
	}

	// Wait for all parsers to complete in separate goroutine
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	// Wait for either an error or all parsers to complete
	select {
	case err := <-errCh:
		// Parser failed - wait for graceful shutdown of all parsers
		log.Printf("Parser error detected: %v", err)
		<-doneCh
		return err
	case <-doneCh:
		// All parsers completed successfully or via context cancellation
		if ctx.Err() != nil {
			log.Println("Parser stopped gracefully")
			return nil
		}
		log.Println("Parser stopped")
		return nil
	}
}
