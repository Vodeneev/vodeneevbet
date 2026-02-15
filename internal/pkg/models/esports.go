package models

import "time"

// EsportsMatch — модель матча по киберспорту (Dota 2, CS и т.д.).
// Отдельная от футбольной Match: футбол остаётся в Match/Event/Outcome.
type EsportsMatch struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	HomeTeam   string           `json:"home_team"`
	AwayTeam   string           `json:"away_team"`
	StartTime  time.Time        `json:"start_time"`
	Discipline string           `json:"discipline"` // dota2, cs
	Tournament string           `json:"tournament"`
	Bookmaker  string           `json:"bookmaker"`
	Markets    []EsportsMarket  `json:"markets"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EsportsMarket — один рынок по киберспорту (исход матча, тотал карт, фора и т.д.).
type EsportsMarket struct {
	ID         string            `json:"id"`
	MatchID    string            `json:"match_id"`
	MarketType string            `json:"market_type"` // main_match, total_maps, handicap_maps, etc.
	MarketName string            `json:"market_name"`
	Bookmaker  string            `json:"bookmaker"`
	Outcomes   []EsportsOutcome  `json:"outcomes"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// EsportsOutcome — один исход в рынке киберспорта.
type EsportsOutcome struct {
	ID          string    `json:"id"`
	MarketID    string    `json:"market_id"`
	OutcomeType string    `json:"outcome_type"` // home_win, away_win, total_over, total_under, etc.
	Parameter   string    `json:"parameter"`    // линия: "2.5", "+1.5"
	Odds        float64   `json:"odds"`
	Bookmaker   string    `json:"bookmaker"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
