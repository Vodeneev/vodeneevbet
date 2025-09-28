package main

import (
	"fmt"
	"io/ioutil"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/export"
)

func main() {
	// Read the existing legacy export
	legacyData, err := ioutil.ReadFile("exports/odds_export_2025-09-28_11-35-23.json")
	if err != nil {
		fmt.Printf("Error reading legacy file: %v\n", err)
		return
	}

	// Create legacy converter
	converter := export.NewLegacyConverter()

	// Convert to hierarchical format
	hierarchicalData, err := converter.ConvertAndExport(legacyData)
	if err != nil {
		fmt.Printf("Error converting legacy data: %v\n", err)
		return
	}

	// Save new format
	err = ioutil.WriteFile("exports/hierarchical_export_2025-09-28.json", hierarchicalData, 0644)
	if err != nil {
		fmt.Printf("Error saving hierarchical export: %v\n", err)
		return
	}

	fmt.Println("✅ Successfully converted legacy export to hierarchical format!")
	fmt.Printf("📁 Saved to: exports/hierarchical_export_2025-09-28.json\n")
	
	// Show summary
	fmt.Printf("\n=== Conversion Summary ===\n")
	fmt.Printf("Legacy file size: %d bytes\n", len(legacyData))
	fmt.Printf("Hierarchical file size: %d bytes\n", len(hierarchicalData))
	fmt.Printf("Compression ratio: %.2f%%\n", float64(len(hierarchicalData))/float64(len(legacyData))*100)
}

// createSampleLegacyData creates a sample legacy export for testing
func createSampleLegacyData() []byte {
	legacyData := `{
  "timestamp": "2025-09-28T11:31:39.482222812Z",
  "total_odds": 3,
  "matches": ["58414095", "58438916"],
  "odds": [
    {
      "match_id": "58414095",
      "bookmaker": "Fonbet",
      "market": "Match Result",
      "outcomes": {
        "away": 2.1,
        "draw": 3.2,
        "home": 1.5
      },
      "updated_at": "2025-09-28T09:02:15.500765Z",
      "match_name": "ЦСКА vs Спартак Москва",
      "match_time": "2025-10-05T13:30:00Z",
      "sport": "football"
    },
    {
      "match_id": "58414095",
      "bookmaker": "Fonbet",
      "market": "Corners",
      "outcomes": {
        "total_+8.5": 1.11,
        "total_-8.5": 6.5,
        "exact_9": 1.17
      },
      "updated_at": "2025-09-28T09:02:15.500765Z",
      "match_name": "Corners for event 58414095",
      "match_time": "2025-10-05T13:30:00Z",
      "sport": "football"
    },
    {
      "match_id": "58438916",
      "bookmaker": "Fonbet",
      "market": "Match Result",
      "outcomes": {
        "away": 2.1,
        "draw": 3.2,
        "home": 1.5
      },
      "updated_at": "2025-09-28T09:02:15.500765Z",
      "match_name": "Челси vs Ливерпуль",
      "match_time": "2025-10-04T16:30:00Z",
      "sport": "football"
    }
  ]
}`
	return []byte(legacyData)
}
