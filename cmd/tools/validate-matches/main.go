package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

type Match struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	HomeTeam   string    `json:"home_team"`
	AwayTeam   string    `json:"away_team"`
	StartTime  string    `json:"start_time"`
	Sport      string    `json:"sport"`
	Tournament string    `json:"tournament"`
	Bookmaker  string    `json:"bookmaker"`
	Events     []interface{} `json:"events"`
}

type MatchData struct {
	Matches []Match `json:"matches"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go <matches.json>")
	}

	data, err := ioutil.ReadFile(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to read file: %v", err)
	}

	var matchData MatchData
	if err := json.Unmarshal(data, &matchData); err != nil {
		log.Fatalf("Failed to parse JSON: %v", err)
	}

	// Group matches by canonical ID
	canonicalGroups := make(map[string][]Match)
	
	for _, match := range matchData.Matches {
		startTime, err := time.Parse(time.RFC3339, match.StartTime)
		if err != nil {
			log.Printf("Warning: failed to parse time for match %s: %v", match.ID, err)
			continue
		}

		canonicalID := models.CanonicalMatchID(match.HomeTeam, match.AwayTeam, startTime)
		canonicalGroups[canonicalID] = append(canonicalGroups[canonicalID], match)
	}

	// Find groups with multiple bookmakers (good - means merging works)
	multiBookmakerGroups := make(map[string][]Match)
	singleBookmakerCount := 0
	
	for canonicalID, matches := range canonicalGroups {
		bookmakers := make(map[string]bool)
		for _, match := range matches {
			if match.Bookmaker != "" {
				bookmakers[match.Bookmaker] = true
			}
		}
		
		if len(bookmakers) > 1 {
			multiBookmakerGroups[canonicalID] = matches
		} else {
			singleBookmakerCount++
		}
	}

	// Print statistics
	fmt.Printf("=== Validation Results ===\n\n")
	fmt.Printf("Total matches: %d\n", len(matchData.Matches))
	fmt.Printf("Unique canonical IDs: %d\n", len(canonicalGroups))
	fmt.Printf("Groups with single bookmaker: %d\n", singleBookmakerCount)
	fmt.Printf("Groups with multiple bookmakers: %d\n\n", len(multiBookmakerGroups))

	// Show examples of multi-bookmaker groups (successful merging)
	if len(multiBookmakerGroups) > 0 {
		fmt.Printf("=== Successfully Merged Matches (Multiple Bookmakers) ===\n\n")
		
		// Sort by canonical ID for consistent output
		sortedIDs := make([]string, 0, len(multiBookmakerGroups))
		for id := range multiBookmakerGroups {
			sortedIDs = append(sortedIDs, id)
		}
		sort.Strings(sortedIDs)

		// Show first 20 examples
		shown := 0
		for _, canonicalID := range sortedIDs {
			if shown >= 20 {
				break
			}
			matches := multiBookmakerGroups[canonicalID]
			
			bookmakers := make(map[string]bool)
			for _, match := range matches {
				if match.Bookmaker != "" {
					bookmakers[match.Bookmaker] = true
				}
			}
			
			fmt.Printf("Canonical ID: %s\n", canonicalID)
			for _, match := range matches {
				fmt.Printf("  - %s vs %s (%s) [%s]\n", 
					match.HomeTeam, match.AwayTeam, match.Bookmaker, match.StartTime)
			}
			fmt.Println()
			shown++
		}
		
		if len(multiBookmakerGroups) > 20 {
			fmt.Printf("... and %d more groups\n\n", len(multiBookmakerGroups)-20)
		}
	}

	// Check for potential issues: same teams, same time, different canonical IDs
	fmt.Printf("=== Potential Issues (Same Teams, Same Time, Different Canonical IDs) ===\n\n")
	
	// Group by (home_team, away_team, start_time) to find duplicates
	teamTimeGroups := make(map[string][]Match)
	for _, match := range matchData.Matches {
		key := fmt.Sprintf("%s|%s|%s", strings.ToLower(match.HomeTeam), strings.ToLower(match.AwayTeam), match.StartTime)
		teamTimeGroups[key] = append(teamTimeGroups[key], match)
	}

	issuesFound := 0
	for key, matches := range teamTimeGroups {
		if len(matches) <= 1 {
			continue
		}

		// Check if they have different canonical IDs
		canonicalIDs := make(map[string]bool)
		for _, match := range matches {
			startTime, err := time.Parse(time.RFC3339, match.StartTime)
			if err != nil {
				continue
			}
			canonicalID := models.CanonicalMatchID(match.HomeTeam, match.AwayTeam, startTime)
			canonicalIDs[canonicalID] = true
		}

		if len(canonicalIDs) > 1 {
			issuesFound++
			if issuesFound <= 10 {
				parts := strings.Split(key, "|")
				fmt.Printf("Issue #%d:\n", issuesFound)
				fmt.Printf("  Teams: %s vs %s\n", parts[0], parts[1])
				fmt.Printf("  Time: %s\n", parts[2])
				fmt.Printf("  Different canonical IDs:\n")
				for canonicalID := range canonicalIDs {
					fmt.Printf("    - %s\n", canonicalID)
				}
				fmt.Println()
			}
		}
	}

	if issuesFound == 0 {
		fmt.Printf("No issues found! All matches with same teams and time have the same canonical ID.\n\n")
	} else if issuesFound > 10 {
		fmt.Printf("... and %d more potential issues\n\n", issuesFound-10)
	}

	// Show some examples of normalized team names
	fmt.Printf("=== Team Name Normalization Examples ===\n\n")
	
	teamExamples := make(map[string]string)
	canonicalIDExamples := make(map[string]string) // canonicalID -> match description
	
	for _, match := range matchData.Matches {
		if match.HomeTeam != "" {
			startTime, err := time.Parse(time.RFC3339, match.StartTime)
			if err != nil {
				continue
			}
			canonicalID := models.CanonicalMatchID(match.HomeTeam, match.AwayTeam, startTime)
			parts := strings.Split(canonicalID, "|")
			if len(parts) >= 2 {
				teamExamples[match.HomeTeam] = parts[0]
				teamExamples[match.AwayTeam] = parts[1]
				canonicalIDExamples[canonicalID] = fmt.Sprintf("%s vs %s (%s)", 
					match.HomeTeam, match.AwayTeam, match.StartTime)
			}
		}
	}

	// Show normalization examples
	shown := 0
	fmt.Printf("Team name normalizations:\n")
	for original, normalized := range teamExamples {
		if shown >= 30 {
			break
		}
		originalLower := strings.ToLower(strings.TrimSpace(original))
		if originalLower != normalized {
			fmt.Printf("  %s -> %s\n", original, normalized)
			shown++
		}
	}
	if shown == 0 {
		fmt.Printf("  (No significant normalizations found - all names are already normalized)\n")
	}

	// Show some canonical ID examples
	fmt.Printf("\n=== Sample Canonical IDs ===\n\n")
	shown = 0
	for canonicalID, description := range canonicalIDExamples {
		if shown >= 10 {
			break
		}
		fmt.Printf("  %s\n    Match: %s\n\n", canonicalID, description)
		shown++
	}

	// Check for the specific example from conversation: Bayern vs Union Saint-Gilloise
	fmt.Printf("=== Checking Specific Examples from Conversation ===\n\n")
	for _, match := range matchData.Matches {
		homeLower := strings.ToLower(match.HomeTeam)
		awayLower := strings.ToLower(match.AwayTeam)
		
		if (strings.Contains(homeLower, "bayern") || strings.Contains(awayLower, "bayern")) &&
		   (strings.Contains(homeLower, "union") || strings.Contains(awayLower, "union")) {
			startTime, err := time.Parse(time.RFC3339, match.StartTime)
			if err == nil {
				canonicalID := models.CanonicalMatchID(match.HomeTeam, match.AwayTeam, startTime)
				parts := strings.Split(canonicalID, "|")
				fmt.Printf("Bayern vs Union Saint-Gilloise example:\n")
				fmt.Printf("  Original: %s vs %s\n", match.HomeTeam, match.AwayTeam)
				fmt.Printf("  Normalized: %s vs %s\n", parts[0], parts[1])
				fmt.Printf("  Canonical ID: %s\n", canonicalID)
				fmt.Printf("  Time: %s\n\n", match.StartTime)
			}
		}
	}
}
