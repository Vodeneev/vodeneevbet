// clean-db truncates calculator PostgreSQL tables to free space.
// Usage: set POSTGRES_DSN (same as for calculator), then run:
//
//	go run ./cmd/clean-db
//	# or
//	POSTGRES_DSN='host=... port=5432 user=... password=... dbname=... sslmode=require' ./clean-db
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		log.Fatal("POSTGRES_DSN environment variable is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatalf("Failed to begin transaction: %v", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	tables := []string{"diff_bets", "odds_snapshots", "odds_snapshot_history"}
	for _, table := range tables {
		_, err = tx.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY", table))
		if err != nil {
			log.Printf("Warning: truncate %s: %v (table may not exist)", table, err)
			// Continue with other tables
			continue
		}
		log.Printf("Truncated %s", table)
	}

	if err = tx.Commit(); err != nil {
		log.Fatalf("Failed to commit: %v", err)
	}

	log.Println("Done. Calculator tables cleared.")
}
