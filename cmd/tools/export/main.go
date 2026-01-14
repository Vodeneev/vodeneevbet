package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/export"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

func main() {
	fmt.Println("📊 Starting data export from YDB...")

	var configPath string
	flag.StringVar(&configPath, "config", "configs/local.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println("📥 Fetching matches from YDB...")

	hierarchicalMatches, err := ydbClient.GetMatchesWithLimit(ctx, 5)
	if err != nil {
		log.Fatalf("Failed to get matches from YDB: %v", err)
	}

	fmt.Printf("📊 Found %d matches in YDB (limited)\n", len(hierarchicalMatches))

	if err := os.MkdirAll("exports", 0o755); err != nil {
		log.Fatalf("Failed to create exports directory: %v", err)
	}

	if len(hierarchicalMatches) == 0 {
		fmt.Println("⚠️  No matches found in YDB")
		fmt.Println("💡 Run the parser first to populate data")

		infoFile := "exports/no_data_info.txt"
		infoContent := "No data found in YDB.\nRun the parser first to populate data."
		if err := os.WriteFile(infoFile, []byte(infoContent), 0o644); err != nil {
			log.Printf("Warning: failed to create info file: %v", err)
		}

		fmt.Println("📁 Created exports directory with info file")
		return
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	jsonFile := fmt.Sprintf("exports/export_%s.json", timestamp)
	csvFile := fmt.Sprintf("exports/export_%s.csv", timestamp)

	fmt.Printf("💾 Exporting to JSON: %s\n", jsonFile)
	exporter := export.NewExporter()
	jsonData, err := exporter.ExportToJSON(hierarchicalMatches)
	if err != nil {
		log.Fatalf("Failed to export JSON: %v", err)
	}

	if err := os.WriteFile(jsonFile, jsonData, 0o644); err != nil {
		log.Fatalf("Failed to write JSON file: %v", err)
	}

	fmt.Printf("💾 Exporting to CSV: %s\n", csvFile)
	if err := exportToCSV(hierarchicalMatches, csvFile); err != nil {
		log.Fatalf("Failed to export CSV: %v", err)
	}

	fmt.Println("\n✅ Export completed successfully!")
	fmt.Printf("📊 Total matches exported: %d\n", len(hierarchicalMatches))
}

func exportToCSV(matches []models.Match, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintln(file, "Match ID,Match Name,Home Team,Away Team,Start Time,Sport,Event Type,Market Name,Outcome Type,Parameter,Odds,Bookmaker,Updated At")

	for _, match := range matches {
		for _, event := range match.Events {
			for _, outcome := range event.Outcomes {
				fmt.Fprintf(file, "%s,\"%s\",\"%s\",\"%s\",%s,%s,%s,\"%s\",%s,\"%s\",%.2f,%s,%s\n",
					match.ID,
					match.Name,
					match.HomeTeam,
					match.AwayTeam,
					match.StartTime.Format("2006-01-02 15:04:05"),
					match.Sport,
					event.EventType,
					event.MarketName,
					outcome.OutcomeType,
					outcome.Parameter,
					outcome.Odds,
					outcome.Bookmaker,
					outcome.UpdatedAt.Format("2006-01-02 15:04:05"),
				)
			}
		}
	}

	return nil
}

