package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/export"
)

func main() {
	// Read the hierarchical export
	data, err := ioutil.ReadFile("exports/hierarchical_export_2025-09-28.json")
	if err != nil {
		fmt.Printf("Error reading hierarchical export: %v\n", err)
		return
	}

	// Parse the hierarchical export
	var hierarchicalExport export.HierarchicalExport
	if err := json.Unmarshal(data, &hierarchicalExport); err != nil {
		fmt.Printf("Error parsing hierarchical export: %v\n", err)
		return
	}

	// Show the structure
	fmt.Println("🏆 HIERARCHICAL EXPORT STRUCTURE")
	fmt.Println("==================================================")
	
	fmt.Printf("📊 Total Matches: %d\n", hierarchicalExport.TotalMatches)
	fmt.Printf("⏰ Timestamp: %s\n", hierarchicalExport.Timestamp)
	
	// Show first few matches with their events
	for i, match := range hierarchicalExport.Matches {
		if i >= 3 { // Show only first 3 matches
			break
		}
		
		fmt.Printf("\n🏈 MATCH %d: %s\n", i+1, match.Name)
		fmt.Printf("   Teams: %s vs %s\n", match.HomeTeam, match.AwayTeam)
		fmt.Printf("   Start: %s\n", match.StartTime.Format("2006-01-02 15:04"))
		fmt.Printf("   Events: %d\n", len(match.Events))
		
		for j, event := range match.Events {
			fmt.Printf("   📋 Event %d: %s (%s)\n", j+1, event.MarketName, event.EventType)
			fmt.Printf("      Outcomes: %d\n", len(event.Outcomes))
			
			// Show first few outcomes
			for k, outcome := range event.Outcomes {
				if k >= 3 { // Show only first 3 outcomes
					if len(event.Outcomes) > 3 {
						fmt.Printf("      ... and %d more outcomes\n", len(event.Outcomes)-3)
					}
					break
				}
				fmt.Printf("      🎯 %s: %.2f\n", outcome.OutcomeType, outcome.Odds)
			}
		}
	}
	
	// Show statistics
	fmt.Printf("\n📈 STATISTICS\n")
	fmt.Println("------------------------------")
	
	totalEvents := 0
	totalOutcomes := 0
	eventTypeCount := make(map[string]int)
	
	for _, match := range hierarchicalExport.Matches {
		totalEvents += len(match.Events)
		for _, event := range match.Events {
			totalOutcomes += len(event.Outcomes)
			eventTypeCount[event.EventType]++
		}
	}
	
	fmt.Printf("Total Events: %d\n", totalEvents)
	fmt.Printf("Total Outcomes: %d\n", totalOutcomes)
	
	fmt.Printf("\nEvent Types Distribution:\n")
	for eventType, count := range eventTypeCount {
		fmt.Printf("  %s: %d\n", eventType, count)
	}
	
	// Show comparison with legacy format
	fmt.Printf("\n🔄 COMPARISON WITH LEGACY FORMAT\n")
	fmt.Println("----------------------------------------")
	fmt.Println("✅ Hierarchical: Structured by match → events → outcomes")
	fmt.Println("❌ Legacy: Flat list of all odds")
	fmt.Println("📊 Better organization for analysis and visualization")
}
