package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers/fonbet"
)

func main() {
	fmt.Println("Starting parser...")
	
	var configPath string
	flag.StringVar(&configPath, "config", "../../configs/local.yaml", "Path to config file")
	flag.Parse()

	fmt.Printf("Loading config from: %s\n", configPath)
	
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	fmt.Println("Config loaded successfully")

	var parser parsers.Parser
	parserType := cfg.Parser.Type
	if parserType == "" {
		parserType = "test"
	}
	
	switch parserType {
	case "fonbet":
		parser = fonbet.NewParserWrapper(cfg)
	case "test":
		parser = parsers.NewTestParser(cfg)
	default:
		log.Fatalf("Unknown parser type: %s", parserType)
	}
	
	fmt.Printf("Using parser: %s\n", parser.GetName())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping parser...")
		cancel()
	}()

	log.Println("Starting parser...")
	if err := parser.Start(ctx); err != nil {
		log.Fatalf("Parser failed: %v", err)
	}

	log.Println("Parser stopped")
}
