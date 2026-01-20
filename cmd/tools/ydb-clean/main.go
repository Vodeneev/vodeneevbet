package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

func main() {
	fmt.Println("🧹 YDB Data Cleaner")
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

	tables := []string{"outcomes", "events", "matches"}

	fmt.Printf("🗑️  Cleaning %d tables...\n", len(tables))
	for i, tableName := range tables {
		fmt.Printf("⏳ Cleaning table %d/%d: %s\n", i+1, len(tables), tableName)

		startTime := time.Now()
		if err := ydbClient.CleanTable(ctx, tableName); err != nil {
			log.Printf("❌ Failed to clean table %s: %v", tableName, err)
			continue
		}

		fmt.Printf("✅ Table %s cleaned in %v\n", tableName, time.Since(startTime))
	}

	fmt.Println("🎉 All tables cleaned successfully!")
}

