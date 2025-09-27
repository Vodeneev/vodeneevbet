package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

type ExportData struct {
	Timestamp   time.Time     `json:"timestamp"`
	TotalOdds   int           `json:"total_odds"`
	Matches     []string      `json:"matches"`
	Odds        []*models.Odd `json:"odds"`
}

func main() {
	fmt.Println("📊 Starting data export from YDB...")
	
	// Load config
	cfg, err := config.Load("../../configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	// Create YDB client
	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		log.Fatalf("Failed to connect to YDB: %v", err)
	}
	defer ydbClient.Close()
	
	// Get all data from YDB
	ctx := context.Background()
	
	fmt.Println("📥 Fetching all odds from YDB...")
	odds, err := ydbClient.GetAllOdds(ctx)
	if err != nil {
		log.Fatalf("Failed to get odds: %v", err)
	}
	
	fmt.Println("📥 Fetching all matches from YDB...")
	matches, err := ydbClient.GetAllMatches(ctx)
	if err != nil {
		log.Fatalf("Failed to get matches: %v", err)
	}
	
	// Create export data
	exportData := ExportData{
		Timestamp: time.Now(),
		TotalOdds: len(odds),
		Matches:   matches,
		Odds:      odds,
	}
	
	// Create exports directory
	if err := os.MkdirAll("exports", 0755); err != nil {
		log.Fatalf("Failed to create exports directory: %v", err)
	}
	
	// Generate filename with timestamp
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	jsonFile := fmt.Sprintf("exports/odds_export_%s.json", timestamp)
	csvFile := fmt.Sprintf("exports/odds_export_%s.csv", timestamp)
	
	// Export to JSON
	fmt.Printf("💾 Exporting to JSON: %s\n", jsonFile)
	if err := exportToJSON(exportData, jsonFile); err != nil {
		log.Fatalf("Failed to export JSON: %v", err)
	}
	
	// Export to CSV
	fmt.Printf("💾 Exporting to CSV: %s\n", csvFile)
	if err := exportToCSV(odds, csvFile); err != nil {
		log.Fatalf("Failed to export CSV: %v", err)
	}
	
	// Print summary
	fmt.Println("\n✅ Export completed successfully!")
	fmt.Printf("📊 Total odds exported: %d\n", len(odds))
	fmt.Printf("⚽ Total matches: %d\n", len(matches))
	fmt.Printf("📁 Files created:\n")
	fmt.Printf("   - %s\n", jsonFile)
	fmt.Printf("   - %s\n", csvFile)
}

func exportToJSON(data ExportData, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func exportToCSV(odds []*models.Odd, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	
	// Write CSV header
	fmt.Fprintf(file, "Match ID,Bookmaker,Market,Match Name,Match Time,Sport,Outcomes,Updated At\n")
	
	for _, odd := range odds {
		// Convert outcomes map to string
		outcomesStr := ""
		for outcome, value := range odd.Outcomes {
			if outcomesStr != "" {
				outcomesStr += "; "
			}
			outcomesStr += fmt.Sprintf("%s:%.2f", outcome, value)
		}
		
		fmt.Fprintf(file, "%s,%s,%s,\"%s\",%s,%s,\"%s\",%s\n",
			odd.MatchID,
			odd.Bookmaker,
			odd.Market,
			odd.MatchName,
			odd.MatchTime.Format("2006-01-02 15:04:05"),
			odd.Sport,
			outcomesStr,
			odd.UpdatedAt.Format("2006-01-02 15:04:05"),
		)
	}
	
	return nil
}
