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
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
	"github.com/Vodeneev/vodeneevbet/internal/calculator/calculator"
)

func main() {
	fmt.Println("Starting Value Bet Calculator...")
	
	var configPath string
	flag.StringVar(&configPath, "config", "../../configs/local.yaml", "Path to config file")
	flag.Parse()

	fmt.Printf("Loading config from: %s\n", configPath)
	
	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	fmt.Println("Config loaded successfully")

	// Connect to YDB
	ydbClient, err := storage.NewYDBWorkingClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()

	// Create Value Bet calculator
	valueCalculator := calculator.NewValueCalculator(ydbClient, &cfg.ValueCalculator)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping calculator...")
		cancel()
	}()

	// Start calculator
	log.Println("Starting Value Bet Calculator...")
	if err := valueCalculator.Start(ctx); err != nil {
		log.Fatalf("Calculator failed: %v", err)
	}

	log.Println("Value Bet Calculator stopped")
}
