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
	"vodeneevbet/internal/parser/parsers"
)

func main() {
	fmt.Println("Starting parser...")
	
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

	// Создаем парсер для тестовой БК (пока заглушка)
	parser := parsers.NewTestParser(ydbClient, cfg)

	// Создаем контекст с возможностью отмены
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Обработка сигналов для graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Received shutdown signal, stopping parser...")
		cancel()
	}()

	// Запускаем парсер
	log.Println("Starting parser...")
	if err := parser.Start(ctx); err != nil {
		log.Fatalf("Parser failed: %v", err)
	}

	log.Println("Parser stopped")
}
