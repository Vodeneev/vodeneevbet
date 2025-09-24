package models

import (
	"time"
)

// Odd represents bookmaker coefficient
type Odd struct {
	MatchID    string             `json:"match_id"`
	Bookmaker  string             `json:"bookmaker"`
	Market     string             `json:"market"`     // "1x2", "total", "handicap"
	Outcomes   map[string]float64 `json:"outcomes"`   // {"win_a": 2.05, "win_b": 3.10, "draw": 3.20}
	UpdatedAt  time.Time          `json:"updated_at"`
	MatchName  string             `json:"match_name"`
	MatchTime  time.Time          `json:"match_time"`
	Sport      string             `json:"sport"`
}

// Arbitrage represents found arbitrage situation
type Arbitrage struct {
	ID           string    `json:"id"`
	MatchID      string    `json:"match_id"`
	MatchName    string    `json:"match_name"`
	MatchTime    time.Time `json:"match_time"`
	Sport        string    `json:"sport"`
	Market       string    `json:"market"`
	ProfitPercent float64  `json:"profit_percent"`
	Bets         []Bet     `json:"bets"`
	FoundAt      time.Time `json:"found_at"`
}

// Bet represents bet in arbitrage situation
type Bet struct {
	Bookmaker string  `json:"bookmaker"`
	Outcome   string  `json:"outcome"`
	Odd       float64 `json:"odd"`
	Stake     float64 `json:"stake"`    // Bet size as percentage of bank
	Return    float64 `json:"return"`   // Return on win
}
