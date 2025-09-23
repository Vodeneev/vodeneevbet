package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"vodeneevbet/internal/pkg/config"
	"vodeneevbet/internal/pkg/storage"
	"vodeneevbet/internal/calculator/calculator"
)

func main() {
	fmt.Println("Starting Value Bet Calculator...")
	
	var configPath string
	flag.StringVar(&configPath, "config", "../../configs/local.yaml", "Path to config file")
	flag.Parse()

	fmt.Printf("Loading config from: %s\n", configPath)
	
	// Загружаем конфигурацию
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	fmt.Println("Config loaded successfully")

	// Подключаемся к YDB
	ydbClient, err := storage.NewYDBWorkingClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()

	// Создаем калькулятор Value Bet
	valueCalculator := calculator.NewValueCalculator(ydbClient, &cfg.ValueCalculator)

	// Создаем контекст с возможностью отмены
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Обработка сигналов для graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping calculator...")
		cancel()
	}()

	// Запускаем калькулятор
	log.Println("Starting Value Bet Calculator...")
	if err := valueCalculator.Start(ctx); err != nil {
		log.Fatalf("Calculator failed: %v", err)
	}

	log.Println("Value Bet Calculator stopped")
}
