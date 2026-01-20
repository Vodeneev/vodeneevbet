package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers/fonbet"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers/pinnacle"
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

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("Config loaded successfully")

	var p parsers.Parser
	parserType := cfg.Parser.Type
	if parserType == "" {
		parserType = "fonbet"
	}

	switch parserType {
	case "fonbet":
		p = fonbet.NewParserWrapper(cfg)
	case "pinnacle":
		p = pinnacle.NewParserWrapper(cfg)
	default:
		log.Fatalf("Unknown parser type: %s", parserType)
	}

	fmt.Printf("Using parser: %s\n", p.GetName())

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

	log.Println("Starting parser...")
	if err := p.Start(ctx); err != nil {
		log.Fatalf("Parser failed: %v", err)
	}

	log.Println("Parser stopped")
}
