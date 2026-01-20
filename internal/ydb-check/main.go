package main

import (
	"context"
	"fmt"
	"log"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

func main() {
	fmt.Println("🔍 YDB Data Checker")
	fmt.Println("==================")

	// Load config
	cfg, err := config.Load("configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Fix key path for current directory
	cfg.YDB.ServiceAccountKeyFile = "keys/service-account-key.json"

	// Create YDB client
	fmt.Println("Connecting to YDB...")
	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to create YDB client: %v", err)
	}
	defer ydbClient.Close()

	ctx := context.Background()

	// Check each table
	tables := []string{"matches", "events", "outcomes"}

	fmt.Println("📊 Checking YDB tables...")

	for _, tableName := range tables {
		fmt.Printf("\n🔍 Checking table: %s\n", tableName)

		count, err := getTableCount(ctx, ydbClient, tableName)
		if err != nil {
			fmt.Printf("❌ Error checking table %s: %v\n", tableName, err)
			continue
		}

		fmt.Printf("📈 Table %s contains %d records\n", tableName, count)

		if count > 0 {
			// Show sample data
			fmt.Printf("📋 Sample data from %s:\n", tableName)
			err := showSampleData(ctx, ydbClient, tableName)
			if err != nil {
				fmt.Printf("⚠️  Could not show sample data: %v\n", err)
			}
		}
	}

	fmt.Println("\n✅ YDB data check completed!")
}

func getTableCount(ctx context.Context, client *storage.YDBClient, tableName string) (int, error) {
	// This is a simplified version - in real implementation you would use YDB SDK
	// For now, we'll use the existing GetMatchesWithLimit method as a proxy

	if tableName == "matches" {
		matches, err := client.GetMatchesWithLimit(ctx, 1000) // Large limit to get all
		if err != nil {
			return 0, err
		}
		return len(matches), nil
	}

	// For events and outcomes, we would need to implement specific methods
	// For now, return 0 as we don't have direct count methods
	return 0, nil
}

func showSampleData(ctx context.Context, client *storage.YDBClient, tableName string) error {
	if tableName == "matches" {
		matches, err := client.GetMatchesWithLimit(ctx, 3) // Show first 3 matches
		if err != nil {
			return err
		}

		for i, match := range matches {
			fmt.Printf("  %d. Match ID: %s, Name: %s, Teams: %s vs %s\n",
				i+1, match.ID, match.Name, match.HomeTeam, match.AwayTeam)
			fmt.Printf("     Events: %d, Created: %s\n",
				len(match.Events), match.CreatedAt.Format("2006-01-02 15:04:05"))
		}
	}

	return nil
}

