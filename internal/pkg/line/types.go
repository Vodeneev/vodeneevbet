// Package line defines the unified line model for mapping multiple bookmakers.
// All parsers (Fonbet, xbet, etc.) conceptually map their API responses to this structure;
// then ToModelsMatch() produces the storage/API model used by the rest of the app.
package line

import "time"

// Match is the unified line representation: one match from any bookmaker.
// Fields are normalized so that the same match from different bookmakers can be compared.
type Match struct {
	HomeTeam  string
	AwayTeam  string
	StartTime time.Time
	Sport     string // canonical alias: football, dota2, cs (enums.Sport)
	League    string
	Bookmaker string
	Markets   []Market
}

// Market is one betting market (main result, total, corners, etc.).
// EventType uses models.StandardEventType (main_match, corners, yellow_cards, ...).
type Market struct {
	EventType string   // StandardEventType
	MarketName string  // human-readable (e.g. "Corners")
	Outcomes  []Outcome
}

// Outcome is one outcome (selection) within a market.
// OutcomeType uses standard types: home_win, draw, total_over, total_under, handicap_home, etc.
type Outcome struct {
	OutcomeType string  // StandardOutcomeType or bookmaker-specific normalized to standard
	Parameter   string  // line value: "2.5", "+1.5", "-2"
	Odds        float64
}
