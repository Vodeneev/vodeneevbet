package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"vodeneevbet/internal/pkg/config"
	"vodeneevbet/internal/pkg/models"
	"vodeneevbet/internal/pkg/storage"
)

func main() {
	fmt.Println("=== Testing YDB with Authentication ===")
	
	// Загружаем конфигурацию
	cfg, err := config.Load("configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	fmt.Println("Config loaded successfully")
	fmt.Printf("YDB endpoint: %s\n", cfg.YDB.Endpoint)
	fmt.Printf("YDB database: %s\n", cfg.YDB.Database)
	fmt.Printf("Service account key file: %s\n", cfg.YDB.ServiceAccountKeyFile)
	
	// Проверяем наличие файла ключа
	if cfg.YDB.ServiceAccountKeyFile == "" {
		log.Fatal("Service account key file not specified in config")
	}
	
	// Создаем YDB клиент с аутентификацией
	ydbClient, err := storage.NewYDBWorkingClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to create YDB client: %v", err)
	}
	defer ydbClient.Close()
	
	fmt.Println("YDB client created successfully with authentication!")
	
	// Создаем тестовый коэффициент
	testOdd := &models.Odd{
		MatchID:   "test_match_ydb",
		Bookmaker: "test_bookmaker",
		Market:    "1x2",
		Outcomes: map[string]float64{
			"win_home": 1.5,
			"draw":     3.0,
			"win_away": 2.5,
		},
		UpdatedAt: time.Now(),
		MatchName: "Test Match YDB",
		MatchTime: time.Now().Add(2 * time.Hour),
		Sport:     "football",
	}

	// Сохраняем в YDB
	ctx := context.Background()
	if err := ydbClient.StoreOdd(ctx, testOdd); err != nil {
		log.Fatalf("Failed to store odd: %v", err)
	}

	fmt.Println("Successfully stored odd in YDB!")

	// Получаем все матчи
	matches, err := ydbClient.GetAllMatches(ctx)
	if err != nil {
		log.Fatalf("Failed to get matches: %v", err)
	}

	fmt.Printf("Found %d matches: %v\n", len(matches), matches)

	// Получаем коэффициенты для тестового матча
	odds, err := ydbClient.GetOddsByMatch(ctx, "test_match_ydb")
	if err != nil {
		log.Fatalf("Failed to get odds: %v", err)
	}

	fmt.Printf("Found %d odds for test_match_ydb\n", len(odds))
	for _, odd := range odds {
		fmt.Printf("  %s: %+v\n", odd.Market, odd.Outcomes)
	}
	
	fmt.Println("=== YDB Test Complete ===")
}
