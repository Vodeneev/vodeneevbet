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

func main() {
	fmt.Println("Starting parser...")

	var configPath string
	var healthAddr string
	var runFor time.Duration
	flag.StringVar(&configPath, "config", "configs/local.yaml", "Path to config file")
	flag.StringVar(&healthAddr, "health-addr", ":8080", "Health server listen address (e.g. :8080)")
	flag.DurationVar(&runFor, "run-for", 0, "Auto-stop after duration (e.g. 10s, 1m). 0 = run until SIGINT/SIGTERM")
	flag.Parse()

	fmt.Printf("Loading config from: %s\n", configPath)

	cfg, err := pkgconfig.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("Config loaded successfully")

	available := parsers.Available()

	// Which parsers to run is configured in config: `parser.enabled_parsers`.
	// If empty, we run all available parsers.
	enabledSet := map[string]bool{}
	for _, name := range cfg.Parser.EnabledParsers {
		n := strings.ToLower(strings.TrimSpace(name))
		if n != "" {
			enabledSet[n] = true
		}
	}

	var ps []parsers.Parser
	if len(enabledSet) > 0 {
		// Validate configured names to avoid silently skipping typos.
		var unknown []string
		for name := range enabledSet {
			if _, ok := available[name]; !ok {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			log.Fatalf("Unknown parsers in parser.enabled_parsers: %v (available: %v)", unknown, parsers.AvailableNames())
		}
	}

	for key, ctor := range available {
		if len(enabledSet) == 0 || enabledSet[key] {
			ps = append(ps, ctor(cfg))
		}
	}

	if len(ps) == 0 {
		log.Fatalf("No parsers selected to run (parser.enabled_parsers=%v)", cfg.Parser.EnabledParsers)
	}

	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.GetName())
	}
	sort.Strings(names)
	fmt.Printf("Using parsers: %s\n", strings.Join(names, ", "))

	ctx := context.Background()
	var cancel context.CancelFunc = func() {}
	if runFor > 0 {
		ctx, cancel = context.WithTimeout(ctx, runFor)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping parser...")
		cancel()
	}()

	health.Run(ctx, healthAddr, "parser")

	log.Println("Starting parsers...")

	errCh := make(chan error, len(ps))
	var wg sync.WaitGroup
	for _, p := range ps {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("Starting %s parser...", p.GetName())
			if err := p.Start(ctx); err != nil && ctx.Err() == nil {
				errCh <- fmt.Errorf("%s parser failed: %w", p.GetName(), err)
			}
		}()
	}

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case err := <-errCh:
		// If any parser fails, stop the whole service.
		log.Printf("Parser failed: %v", err)
		cancel()
		<-doneCh
		log.Fatalf("Parsers stopped due to error: %v", err)
	case <-ctx.Done():
		// Graceful shutdown: wait for all parsers to stop on ctx cancellation.
		<-doneCh
	}

	log.Println("Parser stopped")
}
