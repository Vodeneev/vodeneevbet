package models

import (
	"time"
)

// ValueBet представляет найденную валуйную ставку
type ValueBet struct {
	ID              string    `json:"id"`
	MatchID         string    `json:"match_id"`
	MatchName       string    `json:"match_name"`
	MatchTime       time.Time `json:"match_time"`
	Sport           string    `json:"sport"`
	Market          string    `json:"market"`
	Outcome         string    `json:"outcome"`          // "win_home", "draw", "over_2.5"
	
	// Коэффициенты
	BookmakerOdd    float64   `json:"bookmaker_odd"`    // Коэффициент в БК
	ReferenceOdd    float64   `json:"reference_odd"`    // Референсный коэффициент
	ValuePercent    float64   `json:"value_percent"`    // Процент value (15.5%)
	
	// Метаданные
	Bookmaker       string    `json:"bookmaker"`        // Название БК
	ReferenceSource string    `json:"reference_source"` // Источник референса
	Stake           float64   `json:"stake"`            // Рекомендуемая ставка
	PotentialWin    float64   `json:"potential_win"`   // Потенциальный выигрыш
	
	// Временные метки
	FoundAt         time.Time `json:"found_at"`
	ExpiresAt       time.Time `json:"expires_at"`       // Время истечения
}

// ReferenceData представляет референсные данные для расчета
type ReferenceData struct {
	MatchID         string             `json:"match_id"`
	Market          string             `json:"market"`
	Outcome         string             `json:"outcome"`
	ReferenceOdd    float64           `json:"reference_odd"`
	Source          string             `json:"source"`           // "pinnacle", "average_top5"
	BookmakerOdds   map[string]float64 `json:"bookmaker_odds"`    // БК -> коэффициент
	CalculatedAt    time.Time          `json:"calculated_at"`
}

// ValueBetFilter представляет фильтры для поиска value bet
type ValueBetFilter struct {
	MinValuePercent   float64   `json:"min_value_percent"`
	MaxRiskPercent    float64   `json:"max_risk_percent"`
	MinStake          float64   `json:"min_stake"`
	MaxStake          float64   `json:"max_stake"`
	Sports            []string  `json:"sports"`
	Markets           []string  `json:"markets"`
	Bookmakers        []string  `json:"bookmakers"`
	MinTimeToMatch    time.Duration `json:"min_time_to_match"`
	MaxTimeToMatch    time.Duration `json:"max_time_to_match"`
}

// ValueBetStats представляет статистику по value bet
type ValueBetStats struct {
	TotalFound       int     `json:"total_found"`
	TotalValue       float64 `json:"total_value"`
	AverageValue     float64 `json:"average_value"`
	BestValue        float64 `json:"best_value"`
	SportsBreakdown  map[string]int `json:"sports_breakdown"`
	MarketsBreakdown map[string]int `json:"markets_breakdown"`
	BookmakersBreakdown map[string]int `json:"bookmakers_breakdown"`
}
