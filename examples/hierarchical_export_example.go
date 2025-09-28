package main

import (
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/export"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

func main() {
	// Create example matches with hierarchical structure
	matches := createExampleMatches()
	
	// Create hierarchical exporter
	exporter := export.NewHierarchicalExporter()
	
	// Export to JSON
	jsonData, err := exporter.ExportToJSON(matches)
	if err != nil {
		fmt.Printf("Error exporting to JSON: %v\n", err)
		return
	}
	
	// Print the JSON
	fmt.Println("=== Hierarchical Export Example ===")
	fmt.Println(string(jsonData))
	
	// Print summary
	exportData := exporter.ExportMatches(matches)
	exporter.PrintSummary(exportData)
}

func createExampleMatches() []models.Match {
	now := time.Now()
	
	// Match 1: ЦСКА vs Спартак Москва
	match1 := models.Match{
		ID:         "58414095",
		Name:       "ЦСКА vs Спартак Москва",
		HomeTeam:   "ЦСКА",
		AwayTeam:   "Спартак Москва",
		StartTime:  time.Date(2025, 10, 5, 13, 30, 0, 0, time.UTC),
		Sport:      "football",
		Tournament: "Russian Premier League",
		Bookmaker:  "Fonbet",
		Events: []models.Event{
			// Main match event
			{
				ID:         "58414095_main",
				MatchID:    "58414095",
				EventType:  string(models.StandardEventMainMatch),
				MarketName: "Match Result",
				Bookmaker:  "Fonbet",
				Outcomes: []models.Outcome{
					{
						ID:          "58414095_main_home",
						EventID:     "58414095_main",
						OutcomeType: string(models.OutcomeTypeHomeWin),
						Parameter:   "",
						Odds:        1.5,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
					{
						ID:          "58414095_main_draw",
						EventID:     "58414095_main",
						OutcomeType: string(models.OutcomeTypeDraw),
						Parameter:   "",
						Odds:        3.2,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
					{
						ID:          "58414095_main_away",
						EventID:     "58414095_main",
						OutcomeType: string(models.OutcomeTypeAwayWin),
						Parameter:   "",
						Odds:        2.1,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
			// Corners event
			{
				ID:         "58414095_corners",
				MatchID:    "58414095",
				EventType:  string(models.StandardEventCorners),
				MarketName: "Corners",
				Bookmaker:  "Fonbet",
				Outcomes: []models.Outcome{
					{
						ID:          "58414095_corners_total_8.5_over",
						EventID:     "58414095_corners",
						OutcomeType: string(models.OutcomeTypeTotalOver),
						Parameter:   "8.5",
						Odds:        1.11,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
					{
						ID:          "58414095_corners_total_8.5_under",
						EventID:     "58414095_corners",
						OutcomeType: string(models.OutcomeTypeTotalUnder),
						Parameter:   "8.5",
						Odds:        6.5,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
					{
						ID:          "58414095_corners_exact_9",
						EventID:     "58414095_corners",
						OutcomeType: string(models.OutcomeTypeExactCount),
						Parameter:   "9",
						Odds:        1.17,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	
	// Match 2: Челси vs Ливерпуль
	match2 := models.Match{
		ID:         "58438916",
		Name:       "Челси vs Ливерпуль",
		HomeTeam:   "Челси",
		AwayTeam:   "Ливерпуль",
		StartTime:  time.Date(2025, 10, 4, 16, 30, 0, 0, time.UTC),
		Sport:      "football",
		Tournament: "Premier League",
		Bookmaker:  "Fonbet",
		Events: []models.Event{
			// Main match event
			{
				ID:         "58438916_main",
				MatchID:    "58438916",
				EventType:  string(models.StandardEventMainMatch),
				MarketName: "Match Result",
				Bookmaker:  "Fonbet",
				Outcomes: []models.Outcome{
					{
						ID:          "58438916_main_home",
						EventID:     "58438916_main",
						OutcomeType: string(models.OutcomeTypeHomeWin),
						Parameter:   "",
						Odds:        1.5,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
					{
						ID:          "58438916_main_draw",
						EventID:     "58438916_main",
						OutcomeType: string(models.OutcomeTypeDraw),
						Parameter:   "",
						Odds:        3.2,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
					{
						ID:          "58438916_main_away",
						EventID:     "58438916_main",
						OutcomeType: string(models.OutcomeTypeAwayWin),
						Parameter:   "",
						Odds:        2.1,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
			// Yellow cards event
			{
				ID:         "58438916_yellow_cards",
				MatchID:    "58438916",
				EventType:  string(models.StandardEventYellowCards),
				MarketName: "Yellow Cards",
				Bookmaker:  "Fonbet",
				Outcomes: []models.Outcome{
					{
						ID:          "58438916_yellow_cards_total_4.5_over",
						EventID:     "58438916_yellow_cards",
						OutcomeType: string(models.OutcomeTypeTotalOver),
						Parameter:   "4.5",
						Odds:        1.90,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
					{
						ID:          "58438916_yellow_cards_total_4.5_under",
						EventID:     "58438916_yellow_cards",
						OutcomeType: string(models.OutcomeTypeTotalUnder),
						Parameter:   "4.5",
						Odds:        1.90,
						Bookmaker:   "Fonbet",
						CreatedAt:   now,
						UpdatedAt:   now,
					},
				},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	
	return []models.Match{match1, match2}
}
