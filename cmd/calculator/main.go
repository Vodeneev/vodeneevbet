package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Vodeneev/vodeneevbet/internal/calculator/calculator"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/health"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

func main() {
	fmt.Println("Starting Value Bet Calculator...")

	var configPath string
	var healthAddr string
	flag.StringVar(&configPath, "config", "configs/local.yaml", "Path to config file")
	flag.StringVar(&healthAddr, "health-addr", ":8080", "Health server listen address (e.g. :8080)")
	flag.Parse()

	fmt.Printf("Loading config from: %s\n", configPath)

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("Config loaded successfully")

	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()

	valueCalculator := calculator.NewValueCalculator(ydbClient, &cfg.ValueCalculator)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping calculator...")
		cancel()
	}()

	health.Run(ctx, healthAddr, "calculator")

	log.Println("Starting Value Bet Calculator...")
	if err := valueCalculator.Start(ctx); err != nil {
		log.Fatalf("Calculator failed: %v", err)
	}

	log.Println("Value Bet Calculator stopped")
}
