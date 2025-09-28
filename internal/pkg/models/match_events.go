package models

import "time"

// Match represents a main match with all its events
type Match struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	HomeTeam     string    `json:"home_team"`
	AwayTeam     string    `json:"away_team"`
	StartTime    time.Time `json:"start_time"`
	Sport        string    `json:"sport"`
	Tournament   string    `json:"tournament"`
	Bookmaker    string    `json:"bookmaker"`
	Events       []Event   `json:"events"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Event represents a specific event type within a match (corners, yellow cards, etc.)
type Event struct {
	ID          string    `json:"id"`
	MatchID     string    `json:"match_id"`
	EventType   string    `json:"event_type"`   // StandardEventType (corners, yellow_cards, etc.)
	MarketName  string    `json:"market_name"`  // Human-readable market name
	Bookmaker   string    `json:"bookmaker"`
	Outcomes    []Outcome `json:"outcomes"`     // All betting outcomes for this event
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Outcome represents a specific betting outcome within an event
type Outcome struct {
	ID          string  `json:"id"`
	EventID     string  `json:"event_id"`
	OutcomeType string  `json:"outcome_type"` // total_over, total_under, exact_count, etc.
	Parameter   string  `json:"parameter"`    // "2.5", "3", "4-6", etc.
	Odds        float64 `json:"odds"`
	Bookmaker   string  `json:"bookmaker"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// StandardEventType represents standardized event types across all bookmakers
type StandardEventType string

const (
	StandardEventMainMatch      StandardEventType = "main_match"
	StandardEventCorners        StandardEventType = "corners"
	StandardEventYellowCards    StandardEventType = "yellow_cards"
	StandardEventFouls          StandardEventType = "fouls"
	StandardEventShotsOnTarget  StandardEventType = "shots_on_target"
	StandardEventOffsides       StandardEventType = "offsides"
	StandardEventThrowIns       StandardEventType = "throw_ins"
)

// StandardOutcomeType represents standardized outcome types
type StandardOutcomeType string

const (
	// Main match outcomes
	OutcomeTypeHomeWin     StandardOutcomeType = "home_win"
	OutcomeTypeDraw        StandardOutcomeType = "draw"
	OutcomeTypeAwayWin     StandardOutcomeType = "away_win"
	
	// Total outcomes
	OutcomeTypeTotalOver   StandardOutcomeType = "total_over"
	OutcomeTypeTotalUnder  StandardOutcomeType = "total_under"
	
	// Exact count outcomes
	OutcomeTypeExactCount  StandardOutcomeType = "exact_count"
	
	// Alternative totals
	OutcomeTypeAltTotalOver  StandardOutcomeType = "alt_total_over"
	OutcomeTypeAltTotalUnder StandardOutcomeType = "alt_total_under"
)

// GetMarketName returns the market name for a standard event type
func GetMarketName(eventType StandardEventType) string {
	switch eventType {
	case StandardEventMainMatch:
		return "Match Result"
	case StandardEventCorners:
		return "Corners"
	case StandardEventYellowCards:
		return "Yellow Cards"
	case StandardEventFouls:
		return "Fouls"
	case StandardEventShotsOnTarget:
		return "Shots on Target"
	case StandardEventOffsides:
		return "Offsides"
	case StandardEventThrowIns:
		return "Throw-ins"
	default:
		return "Unknown Market"
	}
}

// GetOutcomeTypeName returns a human-readable name for outcome type
func GetOutcomeTypeName(outcomeType StandardOutcomeType) string {
	switch outcomeType {
	case OutcomeTypeHomeWin:
		return "Home Win"
	case OutcomeTypeDraw:
		return "Draw"
	case OutcomeTypeAwayWin:
		return "Away Win"
	case OutcomeTypeTotalOver:
		return "Total Over"
	case OutcomeTypeTotalUnder:
		return "Total Under"
	case OutcomeTypeExactCount:
		return "Exact Count"
	case OutcomeTypeAltTotalOver:
		return "Alternative Total Over"
	case OutcomeTypeAltTotalUnder:
		return "Alternative Total Under"
	default:
		return "Unknown Outcome"
	}
}
