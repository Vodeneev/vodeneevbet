package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/export"
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
	fmt.Println("📊 Starting hierarchical data export from YDB...")
	
	// Load config
	cfg, err := config.Load("../../configs/local.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	
	// Connect to YDB
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
		log.Fatalf("Failed to get odds from YDB: %v", err)
	}
	
	fmt.Println("📥 Fetching all matches from YDB...")
	matches, err := ydbClient.GetAllMatches(ctx)
	if err != nil {
		log.Fatalf("Failed to get matches from YDB: %v", err)
	}
	
	// Convert to hierarchical format
	fmt.Println("🔄 Converting to hierarchical format...")
	hierarchicalMatches, err := convertToHierarchicalFormat(odds, matches)
	if err != nil {
		log.Fatalf("Failed to convert to hierarchical format: %v", err)
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
	jsonData, err := hierarchicalExporter.ExportToJSON(hierarchicalMatches)
	if err != nil {
		log.Fatalf("Failed to export hierarchical JSON: %v", err)
	}
	
	if err := os.WriteFile(jsonFile, jsonData, 0644); err != nil {
		log.Fatalf("Failed to write JSON file: %v", err)
	}
	
	// Export to CSV (simplified for hierarchical format)
	fmt.Printf("💾 Exporting to CSV: %s\n", csvFile)
	if err := exportToHierarchicalCSV(hierarchicalMatches, csvFile); err != nil {
		log.Fatalf("Failed to export CSV: %v", err)
	}
	
	// Print summary
	fmt.Println("\n✅ Hierarchical export completed successfully!")
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

// convertToHierarchicalFormat converts flat odds data to hierarchical match structure
func convertToHierarchicalFormat(odds []*models.Odd, matchIDs []string) ([]models.Match, error) {
	// Group odds by match ID
	matchMap := make(map[string]*models.Match)
	
	for _, odd := range odds {
		matchID := odd.MatchID
		
		// Create match if it doesn't exist
		if matchMap[matchID] == nil {
			matchMap[matchID] = &models.Match{
				ID:         matchID,
				Name:       odd.MatchName,
				HomeTeam:   extractHomeTeam(odd.MatchName),
				AwayTeam:   extractAwayTeam(odd.MatchName),
				StartTime:  odd.MatchTime,
				Sport:      odd.Sport,
				Tournament: "Unknown Tournament", // Could be enhanced to get from YDB
				Bookmaker:  odd.Bookmaker,
				Events:    []models.Event{},
				CreatedAt:  odd.UpdatedAt,
				UpdatedAt:  odd.UpdatedAt,
			}
		}
		
		// Create event for this market
		event := models.Event{
			ID:         fmt.Sprintf("%s_%s", matchID, getEventTypeFromMarket(odd.Market)),
			EventType:  getEventTypeFromMarket(odd.Market),
			MarketName: odd.Market,
			Bookmaker:  odd.Bookmaker,
			Outcomes:   []models.Outcome{},
			CreatedAt:  odd.UpdatedAt,
			UpdatedAt:  odd.UpdatedAt,
		}
		
		// Convert outcomes
		for outcomeType, oddsValue := range odd.Outcomes {
			outcome := models.Outcome{
				ID:          fmt.Sprintf("%s_%s_%s", event.ID, outcomeType, getParameterFromOutcome(outcomeType)),
				OutcomeType: getStandardOutcomeType(outcomeType),
				Parameter:   getParameterFromOutcome(outcomeType),
				Odds:        oddsValue,
				Bookmaker:   odd.Bookmaker,
				CreatedAt:   odd.UpdatedAt,
				UpdatedAt:   odd.UpdatedAt,
			}
			event.Outcomes = append(event.Outcomes, outcome)
		}
		
		// Add event to match
		matchMap[matchID].Events = append(matchMap[matchID].Events, event)
	}
	
	// Convert map to slice
	var matches []models.Match
	for _, match := range matchMap {
		matches = append(matches, *match)
	}
	
	return matches, nil
}

// Helper functions for conversion
func extractHomeTeam(matchName string) string {
	// Simple extraction - could be enhanced
	parts := splitMatchName(matchName)
	if len(parts) >= 1 {
		return parts[0]
	}
	return "Unknown Home"
}

func extractAwayTeam(matchName string) string {
	// Simple extraction - could be enhanced
	parts := splitMatchName(matchName)
	if len(parts) >= 2 {
		return parts[1]
	}
	return "Unknown Away"
}

func splitMatchName(matchName string) []string {
	// Split by " vs " or " - " or similar patterns
	// This is a simplified version - could be enhanced
	if matchName == "" {
		return []string{"Unknown Home", "Unknown Away"}
	}
	
	// Try different separators
	separators := []string{" vs ", " - ", " v "}
	for _, sep := range separators {
		if parts := strings.Split(matchName, sep); len(parts) == 2 {
			return parts
		}
	}
	
	// Fallback
	return []string{matchName, "Unknown Away"}
}

func getEventTypeFromMarket(market string) string {
	switch market {
	case "Match Result":
		return "main_match"
	case "Corners":
		return "corners"
	case "Yellow Cards":
		return "yellow_cards"
	case "Fouls":
		return "fouls"
	case "Shots on Target":
		return "shots_on_target"
	case "Offsides":
		return "offsides"
	case "Throw-ins":
		return "throw_ins"
	default:
		return "unknown"
	}
}

func getStandardOutcomeType(outcome string) string {
	// Map common outcome types to standard types
	switch {
	case strings.Contains(outcome, "home"):
		return "home_win"
	case strings.Contains(outcome, "away"):
		return "away_win"
	case strings.Contains(outcome, "draw"):
		return "draw"
	case strings.Contains(outcome, "total_+"):
		return "total_over"
	case strings.Contains(outcome, "total_-"):
		return "total_under"
	case strings.Contains(outcome, "exact_"):
		return "exact_count"
	default:
		return outcome
	}
}

func getParameterFromOutcome(outcome string) string {
	// Extract parameter from outcome string
	// This is simplified - could be enhanced
	if strings.Contains(outcome, "total_") {
		parts := strings.Split(outcome, "_")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	if strings.Contains(outcome, "exact_") {
		parts := strings.Split(outcome, "_")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	return ""
}

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

