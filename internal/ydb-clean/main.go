package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

func main() {
	fmt.Println("🧹 YDB Data Cleaner")
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

	// Clean all tables in correct order (due to foreign key constraints)
	tables := []string{"outcomes", "events", "matches"}

	fmt.Printf("🗑️  Cleaning %d tables...\n", len(tables))

	for i, tableName := range tables {
		fmt.Printf("⏳ Cleaning table %d/%d: %s\n", i+1, len(tables), tableName)

		startTime := time.Now()
		err := ydbClient.CleanTable(ctx, tableName)
		if err != nil {
			log.Printf("❌ Failed to clean table %s: %v", tableName, err)
			continue
		}

		duration := time.Since(startTime)
		fmt.Printf("✅ Table %s cleaned in %v\n", tableName, duration)
	}

	fmt.Println("🎉 All tables cleaned successfully!")
	fmt.Println("YDB is now empty and ready for fresh data.")
}

