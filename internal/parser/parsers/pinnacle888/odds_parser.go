package pinnacle888

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// parseOddsResponse parses the new odds endpoint response (league odds: leagues with events)
func parseOddsResponse(data []byte) ([]*models.Match, error) {
	var resp OddsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal odds response: %w", err)
	}

	var matches []*models.Match

	for _, league := range resp.Leagues {
		for _, event := range league.Events {
			match := buildMatchFromOddsEvent(league.Name, event)
			if match != nil {
				matches = append(matches, match)
			}
		}
	}

	return matches, nil
}

// ParseEventOddsResponse parses the single-event odds response from /odds/event
func ParseEventOddsResponse(data []byte) (*models.Match, error) {
	var resp EventOddsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal event odds response: %w", err)
	}
	leagueName := resp.Info.LeagueName
	if leagueName == "" {
		leagueName = resp.Info.LeagueCode
	}
	
	// Build match from normal event
	match := buildMatchFromOddsEvent(leagueName, resp.Normal)
	if match == nil {
		return nil, nil
	}
	
	// Add statistical events (corners, bookings) if available
	if resp.Corners != nil {
		addStatisticalEvent(match, resp.Corners, models.StandardEventCorners)
	}
	if resp.Bookings != nil {
		addStatisticalEvent(match, resp.Bookings, models.StandardEventYellowCards)
	}
	
	return match, nil
}

// buildMatchFromOddsEvent builds a Match from an OddsResponse Event
func buildMatchFromOddsEvent(leagueName string, event Event) *models.Match {
	// Extract home and away teams from participants
	var homeTeam, awayTeam string
	for _, p := range event.Participants {
		if p.Type == "HOME" {
			// Prefer English name if available
			if p.EnglishName != "" {
				homeTeam = p.EnglishName
			} else {
				homeTeam = p.Name
			}
		} else if p.Type == "AWAY" {
			if p.EnglishName != "" {
				awayTeam = p.EnglishName
			} else {
				awayTeam = p.Name
			}
		}
	}

	if homeTeam == "" || awayTeam == "" {
		slog.Debug("Pinnacle888: skip event (no home/away)", "eventId", event.ID, "participants", len(event.Participants))
		return nil
	}

	// Parse start time (Unix timestamp in milliseconds)
	startTime := time.Unix(event.Time/1000, 0).UTC()
	now := time.Now().UTC()

	// Skip past events
	if startTime.Before(now) && !event.Live {
		slog.Debug("Pinnacle888: skip event (past start)", "eventId", event.ID, "startTime", startTime.Format(time.RFC3339), "home", homeTeam, "away", awayTeam)
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
		Bookmaker:  "Pinnacle888",
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Parse markets from period "0" (full match)
	if period0, ok := event.Periods["0"]; ok {
		events := parseOddsMarkets(matchID, period0)
		match.Events = events
	}

	return match
}

// parseOddsMarkets parses markets from PeriodData
func parseOddsMarkets(matchID string, period PeriodData) []models.Event {
	var events []models.Event
	now := time.Now()

	// Parse moneyline (1x2)
	if !period.MoneyLine.Offline && !period.MoneyLine.Unavailable {
		homeOdds := parseOddsString(period.MoneyLine.HomePrice)
		awayOdds := parseOddsString(period.MoneyLine.AwayPrice)
		drawOdds := parseOddsString(period.MoneyLine.DrawPrice)

		if homeOdds > 0 || awayOdds > 0 || drawOdds > 0 {
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
	// Use main line (indexMainLineHdp) if available, otherwise use first non-alternate
	if len(period.Handicap) > 0 {
		var mainHandicap *HandicapMarket
		if period.IndexMainLineHdp >= 0 && period.IndexMainLineHdp < len(period.Handicap) {
			mainHandicap = &period.Handicap[period.IndexMainLineHdp]
		} else {
			// Find first non-alternate handicap
			for i := range period.Handicap {
				if !period.Handicap[i].IsAlt && !period.Handicap[i].Offline && !period.Handicap[i].Unavailable {
					mainHandicap = &period.Handicap[i]
					break
				}
			}
		}

		if mainHandicap != nil && !mainHandicap.Offline && !mainHandicap.Unavailable {
			homeLine := parseFloatString(mainHandicap.HomeSpread)
			awayLine := parseFloatString(mainHandicap.AwaySpread)
			homeOdds := parseOddsString(mainHandicap.HomeOdds)
			awayOdds := parseOddsString(mainHandicap.AwayOdds)

			if homeOdds > 0 || awayOdds > 0 {
				eventID := fmt.Sprintf("%s_pinnacle888_main_match", matchID)
				// Find existing event or create new
				var foundEvent *models.Event
				for i := range events {
					if events[i].EventType == string(models.StandardEventMainMatch) {
						foundEvent = &events[i]
						break
					}
				}
				if foundEvent == nil {
					events = append(events, models.Event{
						ID:         eventID,
						MatchID:    matchID,
						EventType:  string(models.StandardEventMainMatch),
						MarketName: models.GetMarketName(models.StandardEventMainMatch),
						Bookmaker:  "Pinnacle888",
						Outcomes:   []models.Outcome{},
						CreatedAt:  now,
						UpdatedAt:  now,
					})
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

	// Parse total markets (over/under)
	// Use main line (indexMainLineOU) if available, otherwise use first non-alternate
	if len(period.OverUnder) > 0 {
		var mainTotal *TotalMarket
		if period.IndexMainLineOU >= 0 && period.IndexMainLineOU < len(period.OverUnder) {
			mainTotal = &period.OverUnder[period.IndexMainLineOU]
		} else {
			// Find first non-alternate total
			for i := range period.OverUnder {
				if !period.OverUnder[i].IsAlt && !period.OverUnder[i].Offline && !period.OverUnder[i].Unavailable {
					mainTotal = &period.OverUnder[i]
					break
				}
			}
		}

		if mainTotal != nil && !mainTotal.Offline && !mainTotal.Unavailable {
			line := parseFloatString(mainTotal.Points)
			overOdds := parseOddsString(mainTotal.OverOdds)
			underOdds := parseOddsString(mainTotal.UnderOdds)

			if overOdds > 0 || underOdds > 0 {
				eventID := fmt.Sprintf("%s_pinnacle888_main_match", matchID)
				// Find existing event or create new
				var foundEvent *models.Event
				for i := range events {
					if events[i].EventType == string(models.StandardEventMainMatch) {
						foundEvent = &events[i]
						break
					}
				}
				if foundEvent == nil {
					events = append(events, models.Event{
						ID:         eventID,
						MatchID:    matchID,
						EventType:  string(models.StandardEventMainMatch),
						MarketName: models.GetMarketName(models.StandardEventMainMatch),
						Bookmaker:  "Pinnacle888",
						Outcomes:   []models.Outcome{},
						CreatedAt:  now,
						UpdatedAt:  now,
					})
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

	return events
}

// addStatisticalEvent adds a statistical event (corners, bookings, etc.) to the match
func addStatisticalEvent(match *models.Match, statEvent *Event, eventType models.StandardEventType) {
	// Parse markets from period "0" (full match)
	if period0, ok := statEvent.Periods["0"]; ok {
		statEvents := parseOddsMarkets(match.ID, period0)
		
		// Find or create the statistical event
		var statEventModel *models.Event
		for i := range match.Events {
			if match.Events[i].EventType == string(eventType) {
				statEventModel = &match.Events[i]
				break
			}
		}
		
		if statEventModel == nil {
			// Create new statistical event
			eventID := fmt.Sprintf("%s_pinnacle888_%s", match.ID, string(eventType))
			statEventModel = &models.Event{
				ID:         eventID,
				MatchID:    match.ID,
				EventType:  string(eventType),
				MarketName: models.GetMarketName(eventType),
				Bookmaker:  "Pinnacle888",
				Outcomes:   []models.Outcome{},
				CreatedAt:  match.CreatedAt,
				UpdatedAt:  match.UpdatedAt,
			}
			match.Events = append(match.Events, *statEventModel)
			statEventModel = &match.Events[len(match.Events)-1]
		}
		
		// Merge outcomes from statistical event
		if len(statEvents) > 0 {
			statEventModel.Outcomes = append(statEventModel.Outcomes, statEvents[0].Outcomes...)
		}
	}
}

// parseOddsString parses odds from string (decimal format)
func parseOddsString(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// parseFloatString parses float from string
func parseFloatString(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
