package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

func main() {
	// Загружаем конфигурацию
	cfg, err := config.Load("configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Создаем YDB клиент
	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to create YDB client: %v", err)
	}
	defer ydbClient.Close()

	ctx := context.Background()

	// Тестируем сохранение коэффициента
	testOdd := &models.Odd{
		MatchID:   "test_match_1",
		Bookmaker: "Fonbet",
		Market:    "1x2",
		Outcomes: map[string]float64{
			"home": 1.85,
			"draw": 3.20,
			"away": 4.10,
		},
		UpdatedAt: time.Now(),
		MatchName: "Test Match",
		MatchTime: time.Now().Add(2 * time.Hour),
		Sport:     "football",
	}

	fmt.Println("Testing YDB connection...")
	fmt.Printf("Storing test odd: %+v\n", testOdd)

	// Сохраняем тестовый коэффициент
	err = ydbClient.StoreOdd(ctx, testOdd)
	if err != nil {
		log.Fatalf("Failed to store odd: %v", err)
	}

	fmt.Println("✅ Successfully stored odd in YDB")

	// Получаем все коэффициенты
	odds, err := ydbClient.GetAllOdds(ctx)
	if err != nil {
		log.Fatalf("Failed to get odds: %v", err)
	}

	fmt.Printf("✅ Retrieved %d odds from YDB\n", len(odds))
	for _, odd := range odds {
		fmt.Printf("  - Match: %s, Bookmaker: %s, Market: %s\n", 
			odd.MatchName, odd.Bookmaker, odd.Market)
	}

	// Получаем все матчи
	matches, err := ydbClient.GetAllMatches(ctx)
	if err != nil {
		log.Fatalf("Failed to get matches: %v", err)
	}

	fmt.Printf("✅ Retrieved %d matches from YDB\n", len(matches))
	for _, matchID := range matches {
		fmt.Printf("  - Match ID: %s\n", matchID)
	}

	fmt.Println("🎉 YDB connection test completed successfully!")
}
