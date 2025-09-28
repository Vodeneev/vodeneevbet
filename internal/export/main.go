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
	
	// Parse command line flags
	var configPath string
	flag.StringVar(&configPath, "config", "configs/local.yaml", "Path to config file")
	flag.Parse()
	
	// Load config
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	// Connect to YDB
	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()
	
	// Get all data from YDB with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	
	fmt.Println("📥 Fetching matches from YDB...")
	fmt.Println("⏳ Limiting to first 100 matches to avoid timeout...")
	
	// Get matches with limit to avoid timeout
	hierarchicalMatches, err := ydbClient.GetMatchesWithLimit(ctx, 5)
	if err != nil {
		log.Fatalf("Failed to get matches from YDB: %v", err)
	}
	
	fmt.Printf("📊 Found %d matches in YDB (limited to 100)\n", len(hierarchicalMatches))
	
	// Create exports directory first
	if err := os.MkdirAll("exports", 0755); err != nil {
		log.Fatalf("Failed to create exports directory: %v", err)
	}
	
	if len(hierarchicalMatches) == 0 {
		fmt.Println("⚠️  No matches found in YDB")
		fmt.Println("💡 This means the parser hasn't been updated to use storage yet")
		fmt.Println("💡 Run the parser first to populate data")
		
		// Create empty info file
		infoFile := "exports/no_data_info.txt"
		infoContent := "No data found in YDB.\nRun the parser first to populate data."
		if err := os.WriteFile(infoFile, []byte(infoContent), 0644); err != nil {
			log.Printf("Warning: failed to create info file: %v", err)
		}
		
		fmt.Println("📁 Created empty exports directory with info file")
		return
	}
	
	// Generate filename with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	jsonFile := fmt.Sprintf("exports/export_%s.json", timestamp)
	csvFile := fmt.Sprintf("exports/export_%s.csv", timestamp)
	
	// Export to JSON using exporter
	fmt.Printf("💾 Exporting to JSON: %s\n", jsonFile)
	exporter := export.NewExporter()
	jsonData, err := exporter.ExportToJSON(hierarchicalMatches)
	if err != nil {
		log.Fatalf("Failed to export JSON: %v", err)
	}
	
	if err := os.WriteFile(jsonFile, jsonData, 0644); err != nil {
		log.Fatalf("Failed to write JSON file: %v", err)
	}
	
	// Export to CSV (simplified format)
	fmt.Printf("💾 Exporting to CSV: %s\n", csvFile)
	if err := exportToCSV(hierarchicalMatches, csvFile); err != nil {
		log.Fatalf("Failed to export CSV: %v", err)
	}
	
	// Print summary
	fmt.Println("\n✅ Export completed successfully!")
	fmt.Printf("📊 Total matches exported: %d\n", len(hierarchicalMatches))
	
	totalEvents := 0
	totalOutcomes := 0
	for _, match := range hierarchicalMatches {
		totalEvents += len(match.Events)
		for _, event := range match.Events {
			totalOutcomes += len(event.Outcomes)
		}
	}
	
	fmt.Printf("📋 Total events: %d\n", totalEvents)
	fmt.Printf("🎯 Total outcomes: %d\n", totalOutcomes)
	fmt.Printf("📁 Files created:\n")
	fmt.Printf("   - %s\n", jsonFile)
	fmt.Printf("   - %s\n", csvFile)
}



func exportToCSV(matches []models.Match, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	
	// Write CSV header
	fmt.Fprintf(file, "Match ID,Match Name,Home Team,Away Team,Start Time,Sport,Event Type,Market Name,Outcome Type,Parameter,Odds,Bookmaker,Updated At\n")
	
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

