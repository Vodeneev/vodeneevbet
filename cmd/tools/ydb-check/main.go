package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

func main() {
	fmt.Println("🔍 YDB Data Checker")
	fmt.Println("==================")

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

		if count > 0 && tableName == "matches" {
			fmt.Printf("📋 Sample data from %s:\n", tableName)
			if err := showSampleData(ctx, ydbClient); err != nil {
				fmt.Printf("⚠️  Could not show sample data: %v\n", err)
			}
		}
	}

	fmt.Println("\n✅ YDB data check completed!")
}

func getTableCount(ctx context.Context, client *storage.YDBClient, tableName string) (int, error) {
	if tableName == "matches" {
		matches, err := client.GetMatchesWithLimit(ctx, 1000)
		if err != nil {
			return 0, err
		}
		return len(matches), nil
	}

	// TODO: implement direct count methods if needed
	return 0, nil
}

func showSampleData(ctx context.Context, client *storage.YDBClient) error {
	matches, err := client.GetMatchesWithLimit(ctx, 3)
	if err != nil {
		return err
	}

	for i, match := range matches {
		fmt.Printf("  %d. Match ID: %s, Name: %s, Teams: %s vs %s\n",
			i+1, match.ID, match.Name, match.HomeTeam, match.AwayTeam)
		fmt.Printf("     Events: %d, Created: %s\n",
			len(match.Events), match.CreatedAt.Format("2006-01-02 15:04:05"))
	}

	return nil
}

