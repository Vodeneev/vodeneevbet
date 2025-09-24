package main

import (
	"fmt"
	"log"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

func main() {
	fmt.Println("=== Debug Parser ===")
	
	// Загружаем конфигурацию
	cfg, err := config.Load("configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	fmt.Println("Config loaded successfully")
	fmt.Printf("YDB endpoint: %s\n", cfg.YDB.Endpoint)
	fmt.Printf("YDB database: %s\n", cfg.YDB.Database)
	
	// Создаем YDB клиент
	ydbClient, err := storage.NewYDBSimpleClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to create YDB client: %v", err)
	}
	defer ydbClient.Close()
	
	fmt.Println("YDB client created successfully")
	fmt.Println("=== Debug Complete ===")
}
