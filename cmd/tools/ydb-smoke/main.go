package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

func main() {
	fmt.Println("🧪 Testing YDB Connection...")

	var configPath string
	flag.StringVar(&configPath, "config", "configs/local.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to create YDB client: %v", err)
	}
	defer ydbClient.Close()

	ctx := context.Background()

	fmt.Println("\n📝 Test 1: Storing test match...")
	testMatch := &models.Match{
		ID:         "test_match_001",
		Name:       "Test Team A vs Test Team B",
		HomeTeam:   "Test Team A",
		AwayTeam:   "Test Team B",
		StartTime:  time.Now().Add(2 * time.Hour),
		Sport:      "football",
		Tournament: "Test Tournament",
		Bookmaker:  "Fonbet",
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

	fmt.Println("\n🏆 Test 3: Getting all matches...")
	matches, err := ydbClient.GetAllMatches(ctx)
	if err != nil {
		log.Fatalf("Failed to get matches: %v", err)
	}
	fmt.Printf("✅ Retrieved %d matches\n", len(matches))
	for _, m := range matches {
		fmt.Printf("   Match: %s vs %s (%s)\n", m.HomeTeam, m.AwayTeam, m.Sport)
	}

	fmt.Println("\n🎉 All tests passed! YDB connection is working correctly.")
}

