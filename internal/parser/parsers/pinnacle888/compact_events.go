package pinnacle888

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// CompactEventsResponse represents the compact events API response
type CompactEventsResponse struct {
	L [][]interface{} `json:"l"` // Live Leagues array or Sports array
	N [][]interface{} `json:"n"` // Normal Leagues array or Sports array (pre-match)
}

// parseCompactEvents parses compact events format and converts to Match models
// includeLive: if true, parse live events from "l" key
// includePrematch: if true, parse pre-match events from "n" key
func parseCompactEvents(data []byte, includeLive, includePrematch bool) ([]*models.Match, error) {
	var resp CompactEventsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal compact events: %w", err)
	}

	var matches []*models.Match

	// Parse live events if requested
	if includeLive && len(resp.L) > 0 {
		liveMatches, err := parseCompactDataSection(resp.L, true)
		if err != nil {
			return nil, fmt.Errorf("parse live events: %w", err)
		}
		matches = append(matches, liveMatches...)
	}

	// Parse pre-match events if requested
	if includePrematch && len(resp.N) > 0 {
		prematchMatches, err := parseCompactDataSection(resp.N, false)
		if err != nil {
			return nil, fmt.Errorf("parse pre-match events: %w", err)
		}
		matches = append(matches, prematchMatches...)
	}

	return matches, nil
}

// parseCompactDataSection parses either live (L) or pre-match (N) data section
// isLiveSection: true for live events, false for pre-match
func parseCompactDataSection(dataSection [][]interface{}, isLiveSection bool) ([]*models.Match, error) {
	var matches []*models.Match

	// Structure for live (L): l[0] = [sportID, sportName, [leagues...]]
	// Structure for pre-match (N): n[0] = [sportID, sportName, [leagues...]]
	// Each league: [leagueID, leagueName, [events...], ...]
	// Each event: [eventID, homeTeam, awayTeam, status, startTime, ..., markets, ...]
	for _, sportData := range dataSection {
		if len(sportData) < 3 {
			continue
		}

		// sportData[2] should be array of leagues
		leagues, ok := sportData[2].([]interface{})
		if !ok {
			continue
		}

		for _, leagueData := range leagues {
			leagueMatches, err := processCompactLeague(leagueData, isLiveSection)
			if err != nil {
				continue // Skip invalid leagues
			}
			matches = append(matches, leagueMatches...)
		}
	}

	return matches, nil
}

// processCompactLeague processes a single league and returns its matches
func processCompactLeague(leagueData interface{}, isLiveEvent bool) ([]*models.Match, error) {
	league, ok := leagueData.([]interface{})
	if !ok || len(league) < 3 {
		return nil, fmt.Errorf("invalid league data")
	}

	// league[2] should be array of events
	events, ok := league[2].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid events array")
	}

	leagueName := ""
	if name, ok := league[1].(string); ok {
		leagueName = name
	}

	var matches []*models.Match
	for _, eventData := range events {
		match := buildMatchFromCompactEvent(leagueName, eventData, isLiveEvent)
		if match != nil {
			matches = append(matches, match)
		}
	}

	return matches, nil
}

// buildMatchFromCompactEvent builds a Match from a compact event
func buildMatchFromCompactEvent(leagueName string, eventData interface{}, isLiveEvent bool) *models.Match {
	event, ok := eventData.([]interface{})
	if !ok || len(event) < 8 {
		return nil
	}

	// Parse event: [eventID, homeTeam, awayTeam, status, startTime, ..., markets, ...]
	// Note: Pinnacle888 API returns teams in normal order: [home, away]
	eventID, ok := event[0].(float64)
	if !ok {
		return nil
	}

	// Teams are in normal order: event[1] = home, event[2] = away
	// But these may be in Russian. Check for English names at event[24] and event[25]
	homeTeam, ok := event[1].(string)
	if !ok {
		return nil
	}

	awayTeam, ok := event[2].(string)
	if !ok {
		return nil
	}

	// Prefer English team names if available (at indices 24-25)
	// This ensures proper match merging with other bookmakers
	if len(event) > 25 {
		if engHome, ok := event[24].(string); ok && engHome != "" {
			homeTeam = engHome
		}
		if engAway, ok := event[25].(string); ok && engAway != "" {
			awayTeam = engAway
		}
	}

	// Status: 1 = live, 0 = upcoming/pre-match
	status, ok := event[3].(float64)
	if !ok {
		return nil
	}

	// Start time in milliseconds
	startTimeMs, ok := event[4].(float64)
	if !ok {
		return nil
	}

	startTime := time.Unix(int64(startTimeMs)/1000, 0).UTC()
	now := time.Now().UTC()

	// For pre-match events: skip if in the past
	// Status values: 0 = finished, 1 = live, 9 = upcoming/pre-match, 37 = upcoming with markets
	// Note: Status 7, 5, 6, 3 etc. can also indicate live matches in progress
	if !isLiveEvent {
		if startTime.Before(now) {
			return nil // Skip past events
		}
		// Include pre-match events (status 9 or 37 typically means upcoming)
		// Don't filter by status for pre-match - include all future events
	} else {
		// For live events: include if match has started (startTime <= now) and not finished
		// Status 1 = live, but status 7, 5, 6, 3 etc. can also indicate live matches
		// Check if match has started (startTime <= now) and status is not 0 (finished)
		if startTime.After(now) {
			return nil // Match hasn't started yet
		}
		if int(status) == 0 {
			return nil // Match is finished
		}
		// Include all matches that have started and are not finished (status != 0)
	}

	// Parse markets if available (event[8] contains markets as map)
	var marketsData interface{}
	if len(event) > 8 {
		marketsData = event[8]
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
		Bookmaker:  "Pinnacle888",
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Parse markets if available
	if marketsData != nil {
		events := parseCompactMarkets(matchID, int64(eventID), marketsData)
		match.Events = events
	}

	return match
}

// Legacy function for backward compatibility - parses only live events
func parseCompactEventsLegacy(data []byte) ([]*models.Match, error) {
	return parseCompactEvents(data, true, false)
}

// parseCompactMarkets parses markets from compact format
func parseCompactMarkets(matchID string, eventID int64, marketsData interface{}) []models.Event {
	var events []models.Event
	now := time.Now()

	// marketsData is a map[string]interface{} where key is period (e.g., "0" for full match, "1" for first half)
	marketsMap, ok := marketsData.(map[string]interface{})
	if !ok {
		return events
	}

	// Process period "0" (full match) markets
	period0, ok := marketsMap["0"].([]interface{})
	if !ok || len(period0) < 4 {
		return events
	}

	// period0[0] = handicap markets (spread)
	// period0[1] = total markets
	// period0[2] = moneyline (1x2)
	// period0[3] = other markets

	// Parse moneyline (1x2) - period0[2] is [homeOdds, drawOdds, awayOdds, ...]
	// Note: Pinnacle888 API returns odds in normal order: [home, draw, away]
	if len(period0) > 2 {
		moneylineData := period0[2]
		if moneyline, ok := moneylineData.([]interface{}); ok && len(moneyline) >= 3 {
			// Format: [homeOdds, drawOdds, awayOdds, ...] - normal order
			homeOdds := parseOdds(moneyline[0])
			drawOdds := parseOdds(moneyline[1])
			awayOdds := parseOdds(moneyline[2])

			eventID := fmt.Sprintf("%s_pinnacle888_main_match", matchID)
			event := models.Event{
				ID:         eventID,
				MatchID:    matchID,
				EventType:  string(models.StandardEventMainMatch),
				MarketName: models.GetMarketName(models.StandardEventMainMatch),
				Bookmaker:  "Pinnacle888",
				Outcomes:   []models.Outcome{},
				CreatedAt:  now,
				UpdatedAt:  now,
			}

			if homeOdds > 0 {
				event.Outcomes = append(event.Outcomes, models.Outcome{
					ID:          fmt.Sprintf("%s_home_win", eventID),
					EventID:     eventID,
					OutcomeType: "home_win",
					Parameter:   "",
					Odds:        homeOdds,
					Bookmaker:   "Pinnacle888",
					CreatedAt:   now,
					UpdatedAt:   now,
				})
			}
			if drawOdds > 0 {
				event.Outcomes = append(event.Outcomes, models.Outcome{
					ID:          fmt.Sprintf("%s_draw", eventID),
					EventID:     eventID,
					OutcomeType: "draw",
					Parameter:   "",
					Odds:        drawOdds,
					Bookmaker:   "Pinnacle888",
					CreatedAt:   now,
					UpdatedAt:   now,
				})
			}
			if awayOdds > 0 {
				event.Outcomes = append(event.Outcomes, models.Outcome{
					ID:          fmt.Sprintf("%s_away_win", eventID),
					EventID:     eventID,
					OutcomeType: "away_win",
					Parameter:   "",
					Odds:        awayOdds,
					Bookmaker:   "Pinnacle888",
					CreatedAt:   now,
					UpdatedAt:   now,
				})
			}

			if len(event.Outcomes) > 0 {
				events = append(events, event)
			}
		}
	}

	// Parse handicap markets (spread)
	if len(period0) > 0 {
		handicapData := period0[0]
		if handicaps, ok := handicapData.([]interface{}); ok {
			for _, h := range handicaps {
				if handicap, ok := h.([]interface{}); ok && len(handicap) >= 5 {
					// Note: Pinnacle888 API returns handicap data: [awayLine, homeLine, ..., awayOdds, homeOdds]
					// handicap[0] = awayLine, handicap[1] = homeLine
					// handicap[3] = awayOdds, handicap[4] = homeOdds
					awayLine := parseFloat(handicap[0])
					homeLine := parseFloat(handicap[1])
					awayOdds := parseOdds(handicap[3])
					homeOdds := parseOdds(handicap[4])

					if homeOdds > 0 || awayOdds > 0 {
						eventID := fmt.Sprintf("%s_pinnacle888_main_match", matchID)
						event := models.Event{
							ID:         eventID,
							MatchID:    matchID,
							EventType:  string(models.StandardEventMainMatch),
							MarketName: models.GetMarketName(models.StandardEventMainMatch),
							Bookmaker:  "Pinnacle888",
							Outcomes:   []models.Outcome{},
							CreatedAt:  now,
							UpdatedAt:  now,
						}

						// Find existing event or create new
						var foundEvent *models.Event
						for i := range events {
							if events[i].EventType == string(models.StandardEventMainMatch) {
								foundEvent = &events[i]
								break
							}
						}
						if foundEvent == nil {
							events = append(events, event)
							foundEvent = &events[len(events)-1]
						}

						if homeOdds > 0 {
							foundEvent.Outcomes = append(foundEvent.Outcomes, models.Outcome{
								ID:          fmt.Sprintf("%s_handicap_home_%s", foundEvent.ID, formatLine(homeLine)),
								EventID:     foundEvent.ID,
								OutcomeType: "handicap_home",
								Parameter:   formatSignedLine(homeLine),
								Odds:        homeOdds,
								Bookmaker:   "Pinnacle888",
								CreatedAt:   now,
								UpdatedAt:   now,
							})
						}
						if awayOdds > 0 {
							foundEvent.Outcomes = append(foundEvent.Outcomes, models.Outcome{
								ID:          fmt.Sprintf("%s_handicap_away_%s", foundEvent.ID, formatLine(awayLine)),
								EventID:     foundEvent.ID,
								OutcomeType: "handicap_away",
								Parameter:   formatSignedLine(awayLine),
								Odds:        awayOdds,
								Bookmaker:   "Pinnacle888",
								CreatedAt:   now,
								UpdatedAt:   now,
							})
						}
					}
				}
			}
		}
	}

	// Parse total markets
	if len(period0) > 1 {
		totalData := period0[1]
		if totals, ok := totalData.([]interface{}); ok {
			for _, t := range totals {
				if total, ok := t.([]interface{}); ok && len(total) >= 3 {
					line := parseFloat(total[0])
					overOdds := parseOdds(total[1])
					underOdds := parseOdds(total[2])

					if overOdds > 0 || underOdds > 0 {
						eventID := fmt.Sprintf("%s_pinnacle888_main_match", matchID)
						event := models.Event{
							ID:         eventID,
							MatchID:    matchID,
							EventType:  string(models.StandardEventMainMatch),
							MarketName: models.GetMarketName(models.StandardEventMainMatch),
							Bookmaker:  "Pinnacle888",
							Outcomes:   []models.Outcome{},
							CreatedAt:  now,
							UpdatedAt:  now,
						}

						// Find existing event or create new
						var foundEvent *models.Event
						for i := range events {
							if events[i].EventType == string(models.StandardEventMainMatch) {
								foundEvent = &events[i]
								break
							}
						}
						if foundEvent == nil {
							events = append(events, event)
							foundEvent = &events[len(events)-1]
						}

						if overOdds > 0 {
							foundEvent.Outcomes = append(foundEvent.Outcomes, models.Outcome{
								ID:          fmt.Sprintf("%s_total_over_%s", foundEvent.ID, formatLine(line)),
								EventID:     foundEvent.ID,
								OutcomeType: "total_over",
								Parameter:   formatLine(line),
								Odds:        overOdds,
								Bookmaker:   "Pinnacle888",
								CreatedAt:   now,
								UpdatedAt:   now,
							})
						}
						if underOdds > 0 {
							foundEvent.Outcomes = append(foundEvent.Outcomes, models.Outcome{
								ID:          fmt.Sprintf("%s_total_under_%s", foundEvent.ID, formatLine(line)),
								EventID:     foundEvent.ID,
								OutcomeType: "total_under",
								Parameter:   formatLine(line),
								Odds:        underOdds,
								Bookmaker:   "Pinnacle888",
								CreatedAt:   now,
								UpdatedAt:   now,
							})
						}
					}
				}
			}
		}
	}

	return events
}

func parseOdds(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return 0
}

func parseFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	}
	return 0
}

func formatLine(points float64) string {
	// For totals lines we keep unsigned.
	return strconv.FormatFloat(points, 'f', -1, 64)
}

func formatSignedLine(points float64) string {
	if points == 0 {
		return "0"
	}
	if points > 0 {
		return "+" + strconv.FormatFloat(points, 'f', -1, 64)
	}
	return strconv.FormatFloat(points, 'f', -1, 64)
}
