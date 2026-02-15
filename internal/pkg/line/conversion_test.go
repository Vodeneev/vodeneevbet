package line

import (
	"testing"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

func TestToModelsMatch(t *testing.T) {
	start := time.Date(2025, 3, 15, 18, 0, 0, 0, time.UTC)
	m := &Match{
		HomeTeam:  "Team A",
		AwayTeam:  "Team B",
		StartTime:  start,
		Sport:     "football",
		League:    "League 1",
		Bookmaker: "fonbet",
		Markets: []Market{
			{
				EventType:  string(models.StandardEventMainMatch),
				MarketName: models.GetMarketName(models.StandardEventMainMatch),
				Outcomes: []Outcome{
					{OutcomeType: "home_win", Parameter: "", Odds: 2.10},
					{OutcomeType: "draw", Parameter: "", Odds: 3.40},
					{OutcomeType: "away_win", Parameter: "", Odds: 3.20},
				},
			},
			{
				EventType:  string(models.StandardEventMainMatch),
				MarketName: models.GetMarketName(models.StandardEventMainMatch),
				Outcomes: []Outcome{
					{OutcomeType: "total_over", Parameter: "2.5", Odds: 1.90},
					{OutcomeType: "total_under", Parameter: "2.5", Odds: 1.95},
				},
			},
		},
	}
	out := m.ToModelsMatch()
	if out == nil {
		t.Fatal("ToModelsMatch returned nil")
	}
	if out.HomeTeam != "Team A" || out.AwayTeam != "Team B" {
		t.Errorf("teams: got %s vs %s", out.HomeTeam, out.AwayTeam)
	}
	if out.Sport != "football" || out.Tournament != "League 1" {
		t.Errorf("sport=%s tournament=%s", out.Sport, out.Tournament)
	}
	if out.ID != models.CanonicalMatchID("Team A", "Team B", start) {
		t.Errorf("id mismatch: %s", out.ID)
	}
	if len(out.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(out.Events))
	}
	if len(out.Events) > 0 && len(out.Events[0].Outcomes) != 3 {
		t.Errorf("first event: expected 3 outcomes, got %d", len(out.Events[0].Outcomes))
	}
}

func TestToEsportsMatch(t *testing.T) {
	start := time.Date(2025, 4, 1, 20, 0, 0, 0, time.UTC)
	m := &Match{
		HomeTeam:  "Team Liquid",
		AwayTeam:  "Natus Vincere",
		StartTime: start,
		Sport:     "dota2",
		League:    "BLAST Slam",
		Bookmaker: "fonbet",
		Markets: []Market{
			{
				EventType:  "main_match",
				MarketName: "Match Result",
				Outcomes: []Outcome{
					{OutcomeType: "home_win", Parameter: "", Odds: 1.85},
					{OutcomeType: "away_win", Parameter: "", Odds: 1.95},
				},
			},
		},
	}
	out := m.ToEsportsMatch()
	if out == nil {
		t.Fatal("ToEsportsMatch returned nil")
	}
	if out.HomeTeam != "Team Liquid" || out.AwayTeam != "Natus Vincere" {
		t.Errorf("teams: got %s vs %s", out.HomeTeam, out.AwayTeam)
	}
	if out.Discipline != "dota2" || out.Tournament != "BLAST Slam" {
		t.Errorf("discipline=%s tournament=%s", out.Discipline, out.Tournament)
	}
	if out.ID != models.CanonicalMatchID("Team Liquid", "Natus Vincere", start) {
		t.Errorf("id mismatch: %s", out.ID)
	}
	if len(out.Markets) != 1 || len(out.Markets[0].Outcomes) != 2 {
		t.Errorf("expected 1 market with 2 outcomes, got %d markets", len(out.Markets))
	}
}
