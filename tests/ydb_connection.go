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

	// Test 1: Store test match
	fmt.Println("\n📝 Test 1: Storing test match...")
	testMatch := &models.Match{
		ID:        "test_match_001",
		Name:      "Test Team A vs Test Team B",
		HomeTeam:  "Test Team A",
		AwayTeam:  "Test Team B",
		StartTime: time.Now().Add(2 * time.Hour),
		Sport:     "football",
		Tournament: "Test Tournament",
		Bookmaker: "Fonbet",
		Events: []models.Event{
			{
				ID:         "event_001",
				MatchID:    "test_match_001",
				EventType:  string(models.StandardEventMainMatch),
				MarketName: "Match Result",
				Bookmaker:  "Fonbet",
				Outcomes: []models.Outcome{
					{
						ID:          "outcome_001",
						EventID:     "event_001",
						OutcomeType: string(models.OutcomeTypeHomeWin),
						Parameter:   "",
						Odds:        1.85,
						Bookmaker:   "Fonbet",
						CreatedAt:   time.Now(),
						UpdatedAt:   time.Now(),
					},
					{
						ID:          "outcome_002",
						EventID:     "event_001",
						OutcomeType: string(models.OutcomeTypeDraw),
						Parameter:   "",
						Odds:        3.20,
						Bookmaker:   "Fonbet",
						CreatedAt:   time.Now(),
						UpdatedAt:   time.Now(),
					},
					{
						ID:          "outcome_003",
						EventID:     "event_001",
						OutcomeType: string(models.OutcomeTypeAwayWin),
						Parameter:   "",
						Odds:        4.10,
						Bookmaker:   "Fonbet",
						CreatedAt:   time.Now(),
						UpdatedAt:   time.Now(),
					},
				},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := ydbClient.StoreMatch(ctx, testMatch); err != nil {
		log.Fatalf("Failed to store match: %v", err)
	}
	fmt.Println("✅ Match stored successfully")

	// Test 2: Retrieve match
	fmt.Println("\n📖 Test 2: Getting match...")
	match, err := ydbClient.GetMatch(ctx, "test_match_001")
	if err != nil {
		log.Fatalf("Failed to get match: %v", err)
	}
	fmt.Printf("✅ Retrieved match: %s vs %s\n", match.HomeTeam, match.AwayTeam)
	fmt.Printf("   Events: %d\n", len(match.Events))
	for _, event := range match.Events {
		fmt.Printf("   Event: %s (%s) - %d outcomes\n", event.EventType, event.MarketName, len(event.Outcomes))
	}

	// Test 3: Get all matches
	fmt.Println("\n🏆 Test 3: Getting all matches...")
	matches, err := ydbClient.GetAllMatches(ctx)
	if err != nil {
		log.Fatalf("Failed to get matches: %v", err)
	}
	fmt.Printf("✅ Retrieved %d matches\n", len(matches))
	for _, match := range matches {
		fmt.Printf("   Match: %s vs %s (%s)\n", match.HomeTeam, match.AwayTeam, match.Sport)
	}

	// Test 4: Store second match
	fmt.Println("\n📝 Test 4: Storing second match...")
	testMatch2 := &models.Match{
		ID:        "test_match_002",
		Name:      "Test Team C vs Test Team D",
		HomeTeam:  "Test Team C",
		AwayTeam:  "Test Team D",
		StartTime: time.Now().Add(3 * time.Hour),
		Sport:     "football",
		Tournament: "Test Tournament",
		Bookmaker: "Bet365",
		Events: []models.Event{
			{
				ID:         "event_002",
				MatchID:    "test_match_002",
				EventType:  string(models.StandardEventMainMatch),
				MarketName: "Match Result",
				Bookmaker:  "Bet365",
				Outcomes: []models.Outcome{
					{
						ID:          "outcome_004",
						EventID:     "event_002",
						OutcomeType: string(models.OutcomeTypeHomeWin),
						Parameter:   "",
						Odds:        1.90,
						Bookmaker:   "Bet365",
						CreatedAt:   time.Now(),
						UpdatedAt:   time.Now(),
					},
					{
						ID:          "outcome_005",
						EventID:     "event_002",
						OutcomeType: string(models.OutcomeTypeDraw),
						Parameter:   "",
						Odds:        3.10,
						Bookmaker:   "Bet365",
						CreatedAt:   time.Now(),
						UpdatedAt:   time.Now(),
					},
					{
						ID:          "outcome_006",
						EventID:     "event_002",
						OutcomeType: string(models.OutcomeTypeAwayWin),
						Parameter:   "",
						Odds:        3.95,
						Bookmaker:   "Bet365",
						CreatedAt:   time.Now(),
						UpdatedAt:   time.Now(),
					},
				},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := ydbClient.StoreMatch(ctx, testMatch2); err != nil {
		log.Fatalf("Failed to store second match: %v", err)
	}
	fmt.Println("✅ Second match stored successfully")

	// Test 5: Get all matches again
	fmt.Println("\n📊 Test 5: Getting all matches again...")
	allMatches, err := ydbClient.GetAllMatches(ctx)
	if err != nil {
		log.Fatalf("Failed to get all matches: %v", err)
	}
	fmt.Printf("✅ Retrieved %d matches total\n", len(allMatches))
	for _, match := range allMatches {
		fmt.Printf("   %s vs %s (%s) - %d events\n", match.HomeTeam, match.AwayTeam, match.Sport, len(match.Events))
	}

	fmt.Println("\n🎉 All tests passed! YDB connection is working correctly.")
}
