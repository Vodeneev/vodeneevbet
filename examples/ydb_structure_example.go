package main

import (
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// ExampleYDBStructure demonstrates how match events will be structured in YDB
func ExampleYDBStructure() {
	// Example: Real Madrid vs Barcelona match
	match := &models.Match{
		ID:         "12345",
		Name:       "Real Madrid vs Barcelona",
		HomeTeam:   "Real Madrid",
		AwayTeam:   "Barcelona",
		StartTime:  time.Date(2024, 1, 15, 20, 0, 0, 0, time.UTC),
		Sport:      "football",
		Tournament: "La Liga",
		Bookmaker:  "Fonbet",
		Events: []models.Event{
			// Main match event
			{
				ID:         "12345_main",
				MatchID:    "12345",
				EventType:  string(models.StandardEventMainMatch),
				MarketName: "Match Result",
				Bookmaker:  "Fonbet",
				Outcomes: []models.Outcome{
					{
						ID:          "12345_main_home",
						EventID:     "12345_main",
						OutcomeType: string(models.OutcomeTypeHomeWin),
						Parameter:   "",
						Odds:        2.10,
						Bookmaker:   "Fonbet",
					},
					{
						ID:          "12345_main_draw",
						EventID:     "12345_main",
						OutcomeType: string(models.OutcomeTypeDraw),
						Parameter:   "",
						Odds:        3.20,
						Bookmaker:   "Fonbet",
					},
					{
						ID:          "12345_main_away",
						EventID:     "12345_main",
						OutcomeType: string(models.OutcomeTypeAwayWin),
						Parameter:   "",
						Odds:        3.50,
						Bookmaker:   "Fonbet",
					},
				},
			},
			// Corners event
			{
				ID:         "12345_corners",
				MatchID:    "12345",
				EventType:  string(models.StandardEventCorners),
				MarketName: "Corners",
				Bookmaker:  "Fonbet",
				Outcomes: []models.Outcome{
					{
						ID:          "12345_corners_total_8.5_over",
						EventID:     "12345_corners",
						OutcomeType: string(models.OutcomeTypeTotalOver),
						Parameter:   "8.5",
						Odds:        1.85,
						Bookmaker:   "Fonbet",
					},
					{
						ID:          "12345_corners_total_8.5_under",
						EventID:     "12345_corners",
						OutcomeType: string(models.OutcomeTypeTotalUnder),
						Parameter:   "8.5",
						Odds:        1.95,
						Bookmaker:   "Fonbet",
					},
					{
						ID:          "12345_corners_exact_9",
						EventID:     "12345_corners",
						OutcomeType: string(models.OutcomeTypeExactCount),
						Parameter:   "9",
						Odds:        8.50,
						Bookmaker:   "Fonbet",
					},
				},
			},
			// Yellow cards event
			{
				ID:         "12345_yellow_cards",
				MatchID:    "12345",
				EventType:  string(models.StandardEventYellowCards),
				MarketName: "Yellow Cards",
				Bookmaker:  "Fonbet",
				Outcomes: []models.Outcome{
					{
						ID:          "12345_yellow_cards_total_4.5_over",
						EventID:     "12345_yellow_cards",
						OutcomeType: string(models.OutcomeTypeTotalOver),
						Parameter:   "4.5",
						Odds:        1.90,
						Bookmaker:   "Fonbet",
					},
					{
						ID:          "12345_yellow_cards_total_4.5_under",
						EventID:     "12345_yellow_cards",
						OutcomeType: string(models.OutcomeTypeTotalUnder),
						Parameter:   "4.5",
						Odds:        1.90,
						Bookmaker:   "Fonbet",
					},
				},
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Print the structure
	fmt.Println("=== YDB Match Structure Example ===")
	fmt.Printf("Match: %s\n", match.Name)
	fmt.Printf("Teams: %s vs %s\n", match.HomeTeam, match.AwayTeam)
	fmt.Printf("Start Time: %s\n", match.StartTime.Format("2006-01-02 15:04"))
	fmt.Printf("Bookmaker: %s\n", match.Bookmaker)
	fmt.Printf("Total Events: %d\n", len(match.Events))
	
	for i, event := range match.Events {
		fmt.Printf("\n--- Event %d: %s ---\n", i+1, event.MarketName)
		fmt.Printf("Event Type: %s\n", event.EventType)
		fmt.Printf("Outcomes: %d\n", len(event.Outcomes))
		
		for j, outcome := range event.Outcomes {
			fmt.Printf("  %d. %s %s: %.2f\n", j+1, 
				models.GetOutcomeTypeName(models.StandardOutcomeType(outcome.OutcomeType)),
				outcome.Parameter,
				outcome.Odds)
		}
	}
}

func main() {
	ExampleYDBStructure()
}
