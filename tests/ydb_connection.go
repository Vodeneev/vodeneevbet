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
	fmt.Println("🧪 Testing YDB Connection...")

	// Load configuration
	cfg, err := config.Load("../configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create YDB client
	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to create YDB client: %v", err)
	}
	defer ydbClient.Close()

	ctx := context.Background()

	// Test 1: Store betting odds
	fmt.Println("\n📝 Test 1: Storing odd...")
	testOdd := &models.Odd{
		MatchID:   "test_match_001",
		Bookmaker: "Fonbet",
		Market:    "Match Result",
		Outcomes: map[string]float64{
			"home": 1.85,
			"draw": 3.20,
			"away": 4.10,
		},
		UpdatedAt: time.Now(),
		MatchName: "Test Team A vs Test Team B",
		MatchTime: time.Now().Add(2 * time.Hour),
		Sport:     "football",
	}

	if err := ydbClient.StoreOdd(ctx, testOdd); err != nil {
		log.Fatalf("Failed to store odd: %v", err)
	}
	fmt.Println("✅ Odd stored successfully")

	// Test 2: Retrieve odds
	fmt.Println("\n📖 Test 2: Getting odds...")
	odds, err := ydbClient.GetOddsByMatch(ctx, "test_match_001")
	if err != nil {
		log.Fatalf("Failed to get odds: %v", err)
	}
	fmt.Printf("✅ Retrieved %d odds\n", len(odds))
	for _, odd := range odds {
		fmt.Printf("   %s: %+v\n", odd.Bookmaker, odd.Outcomes)
	}

	// Test 3: Get all matches
	fmt.Println("\n🏆 Test 3: Getting all matches...")
	matches, err := ydbClient.GetAllMatches(ctx)
	if err != nil {
		log.Fatalf("Failed to get matches: %v", err)
	}
	fmt.Printf("✅ Retrieved %d matches\n", len(matches))
	for _, matchID := range matches {
		fmt.Printf("   Match: %s\n", matchID)
	}

	// Test 4: Store second odd
	fmt.Println("\n📝 Test 4: Storing second odd...")
	testOdd2 := &models.Odd{
		MatchID:   "test_match_001",
		Bookmaker: "Bet365",
		Market:    "Match Result",
		Outcomes: map[string]float64{
			"home": 1.90,
			"draw": 3.10,
			"away": 3.95,
		},
		UpdatedAt: time.Now(),
		MatchName: "Test Team A vs Test Team B",
		MatchTime: time.Now().Add(2 * time.Hour),
		Sport:     "football",
	}

	if err := ydbClient.StoreOdd(ctx, testOdd2); err != nil {
		log.Fatalf("Failed to store second odd: %v", err)
	}
	fmt.Println("✅ Second odd stored successfully")

	// Test 5: Get all odds for match
	fmt.Println("\n📊 Test 5: Getting all odds for match...")
	allOdds, err := ydbClient.GetOddsByMatch(ctx, "test_match_001")
	if err != nil {
		log.Fatalf("Failed to get all odds: %v", err)
	}
	fmt.Printf("✅ Retrieved %d odds for match\n", len(allOdds))
	for _, odd := range allOdds {
		fmt.Printf("   %s: %+v\n", odd.Bookmaker, odd.Outcomes)
	}

	fmt.Println("\n🎉 All tests passed! YDB connection is working correctly.")
}
