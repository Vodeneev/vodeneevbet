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
	
	var odds []*models.Odd
	var matches []string
	
	// Try to connect to YDB
	ydbClient, err := storage.NewYDBClient(&cfg.YDB)
	if err != nil {
		fmt.Printf("⚠️  Failed to connect to YDB: %v\n", err)
		fmt.Println("🔄 Using mock data for export...")
		
		// Use mock data if YDB is not available
		odds, matches = getMockData()
	} else {
		defer ydbClient.Close()
		
		// Get all data from YDB
		ctx := context.Background()
		
		fmt.Println("📥 Fetching all odds from YDB...")
		odds, err = ydbClient.GetAllOdds(ctx)
		if err != nil {
			fmt.Printf("⚠️  Failed to get odds from YDB: %v\n", err)
			fmt.Println("🔄 Using mock data for export...")
			odds, matches = getMockData()
		} else {
			fmt.Println("📥 Fetching all matches from YDB...")
			matches, err = ydbClient.GetAllMatches(ctx)
			if err != nil {
				fmt.Printf("⚠️  Failed to get matches from YDB: %v\n", err)
				fmt.Println("🔄 Using mock data for export...")
				odds, matches = getMockData()
			}
		}
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

// getMockData returns mock data for testing when YDB is not available
func getMockData() ([]*models.Odd, []string) {
	mockOdds := []*models.Odd{
		// Match 1: Real Madrid vs Barcelona
		{
			MatchID:   "match_1",
			Bookmaker: "Fonbet",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.85,
				"draw": 3.20,
				"away": 4.10,
			},
			UpdatedAt: time.Now(),
			MatchName: "Real Madrid vs Barcelona",
			MatchTime: time.Now().Add(2 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_1",
			Bookmaker: "Bet365",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.90,
				"draw": 3.10,
				"away": 4.00,
			},
			UpdatedAt: time.Now(),
			MatchName: "Real Madrid vs Barcelona",
			MatchTime: time.Now().Add(2 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_1",
			Bookmaker: "Fonbet",
			Market:    "Total",
			Outcomes: map[string]float64{
				"over_2.5": 1.75,
				"under_2.5": 2.10,
			},
			UpdatedAt: time.Now(),
			MatchName: "Real Madrid vs Barcelona",
			MatchTime: time.Now().Add(2 * time.Hour),
			Sport:     "football",
		},
		// Match 2: Manchester United vs Liverpool
		{
			MatchID:   "match_2",
			Bookmaker: "Fonbet",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 2.10,
				"draw": 3.40,
				"away": 3.20,
			},
			UpdatedAt: time.Now(),
			MatchName: "Manchester United vs Liverpool",
			MatchTime: time.Now().Add(4 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_2",
			Bookmaker: "Bet365",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 2.05,
				"draw": 3.50,
				"away": 3.15,
			},
			UpdatedAt: time.Now(),
			MatchName: "Manchester United vs Liverpool",
			MatchTime: time.Now().Add(4 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_2",
			Bookmaker: "Fonbet",
			Market:    "Total",
			Outcomes: map[string]float64{
				"over_2.5": 1.65,
				"under_2.5": 2.25,
			},
			UpdatedAt: time.Now(),
			MatchName: "Manchester United vs Liverpool",
			MatchTime: time.Now().Add(4 * time.Hour),
			Sport:     "football",
		},
		// Match 3: Bayern Munich vs Borussia Dortmund
		{
			MatchID:   "match_3",
			Bookmaker: "Fonbet",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.70,
				"draw": 3.80,
				"away": 4.50,
			},
			UpdatedAt: time.Now(),
			MatchName: "Bayern Munich vs Borussia Dortmund",
			MatchTime: time.Now().Add(6 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_3",
			Bookmaker: "Bet365",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.75,
				"draw": 3.70,
				"away": 4.30,
			},
			UpdatedAt: time.Now(),
			MatchName: "Bayern Munich vs Borussia Dortmund",
			MatchTime: time.Now().Add(6 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_3",
			Bookmaker: "Fonbet",
			Market:    "Handicap",
			Outcomes: map[string]float64{
				"home_-1": 2.50,
				"away_+1": 1.50,
			},
			UpdatedAt: time.Now(),
			MatchName: "Bayern Munich vs Borussia Dortmund",
			MatchTime: time.Now().Add(6 * time.Hour),
			Sport:     "football",
		},
		// Match 4: PSG vs Marseille
		{
			MatchID:   "match_4",
			Bookmaker: "Fonbet",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.60,
				"draw": 4.00,
				"away": 5.50,
			},
			UpdatedAt: time.Now(),
			MatchName: "PSG vs Marseille",
			MatchTime: time.Now().Add(8 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_4",
			Bookmaker: "Bet365",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.65,
				"draw": 3.90,
				"away": 5.20,
			},
			UpdatedAt: time.Now(),
			MatchName: "PSG vs Marseille",
			MatchTime: time.Now().Add(8 * time.Hour),
			Sport:     "football",
		},
		// Match 5: Chelsea vs Arsenal
		{
			MatchID:   "match_5",
			Bookmaker: "Fonbet",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 2.80,
				"draw": 3.20,
				"away": 2.40,
			},
			UpdatedAt: time.Now(),
			MatchName: "Chelsea vs Arsenal",
			MatchTime: time.Now().Add(10 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_5",
			Bookmaker: "Bet365",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 2.75,
				"draw": 3.30,
				"away": 2.45,
			},
			UpdatedAt: time.Now(),
			MatchName: "Chelsea vs Arsenal",
			MatchTime: time.Now().Add(10 * time.Hour),
			Sport:     "football",
		},
	}
	
	matches := []string{"match_1", "match_2", "match_3", "match_4", "match_5"}
	
	return mockOdds, matches
}
