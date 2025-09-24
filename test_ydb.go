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

	// Создаем YDB клиент (упрощенная версия)
	ydbClient, err := storage.NewYDBSimpleClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to create YDB client: %v", err)
	}
	defer ydbClient.Close()

	// Создаем тестовый коэффициент
	testOdd := &models.Odd{
		MatchID:   "test_match_1",
		Bookmaker: "test_bookmaker",
		Market:    "1x2",
		Outcomes: map[string]float64{
			"win_home": 1.5,
			"draw":     3.0,
			"win_away": 2.5,
		},
		UpdatedAt: time.Now(),
		MatchName: "Test Match",
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
	odds, err := ydbClient.GetOddsByMatch(ctx, "test_match_1")
	if err != nil {
		log.Fatalf("Failed to get odds: %v", err)
	}

	fmt.Printf("Found %d odds for test_match_1\n", len(odds))
	for _, odd := range odds {
		fmt.Printf("  %s: %+v\n", odd.Market, odd.Outcomes)
	}
}
