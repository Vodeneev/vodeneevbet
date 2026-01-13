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
	var (
		configPath  = flag.String("config", "configs/local.yaml", "Path to config file")
		action      = flag.String("action", "status", "Action: status, setup, disable")
		expireAfter = flag.Duration("expire-after", 4*time.Hour, "TTL expire after duration")
	)
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to create YDB client: %v", err)
	}
	defer ydbClient.Close()

	ctx := context.Background()

	switch *action {
	case "status":
		showTTLStatus(ctx, ydbClient)
	case "setup":
		setupTTL(ctx, ydbClient, *expireAfter)
	case "disable":
		disableTTL(ctx, ydbClient)
	default:
		log.Fatalf("Unknown action: %s. Use: status, setup, disable", *action)
	}
}

func showTTLStatus(ctx context.Context, client *storage.YDBClient) {
	fmt.Println("=== YDB TTL Status ===")

	ttlSettings, err := client.GetTTLSettings(ctx)
	if err != nil {
		log.Printf("Failed to get TTL settings: %v", err)
		return
	}

	if ttlSettings == nil {
		fmt.Println("TTL is not configured")
		return
	}

	fmt.Println("TTL is configured")
	fmt.Printf("Settings: %+v\n", ttlSettings)
}

func setupTTL(ctx context.Context, client *storage.YDBClient, expireAfter time.Duration) {
	fmt.Printf("Setting up TTL with expire after %v...\n", expireAfter)

	if err := client.SetupTTL(ctx, expireAfter); err != nil {
		log.Fatalf("Failed to setup TTL: %v", err)
	}

	fmt.Println("TTL configured successfully!")
}

func disableTTL(ctx context.Context, client *storage.YDBClient) {
	fmt.Println("Disabling TTL...")

	if err := client.DisableTTL(ctx); err != nil {
		log.Fatalf("Failed to disable TTL: %v", err)
	}

	fmt.Println("TTL disabled successfully!")
}

