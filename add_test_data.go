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
	fmt.Println("🧪 Adding test data to YDB...")
	
	// Load config
	cfg, err := config.Load("configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	// Create YDB client
	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()
	
	ctx := context.Background()
	
	// Test data
	testOdds := []*models.Odd{
		{
			MatchID:   "match_1",
			Bookmaker: "Fonbet",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.85,
				"draw": 3.20,
				"away": 4.10,
			},
			UpdatedAt: time.Now(),
			MatchName: "Real Madrid vs Barcelona",
			MatchTime: time.Now().Add(2 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_1",
			Bookmaker: "Bet365",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.90,
				"draw": 3.10,
				"away": 4.00,
			},
			UpdatedAt: time.Now(),
			MatchName: "Real Madrid vs Barcelona",
			MatchTime: time.Now().Add(2 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_2",
			Bookmaker: "Fonbet",
			Market:    "Total",
			Outcomes: map[string]float64{
				"over_2.5": 1.75,
				"under_2.5": 2.10,
			},
			UpdatedAt: time.Now(),
			MatchName: "Manchester United vs Liverpool",
			MatchTime: time.Now().Add(4 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_3",
			Bookmaker: "Fonbet",
			Market:    "Handicap",
			Outcomes: map[string]float64{
				"home_-1": 2.50,
				"away_+1": 1.50,
			},
			UpdatedAt: time.Now(),
			MatchName: "Bayern Munich vs Borussia Dortmund",
			MatchTime: time.Now().Add(6 * time.Hour),
			Sport:     "football",
		},
	}
	
	// Store test data
	for i, odd := range testOdds {
		fmt.Printf("📝 Storing odd %d/%d: %s vs %s (%s)\n", 
			i+1, len(testOdds), odd.MatchName, odd.Bookmaker, odd.Market)
		
		err := ydbClient.StoreOdd(ctx, odd)
		if err != nil {
			log.Fatalf("Failed to store odd %d: %v", i+1, err)
		}
	}
	
	fmt.Println("✅ Test data added successfully!")
	fmt.Printf("📊 Added %d odds to YDB\n", len(testOdds))
}
