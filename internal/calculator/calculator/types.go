package calculator

import "time"

// DiffBet represents a "same bet" odds diff between bookmakers.
type DiffBet struct {
	MatchGroupKey string    `json:"match_group_key"`
	MatchName     string    `json:"match_name"`
	StartTime     time.Time `json:"start_time"`
	Sport         string    `json:"sport"`

	EventType    string `json:"event_type"`   // e.g. main_match, corners
	OutcomeType  string `json:"outcome_type"` // e.g. total_over, home_win
	Parameter    string `json:"parameter"`    // e.g. 2.5, +1.5
	BetKey       string `json:"bet_key"`      // eventType|outcomeType|parameter
	Bookmakers   int    `json:"bookmakers"`   // number of bookmakers contributing

	MinBookmaker string  `json:"min_bookmaker"`
	MinOdd       float64 `json:"min_odd"`
	MaxBookmaker string  `json:"max_bookmaker"`
	MaxOdd       float64 `json:"max_odd"`

	DiffAbs     float64 `json:"diff_abs"`     // max - min
	DiffPercent float64 `json:"diff_percent"` // (max/min - 1) * 100

	CalculatedAt time.Time `json:"calculated_at"`
}

// ValueBet represents a value bet found using weighted average of reference bookmakers.
type ValueBet struct {
	MatchGroupKey string    `json:"match_group_key"`
	MatchName     string    `json:"match_name"`
	StartTime     time.Time `json:"start_time"`
	Sport         string    `json:"sport"`

	EventType   string `json:"event_type"`   // e.g. main_match, corners
	OutcomeType string `json:"outcome_type"` // e.g. total_over, home_win
	Parameter   string `json:"parameter"`   // e.g. 2.5, +1.5
	BetKey      string `json:"bet_key"`      // eventType|outcomeType|parameter

	// Reference data (средневзвешенное от всех контор)
	AllBookmakerOdds map[string]float64 `json:"all_bookmaker_odds"` // все коэффициенты от всех контор для этого исхода
	FairOdd          float64            `json:"fair_odd"`            // справедливый коэффициент (1 / avg_probability)
	FairProbability  float64            `json:"fair_probability"`   // справедливая вероятность (средневзвешенная)

	// Value bet data
	Bookmaker    string  `json:"bookmaker"`     // контора с валуем
	BookmakerOdd float64 `json:"bookmaker_odd"` // её коэффициент
	ValuePercent float64 `json:"value_percent"`  // процент валуя: (bookmaker_odd / fair_odd - 1) * 100
	ExpectedValue float64 `json:"expected_value"` // математическое ожидание: (bookmaker_odd * fair_probability) - 1

	CalculatedAt time.Time `json:"calculated_at"`
}

// LineMovement represents a significant odds change in the same bookmaker for the same bet.
type LineMovement struct {
	MatchGroupKey string    `json:"match_group_key"`
	MatchName     string    `json:"match_name"`
	StartTime     time.Time `json:"start_time"`
	Sport         string    `json:"sport"`

	EventType   string    `json:"event_type"`
	OutcomeType string    `json:"outcome_type"`
	Parameter   string    `json:"parameter"`
	BetKey      string    `json:"bet_key"`
	Bookmaker   string    `json:"bookmaker"`
	PreviousOdd float64   `json:"previous_odd"`
	CurrentOdd  float64   `json:"current_odd"`
	ChangeAbs   float64   `json:"change_abs"` // current - previous (positive = рост, negative = падение)
	RecordedAt  time.Time `json:"recorded_at"`
}

