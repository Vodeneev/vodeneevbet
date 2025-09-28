package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
)

func main() {
	fmt.Println("🔄 COMPARING EXPORT FORMATS")
	fmt.Println("==================================================")
	
	// Read old format
	fmt.Println("\n📊 OLD FORMAT (Flat Structure):")
	fmt.Println("----------------------------------------")
	
	oldFiles := []string{
		"exports/odds_export_2025-09-28_11-35-23.json",
		"exports/odds_export_2025-09-28_11-57-41.json",
	}
	
	var oldData map[string]interface{}
	for _, file := range oldFiles {
		if _, err := os.Stat(file); err == nil {
			data, err := ioutil.ReadFile(file)
			if err != nil {
				fmt.Printf("Error reading %s: %v\n", file, err)
				continue
			}
			
			if err := json.Unmarshal(data, &oldData); err != nil {
				fmt.Printf("Error parsing %s: %v\n", file, err)
				continue
			}
			
			fmt.Printf("📁 File: %s\n", file)
			fmt.Printf("   Total Odds: %v\n", oldData["total_odds"])
			fmt.Printf("   Total Matches: %v\n", len(oldData["matches"].([]interface{})))
			
			// Show structure
			if odds, ok := oldData["odds"].([]interface{}); ok && len(odds) > 0 {
				fmt.Printf("   Sample Odd Structure:\n")
				if odd, ok := odds[0].(map[string]interface{}); ok {
					fmt.Printf("     Match ID: %v\n", odd["match_id"])
					fmt.Printf("     Market: %v\n", odd["market"])
					fmt.Printf("     Outcomes: %v\n", odd["outcomes"])
				}
			}
			break
		}
	}
	
	// Read new format
	fmt.Println("\n📊 NEW FORMAT (Hierarchical Structure):")
	fmt.Println("----------------------------------------")
	
	newFiles := []string{
		"exports/hierarchical_export_2025-09-28.json",
		"exports/hierarchical_export_2025-09-28_12-09-42.json",
	}
	
	var newData map[string]interface{}
	for _, file := range newFiles {
		if _, err := os.Stat(file); err == nil {
			data, err := ioutil.ReadFile(file)
			if err != nil {
				fmt.Printf("Error reading %s: %v\n", file, err)
				continue
			}
			
			if err := json.Unmarshal(data, &newData); err != nil {
				fmt.Printf("Error parsing %s: %v\n", file, err)
				continue
			}
			
			fmt.Printf("📁 File: %s\n", file)
			fmt.Printf("   Total Matches: %v\n", newData["total_matches"])
			
			// Show structure
			if matches, ok := newData["matches"].([]interface{}); ok && len(matches) > 0 {
				fmt.Printf("   Sample Match Structure:\n")
				if match, ok := matches[0].(map[string]interface{}); ok {
					fmt.Printf("     Match ID: %v\n", match["id"])
					fmt.Printf("     Name: %v\n", match["name"])
					fmt.Printf("     Teams: %v vs %v\n", match["home_team"], match["away_team"])
					
					if events, ok := match["events"].([]interface{}); ok && len(events) > 0 {
						fmt.Printf("     Events: %d\n", len(events))
						if event, ok := events[0].(map[string]interface{}); ok {
							fmt.Printf("       Event Type: %v\n", event["event_type"])
							fmt.Printf("       Market: %v\n", event["market_name"])
							if outcomes, ok := event["outcomes"].([]interface{}); ok {
								fmt.Printf("       Outcomes: %d\n", len(outcomes))
							}
						}
					}
				}
			}
			break
		}
	}
	
	// Show comparison
	fmt.Println("\n🔄 FORMAT COMPARISON:")
	fmt.Println("==============================")
	
	fmt.Println("❌ OLD FORMAT (Flat):")
	fmt.Println("   Structure: [odds] -> [odd1, odd2, odd3, ...]")
	fmt.Println("   Problems:")
	fmt.Println("     - All odds in one flat list")
	fmt.Println("     - Hard to group by match")
	fmt.Println("     - Difficult to analyze by event type")
	fmt.Println("     - Poor data organization")
	
	fmt.Println("\n✅ NEW FORMAT (Hierarchical):")
	fmt.Println("   Structure: [matches] -> [match1, match2, ...]")
	fmt.Println("             [match] -> [events] -> [outcomes]")
	fmt.Println("   Benefits:")
	fmt.Println("     - Clear match grouping")
	fmt.Println("     - Easy to find all events for a match")
	fmt.Println("     - Better data organization")
	fmt.Println("     - Perfect for UI/UX")
	fmt.Println("     - Easier analysis and visualization")
	
	// Show file sizes
	fmt.Println("\n📊 FILE SIZE COMPARISON:")
	fmt.Println("------------------------------")
	
	for _, oldFile := range oldFiles {
		if stat, err := os.Stat(oldFile); err == nil {
			fmt.Printf("Old format: %s (%d bytes)\n", oldFile, stat.Size())
		}
	}
	
	for _, newFile := range newFiles {
		if stat, err := os.Stat(newFile); err == nil {
			fmt.Printf("New format: %s (%d bytes)\n", newFile, stat.Size())
		}
	}
	
	fmt.Println("\n🎯 CONCLUSION:")
	fmt.Println("The new hierarchical format provides much better data organization")
	fmt.Println("and is more suitable for modern applications and analysis!")
}
