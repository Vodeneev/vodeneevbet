package main

import (
	"context"
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
	fmt.Println("📊 Starting hierarchical data export from YDB...")
	
	// Load config
	cfg, err := config.Load("../../configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	// Connect to YDB using new hierarchical client
	ydbClient, err := storage.NewHierarchicalYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()
	
	// Get all hierarchical matches from YDB
	ctx := context.Background()
	
	fmt.Println("📥 Fetching all hierarchical matches from YDB...")
	matches, err := ydbClient.GetAllMatches(ctx)
	if err != nil {
		log.Fatalf("Failed to get matches from YDB: %v", err)
	}
	
	if len(matches) == 0 {
		fmt.Println("⚠️  No hierarchical matches found in YDB")
		fmt.Println("💡 This means the parser hasn't been updated to use hierarchical storage yet")
		fmt.Println("💡 Run the parser first to populate hierarchical data")
		return
	}
	
	// Create exports directory
	if err := os.MkdirAll("exports", 0755); err != nil {
		log.Fatalf("Failed to create exports directory: %v", err)
	}
	
	// Generate filename with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	jsonFile := fmt.Sprintf("exports/hierarchical_export_%s.json", timestamp)
	csvFile := fmt.Sprintf("exports/hierarchical_export_%s.csv", timestamp)
	
	// Export to JSON using hierarchical exporter
	fmt.Printf("💾 Exporting to hierarchical JSON: %s\n", jsonFile)
	hierarchicalExporter := export.NewHierarchicalExporter()
	jsonData, err := hierarchicalExporter.ExportToJSON(matches)
	if err != nil {
		log.Fatalf("Failed to export hierarchical JSON: %v", err)
	}
	
	if err := os.WriteFile(jsonFile, jsonData, 0644); err != nil {
		log.Fatalf("Failed to write JSON file: %v", err)
	}
	
	// Export to CSV (simplified for hierarchical format)
	fmt.Printf("💾 Exporting to CSV: %s\n", csvFile)
	if err := exportToHierarchicalCSV(matches, csvFile); err != nil {
		log.Fatalf("Failed to export CSV: %v", err)
	}
	
	// Print summary
	fmt.Println("\n✅ Hierarchical export completed successfully!")
	fmt.Printf("📊 Total matches exported: %d\n", len(matches))
	
	totalEvents := 0
	totalOutcomes := 0
	eventTypeCount := make(map[string]int)
	
	for _, match := range matches {
		totalEvents += len(match.Events)
		for _, event := range match.Events {
			totalOutcomes += len(event.Outcomes)
			eventTypeCount[string(event.EventType)]++
		}
	}
	
	fmt.Printf("📋 Total events: %d\n", totalEvents)
	fmt.Printf("🎯 Total outcomes: %d\n", totalOutcomes)
	
	fmt.Printf("\n📊 Event Types Distribution:\n")
	for eventType, count := range eventTypeCount {
		fmt.Printf("  %s: %d\n", eventType, count)
	}
	
	fmt.Printf("📁 Files created:\n")
	fmt.Printf("   - %s\n", jsonFile)
	fmt.Printf("   - %s\n", csvFile)
}

// exportToHierarchicalCSV exports matches to CSV format
func exportToHierarchicalCSV(matches []models.Match, filename string) error {
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
