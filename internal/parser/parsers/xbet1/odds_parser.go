package xbet1

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// ParseGameDetails parses game details from GetGameZip response into Match model
func ParseGameDetails(game *GameDetails, leagueName string) *models.Match {
	if game == nil {
		return nil
	}

	// Extract team names (prefer English names)
	homeTeam := game.O1E
	if homeTeam == "" {
		homeTeam = game.O1
	}
	awayTeam := game.O2E
	if awayTeam == "" {
		awayTeam = game.O2
	}

	if homeTeam == "" || awayTeam == "" {
		slog.Debug("1xbet: skip game (no home/away)", "game_id", game.I, "o1", game.O1, "o2", game.O2)
		return nil
	}

	// Parse start time (Unix timestamp)
	startTime := time.Unix(game.S, 0).UTC()
	now := time.Now().UTC()

	// Skip past events
	if startTime.Before(now) {
		slog.Debug("1xbet: skip game (past start)", "game_id", game.I, "start_time", startTime.Format(time.RFC3339), "home", homeTeam, "away", awayTeam)
		return nil
	}

	// Build match
	matchID := models.CanonicalMatchID(homeTeam, awayTeam, startTime)

	match := &models.Match{
		ID:         matchID,
		Name:       fmt.Sprintf("%s vs %s", homeTeam, awayTeam),
		HomeTeam:   homeTeam,
		AwayTeam:   awayTeam,
		StartTime:  startTime,
		Sport:      "football",
		Tournament: leagueName,
		Bookmaker:  "1xbet",
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Parse events from grouped events
	events := parseGroupedEvents(matchID, game.GE)
	match.Events = events

	if len(match.Events) == 0 {
		slog.Debug("1xbet: match has no events", "match_id", matchID, "home", homeTeam, "away", awayTeam)
		return nil
	}

	return match
}

// parseGroupedEvents parses grouped events into standard event models
func parseGroupedEvents(matchID string, groupEvents []GroupEvent) []models.Event {
	var events []models.Event
	now := time.Now()

	// Map to track events by type
	eventsByType := make(map[string]*models.Event)

	for _, ge := range groupEvents {
		// Group ID mapping:
		// G=1: Moneyline (1x2)
		// G=2: Handicap (Asian/European)
		// G=17: Total (Over/Under)
		// G=15: Team totals
		// G=19: Both teams to score
		// G=62: Individual totals
		// G=99: Exact score
		// G=2854: Double chance
		// etc.

		switch ge.G {
		case 1:
			// Moneyline (1x2)
			parseMoneyline(eventsByType, matchID, ge, now)
		case 2:
			// Handicap
			parseHandicap(eventsByType, matchID, ge, now)
		case 17:
			// Total (Over/Under)
			parseTotal(eventsByType, matchID, ge, now)
		case 15:
			// Team totals
			parseTeamTotal(eventsByType, matchID, ge, now)
		case 19:
			// Both teams to score
			parseBothTeamsToScore(eventsByType, matchID, ge, now)
		case 62:
			// Individual totals
			parseIndividualTotal(eventsByType, matchID, ge, now)
		case 99:
			// Exact score (skip for now - too many outcomes)
		case 2854:
			// Double chance
			parseDoubleChance(eventsByType, matchID, ge, now)
		case 8:
			// Draw no bet
			parseDrawNoBet(eventsByType, matchID, ge, now)
		default:
			// Skip unknown groups
			slog.Debug("1xbet: skipping unknown group", "group_id", ge.G, "group_sub_id", ge.GS)
		}
	}

	// Convert map to slice
	for _, ev := range eventsByType {
		if len(ev.Outcomes) > 0 {
			events = append(events, *ev)
		}
	}

	return events
}

// parseMoneyline parses moneyline (1x2) events
func parseMoneyline(eventsByType map[string]*models.Event, matchID string, ge GroupEvent, now time.Time) {
	eventID := fmt.Sprintf("%s_1xbet_main_match", matchID)
	ev := getOrCreateEvent(eventsByType, eventID, matchID, string(models.StandardEventMainMatch), now)

	// Find main line (usually first non-empty array or marked with CE=1)
	var mainEvents []Event
	for _, eventArray := range ge.E {
		if len(eventArray) > 0 {
			// Check if this is the main line (CE=1 or first one)
			for _, e := range eventArray {
				if e.CE == 1 || len(mainEvents) == 0 {
					mainEvents = eventArray
					break
				}
			}
			if len(mainEvents) > 0 {
				break
			}
		}
	}

	if len(mainEvents) == 0 && len(ge.E) > 0 && len(ge.E[0]) > 0 {
		mainEvents = ge.E[0]
	}

	for _, e := range mainEvents {
		switch e.T {
		case 1:
			// Home win
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "home_win", "", e.C))
		case 2:
			// Draw
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "draw", "", e.C))
		case 3:
			// Away win
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "away_win", "", e.C))
		}
	}
}

// parseHandicap parses handicap events
func parseHandicap(eventsByType map[string]*models.Event, matchID string, ge GroupEvent, now time.Time) {
	eventID := fmt.Sprintf("%s_1xbet_main_match", matchID)
	ev := getOrCreateEvent(eventsByType, eventID, matchID, string(models.StandardEventMainMatch), now)

	// Find main line (CE=1)
	var mainEvents []Event
	for _, eventArray := range ge.E {
		for _, e := range eventArray {
			if e.CE == 1 {
				mainEvents = eventArray
				break
			}
		}
		if len(mainEvents) > 0 {
			break
		}
	}

	if len(mainEvents) == 0 && len(ge.E) > 0 && len(ge.E[0]) > 0 {
		mainEvents = ge.E[0]
	}

	for _, e := range mainEvents {
		switch e.T {
		case 7:
			// Home handicap
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "handicap_home", formatSignedLine(e.P), e.C))
		case 8:
			// Away handicap
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "handicap_away", formatSignedLine(-e.P), e.C))
		}
	}
}

// parseTotal parses total (over/under) events
func parseTotal(eventsByType map[string]*models.Event, matchID string, ge GroupEvent, now time.Time) {
	eventID := fmt.Sprintf("%s_1xbet_main_match", matchID)
	ev := getOrCreateEvent(eventsByType, eventID, matchID, string(models.StandardEventMainMatch), now)

	// Find main line (CE=1)
	var mainEvents []Event
	for _, eventArray := range ge.E {
		for _, e := range eventArray {
			if e.CE == 1 {
				mainEvents = eventArray
				break
			}
		}
		if len(mainEvents) > 0 {
			break
		}
	}

	if len(mainEvents) == 0 && len(ge.E) > 0 && len(ge.E[0]) > 0 {
		mainEvents = ge.E[0]
	}

	for _, e := range mainEvents {
		line := formatLine(e.P)
		switch e.T {
		case 9:
			// Over
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_over", line, e.C))
		case 10:
			// Under
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_under", line, e.C))
		}
	}
}

// parseTeamTotal parses team total events
func parseTeamTotal(eventsByType map[string]*models.Event, matchID string, ge GroupEvent, now time.Time) {
	// Team totals are less common, skip for now
	// Can be implemented later if needed
}

// parseBothTeamsToScore parses both teams to score events
func parseBothTeamsToScore(eventsByType map[string]*models.Event, matchID string, ge GroupEvent, now time.Time) {
	// Both teams to score - skip for now
	// Can be implemented later if needed
}

// parseIndividualTotal parses individual total events
func parseIndividualTotal(eventsByType map[string]*models.Event, matchID string, ge GroupEvent, now time.Time) {
	// Individual totals - skip for now
	// Can be implemented later if needed
}

// parseDoubleChance parses double chance events
func parseDoubleChance(eventsByType map[string]*models.Event, matchID string, ge GroupEvent, now time.Time) {
	// Double chance - skip for now
	// Can be implemented later if needed
}

// parseDrawNoBet parses draw no bet events
func parseDrawNoBet(eventsByType map[string]*models.Event, matchID string, ge GroupEvent, now time.Time) {
	// Draw no bet - skip for now
	// Can be implemented later if needed
}

// getOrCreateEvent gets or creates an event by type
func getOrCreateEvent(eventsByType map[string]*models.Event, eventID, matchID, eventType string, now time.Time) *models.Event {
	if ev, ok := eventsByType[eventType]; ok {
		return ev
	}
	ev := &models.Event{
		ID:         eventID,
		MatchID:    matchID,
		EventType:  eventType,
		MarketName: models.GetMarketName(models.StandardEventType(eventType)),
		Bookmaker:  "1xbet",
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	eventsByType[eventType] = ev
	return ev
}

// newOutcome creates a new outcome
func newOutcome(eventID, outcomeType, param string, odds float64) models.Outcome {
	now := time.Now()
	id := fmt.Sprintf("%s_%s_%s", eventID, outcomeType, param)
	return models.Outcome{
		ID:          id,
		EventID:     eventID,
		OutcomeType: outcomeType,
		Parameter:   param,
		Odds:        odds,
		Bookmaker:   "1xbet",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// formatLine formats a line value as string
func formatLine(p float64) string {
	return strconv.FormatFloat(p, 'f', -1, 64)
}

// formatSignedLine formats a signed line value
func formatSignedLine(p float64) string {
	if p == 0 {
		return "0"
	}
	if p > 0 {
		return "+" + strconv.FormatFloat(p, 'f', -1, 64)
	}
	return strconv.FormatFloat(p, 'f', -1, 64)
}
