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
	matchName := fmt.Sprintf("%s vs %s", homeTeam, awayTeam)

	match := &models.Match{
		ID:         matchID,
		Name:       matchName,
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

	// Log match parsing start
	slog.Info("1xbet: parsing match", "match", matchName, "match_id", matchID, "home", homeTeam, "away", awayTeam, "sub_games_count", len(game.SG))

	// Parse events from grouped events (use SG metadata to identify statistical groups)
	events := parseGroupedEvents(matchID, game.GE, game.SG)
	match.Events = events

	if len(match.Events) == 0 {
		slog.Debug("1xbet: match has no events", "match", matchName, "match_id", matchID, "home", homeTeam, "away", awayTeam)
		return nil
	}

	slog.Info("1xbet: match parsed with main events", "match", matchName, "match_id", matchID, "main_events_count", len(match.Events))

	return match
}

// ParseGameDetailsWithClient parses game details and fetches statistical sub-games
func ParseGameDetailsWithClient(game *GameDetails, leagueName string, client *Client) *models.Match {
	match := ParseGameDetails(game, leagueName)
	if match == nil {
		return nil
	}

	// Log start of statistical sub-games parsing
	slog.Info("1xbet: starting statistical sub-games parsing", "match", match.Name, "match_id", match.ID, "sub_games_available", len(game.SG))

	// Find and parse statistical sub-games (corners, fouls, yellow cards, offsides)
	statisticalEvents := parseStatisticalSubGames(match.ID, match.Name, game.SG, client)
	if len(statisticalEvents) > 0 {
		match.Events = append(match.Events, statisticalEvents...)
		eventTypes := make([]string, len(statisticalEvents))
		for i, ev := range statisticalEvents {
			eventTypes[i] = ev.EventType
		}
		slog.Info("1xbet: added statistical events", "match", match.Name, "match_id", match.ID, "event_types", eventTypes, "events_count", len(statisticalEvents), "total_outcomes", countTotalOutcomes(statisticalEvents))
	} else {
		slog.Info("1xbet: no statistical events found", "match", match.Name, "match_id", match.ID, "sub_games_available", len(game.SG))
	}

	return match
}

// parseStatisticalSubGames parses statistical sub-games (corners, fouls, yellow cards, offsides)
func parseStatisticalSubGames(matchID string, matchName string, subGames []SubGame, client *Client) []models.Event {
	var events []models.Event
	now := time.Now()
	eventsByType := make(map[string]*models.Event)

	// Log all sub-games for debugging
	allSubGameTitles := make([]string, 0, len(subGames))
	for _, sg := range subGames {
		if sg.TG != "" {
			allSubGameTitles = append(allSubGameTitles, fmt.Sprintf("%s(CI:%d,PN:%s)", sg.TG, sg.CI, sg.PN))
		}
	}
	slog.Info("1xbet: checking sub-games for statistical events", "match", matchName, "match_id", matchID, "total_sub_games", len(subGames), "sub_game_titles", allSubGameTitles)

	// Map sub-game titles to event types
	subGameMap := make(map[int64]string) // Maps CI -> event type
	for _, sg := range subGames {
		if sg.TG == "" || sg.PN != "" {
			if sg.TG != "" {
				slog.Debug("1xbet: skipping sub-game", "match", matchName, "match_id", matchID, "title", sg.TG, "reason", "empty title or period-specific", "CI", sg.CI, "PN", sg.PN)
			}
			continue // Skip empty titles or period-specific sub-games
		}
		var eventType string
		switch sg.TG {
		case "Угловые":
			eventType = string(models.StandardEventCorners)
		case "Фолы":
			eventType = string(models.StandardEventFouls)
		case "Желтые карточки":
			eventType = string(models.StandardEventYellowCards)
		case "Офсайды":
			eventType = string(models.StandardEventOffsides)
		default:
			slog.Debug("1xbet: unknown sub-game title", "match", matchName, "match_id", matchID, "title", sg.TG, "CI", sg.CI)
			continue
		}
		subGameMap[sg.CI] = eventType
		slog.Info("1xbet: found statistical sub-game", "match", matchName, "match_id", matchID, "title", sg.TG, "event_type", eventType, "sub_game_id", sg.CI)
	}

	if len(subGameMap) > 0 {
		slog.Info("1xbet: found statistical sub-games", "match", matchName, "match_id", matchID, "sub_games_count", len(subGameMap), "event_types", getMapValues(subGameMap))
	} else {
		slog.Info("1xbet: no statistical sub-games found", "match", matchName, "match_id", matchID, "total_sub_games", len(subGames))
	}

	// Fetch and parse each statistical sub-game
	for subGameCI, eventType := range subGameMap {
		slog.Info("1xbet: fetching sub-game", "match", matchName, "match_id", matchID, "sub_game_id", subGameCI, "event_type", eventType)
		subGameData, err := client.GetSubGame(subGameCI)
		if err != nil {
			slog.Warn("1xbet: failed to fetch sub-game", "match", matchName, "match_id", matchID, "sub_game_id", subGameCI, "event_type", eventType, "error", err)
			continue
		}

		slog.Info("1xbet: fetched sub-game data", "match", matchName, "match_id", matchID, "sub_game_id", subGameCI, "event_type", eventType, "group_events_count", len(subGameData.GE))

		// Parse all markets from sub-game
		subGameEvents := parseStatisticalSubGameMarkets(matchID, matchName, subGameData.GE, eventType, now)
		if len(subGameEvents) > 0 {
			for _, ev := range subGameEvents {
				if existingEv, ok := eventsByType[ev.EventType]; ok {
					// Merge outcomes if event already exists
					existingEv.Outcomes = append(existingEv.Outcomes, ev.Outcomes...)
				} else {
					eventsByType[ev.EventType] = &ev
				}
			}
			slog.Info("1xbet: parsed statistical sub-game markets", "match", matchName, "match_id", matchID, "event_type", eventType, "markets_count", len(subGameEvents), "outcomes_count", countTotalOutcomes(subGameEvents))
		} else {
			slog.Warn("1xbet: no markets found in sub-game", "match", matchName, "match_id", matchID, "event_type", eventType, "sub_game_id", subGameCI, "group_events_count", len(subGameData.GE))
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

// countTotalOutcomes counts total outcomes across all events
func countTotalOutcomes(events []models.Event) int {
	total := 0
	for _, ev := range events {
		total += len(ev.Outcomes)
	}
	return total
}

// getMapValues returns all values from a map as a slice
func getMapValues(m map[int64]string) []string {
	values := make([]string, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	return values
}

// parseStatisticalSubGameMarkets parses all markets from a statistical sub-game
func parseStatisticalSubGameMarkets(matchID string, matchName string, groupEvents []GroupEvent, eventType string, now time.Time) []models.Event {
	var events []models.Event
	eventsByType := make(map[string]*models.Event)

	slog.Debug("1xbet: parsing sub-game markets", "match", matchName, "match_id", matchID, "event_type", eventType, "group_events_count", len(groupEvents))

	for i, ge := range groupEvents {
		slog.Debug("1xbet: processing group event", "match", matchName, "match_id", matchID, "event_type", eventType, "group_index", i, "group_id", ge.G, "event_arrays_count", len(ge.E))
		
		// Find main line (CE=1 or first non-empty)
		var mainEvents []Event
		for _, eventArray := range ge.E {
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
		if len(mainEvents) == 0 && len(ge.E) > 0 && len(ge.E[0]) > 0 {
			mainEvents = ge.E[0]
		}
		if len(mainEvents) == 0 {
			slog.Debug("1xbet: skipping group event (no main events)", "match", matchName, "match_id", matchID, "event_type", eventType, "group_id", ge.G)
			continue
		}

		eventID := fmt.Sprintf("%s_1xbet_%s", matchID, eventType)
		ev := getOrCreateEvent(eventsByType, eventID, matchID, eventType, now)

		slog.Debug("1xbet: parsing group event", "match", matchName, "match_id", matchID, "event_type", eventType, "group_id", ge.G, "main_events_count", len(mainEvents))

		// Parse based on group type
		switch ge.G {
		case 1:
			// 1X2 (Moneyline) - collect all events from all arrays
			for _, eventArray := range ge.E {
				for _, e := range eventArray {
					switch e.T {
					case 1:
						ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "home_win", "", e.C))
					case 2:
						ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "draw", "", e.C))
					case 3:
						ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "away_win", "", e.C))
					}
				}
			}
		case 2:
			// Handicap (Фора)
			parseStatisticalHandicap(ev, eventID, ge.E)
		case 8, 2854:
			// Double chance
			parseStatisticalDoubleChance(ev, eventID, ge.E)
		case 17:
			// Total (Over/Under)
			parseStatisticalTotals(ev, eventID, ge.E)
		default:
			// Try to detect totals by T values (9=over, 10=under)
			if hasTotalStructure(mainEvents) {
				parseStatisticalTotals(ev, eventID, ge.E)
			}
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

// parseStatisticalHandicap parses handicap markets for statistical events
func parseStatisticalHandicap(ev *models.Event, eventID string, eventArrays [][]Event) {
	if len(eventArrays) < 2 {
		return
	}

	homeArray := eventArrays[0]
	awayArray := eventArrays[1]

	homeMap := make(map[float64]float64)
	awayMap := make(map[float64]float64)
	homeSignMap := make(map[float64]float64)

	for _, e := range homeArray {
		if e.T == 7 {
			absP := e.P
			if e.P < 0 {
				absP = -e.P
			}
			homeMap[absP] = e.C
			homeSignMap[absP] = e.P
		}
	}

	for _, e := range awayArray {
		if e.T == 8 {
			absP := e.P
			if e.P < 0 {
				absP = -e.P
			}
			awayMap[absP] = e.C
		}
	}

	seenLines := make(map[float64]bool)
	allAbsP := make(map[float64]bool)
	for absP := range homeMap {
		allAbsP[absP] = true
	}
	for absP := range awayMap {
		allAbsP[absP] = true
	}

	for absP := range allAbsP {
		if seenLines[absP] {
			continue
		}

		homeOdds := homeMap[absP]
		awayOdds := awayMap[absP]

		line := absP
		if homeSign, ok := homeSignMap[absP]; ok {
			line = homeSign
		} else {
			line = -absP
		}

		if homeOdds > 0 {
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "handicap_home", formatSignedLine(line), homeOdds))
		}
		if awayOdds > 0 {
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "handicap_away", formatSignedLine(-line), awayOdds))
		}
		seenLines[absP] = true
	}
}

// parseStatisticalDoubleChance parses double chance markets for statistical events
func parseStatisticalDoubleChance(ev *models.Event, eventID string, eventArrays [][]Event) {
	for _, eventArray := range eventArrays {
		for _, e := range eventArray {
			switch e.T {
			case 4:
				ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "double_chance_1x", "", e.C))
			case 5:
				ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "double_chance_12", "", e.C))
			case 6:
				ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "double_chance_2x", "", e.C))
			}
		}
	}
	// Fallback: if not found by T values, try by position
	if len(ev.Outcomes) == 0 {
		for _, eventArray := range eventArrays {
			if len(eventArray) >= 3 {
				ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "double_chance_1x", "", eventArray[0].C))
				ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "double_chance_12", "", eventArray[1].C))
				ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "double_chance_2x", "", eventArray[2].C))
				break
			}
		}
	}
}

// parseStatisticalTotals parses total (over/under) markets for statistical events
func parseStatisticalTotals(ev *models.Event, eventID string, eventArrays [][]Event) {
	if len(eventArrays) < 2 {
		return
	}

	overArray := eventArrays[0]
	underArray := eventArrays[1]

	overMap := make(map[float64]float64)
	underMap := make(map[float64]float64)

	for _, e := range overArray {
		if e.T == 9 || e.T == 794 {
			overMap[e.P] = e.C
		}
	}

	for _, e := range underArray {
		if e.T == 10 || e.T == 795 {
			underMap[e.P] = e.C
		}
	}

	seenLines := make(map[float64]bool)
	for p, overOdds := range overMap {
		if underOdds, ok := underMap[p]; ok && !seenLines[p] {
			line := formatLine(p)
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_over", line, overOdds))
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_under", line, underOdds))
			seenLines[p] = true
		}
	}

	// Fallback: if arrays don't match by structure, try pairing events
	if len(seenLines) == 0 {
		seenLines = make(map[float64]bool)
		for i, eventArray := range eventArrays {
			for _, e := range eventArray {
				if e.P == 0 {
					continue
				}
				if seenLines[e.P] {
					continue
				}

				var overOdds, underOdds float64
				for _, e2 := range eventArray {
					if e2.P == e.P {
						if e2.T == 9 || e2.T == 794 {
							overOdds = e2.C
						}
						if e2.T == 10 || e2.T == 795 {
							underOdds = e2.C
						}
					}
				}

				if overOdds == 0 || underOdds == 0 {
					for j, otherArray := range eventArrays {
						if i == j {
							continue
						}
						for _, e2 := range otherArray {
							if e2.P == e.P {
								if e2.T == 9 || e2.T == 794 {
									overOdds = e2.C
								}
								if e2.T == 10 || e2.T == 795 {
									underOdds = e2.C
								}
							}
						}
					}
				}

				if overOdds > 0 && underOdds > 0 && !seenLines[e.P] {
					line := formatLine(e.P)
					ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_over", line, overOdds))
					ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_under", line, underOdds))
					seenLines[e.P] = true
				}
			}
		}
	}
}

// hasTotalStructure checks if events have total structure (T=9/10)
func hasTotalStructure(events []Event) bool {
	for _, e := range events {
		if e.T == 9 || e.T == 10 || e.T == 794 || e.T == 795 {
			return true
		}
	}
	return false
}

// parseGroupedEvents parses grouped events into standard event models
func parseGroupedEvents(matchID string, groupEvents []GroupEvent, subGames []SubGame) []models.Event {
	var events []models.Event
	now := time.Now()

	// Map to track events by type
	eventsByType := make(map[string]*models.Event)

	// Build mapping from SG.N to statistical event type
	// SG contains metadata about groups, including TG (title) which identifies statistical events
	sgStatsMap := make(map[int64]string) // Maps SG.N -> event type
	for _, sg := range subGames {
		if sg.TG == "" {
			continue
		}
		// Map Russian titles to standard event types
		switch sg.TG {
		case "Угловые":
			sgStatsMap[sg.N] = string(models.StandardEventCorners)
		case "Фолы":
			sgStatsMap[sg.N] = string(models.StandardEventFouls)
		case "Желтые карточки":
			sgStatsMap[sg.N] = string(models.StandardEventYellowCards)
		case "Офсайды":
			sgStatsMap[sg.N] = string(models.StandardEventOffsides)
		}
	}

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
		// Statistical markets (full events come only from GetGame per match, not from league list)
		case 100, 101:
			// Corners (G=100 or 101)
			parseStatisticalGroup(eventsByType, matchID, ge, now, string(models.StandardEventCorners))
		case 102:
			// Yellow cards
			parseStatisticalGroup(eventsByType, matchID, ge, now, string(models.StandardEventYellowCards))
		case 103:
			// Fouls
			parseStatisticalGroup(eventsByType, matchID, ge, now, string(models.StandardEventFouls))
		case 105:
			// Offsides
			parseStatisticalGroup(eventsByType, matchID, ge, now, string(models.StandardEventOffsides))
		default:
			// Check if this group is a statistical event via SG metadata
			// Since direct mapping doesn't work, if we have statistical SG items,
			// try to parse unknown groups as statistical if they have the right structure
			// Groups 11212, 11412, 8427, 8429 often appear and might be statistical
			// Try parsing them as statistical events if they have over/under or handicap patterns
			if len(sgStatsMap) > 0 {
				// Check if this group has statistical event structure
				// Look for over/under pattern: same P value but different T values (or P=0/None)
				hasStatsStructure := false
				for _, row := range ge.E {
					if len(row) >= 2 {
						// Check if first two events have same P (or both P=0/None) but different T
						p0 := row[0].P
						p1 := row[1].P
						t0 := row[0].T
						t1 := row[1].T
						if (p0 == p1 || (p0 == 0 && p1 == 0)) && t0 != t1 {
							hasStatsStructure = true
							break
						}
						// Also check for standard over/under patterns
						for _, e := range row {
							if e.T == 9 || e.T == 10 || e.T == 794 || e.T == 795 || e.T == 7 || e.T == 8 {
								hasStatsStructure = true
								break
							}
						}
						if hasStatsStructure {
							break
						}
					}
				}
				// If it has statistical structure and we have SG metadata, parse as statistical
				if hasStatsStructure {
					// Try to match by checking if any SG.N matches GE.GS or GE.G
					// If not, use first available statistical type from SG
					eventType := ""
					if sgStatsMap[int64(ge.GS)] != "" {
						eventType = sgStatsMap[int64(ge.GS)]
					} else if sgStatsMap[int64(ge.G)] != "" {
						eventType = sgStatsMap[int64(ge.G)]
					} else {
						// Use first available statistical type (prefer corners)
						for _, et := range []string{
							string(models.StandardEventCorners),
							string(models.StandardEventFouls),
							string(models.StandardEventYellowCards),
							string(models.StandardEventOffsides),
						} {
							for _, sgN := range sgStatsMap {
								if sgN == et {
									eventType = et
									break
								}
							}
							if eventType != "" {
								break
							}
						}
					}
					if eventType != "" {
						slog.Debug("1xbet: parsing unknown group as statistical", "group_id", ge.G, "group_sub_id", ge.GS, "event_type", eventType)
						parseStatisticalGroup(eventsByType, matchID, ge, now, eventType)
					} else {
						slog.Info("1xbet: skipping unknown group (has stats structure but no SG mapping)", "group_id", ge.G, "group_sub_id", ge.GS)
					}
				} else {
					// Skip unknown groups (log at Info level to see what groups are available)
					slog.Info("1xbet: skipping unknown group", "group_id", ge.G, "group_sub_id", ge.GS)
				}
			} else {
				// Skip unknown groups (log at Info level to see what groups are available)
				slog.Info("1xbet: skipping unknown group", "group_id", ge.G, "group_sub_id", ge.GS)
			}
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

// parseStatisticalGroup parses a statistical market group (corners, fouls, yellow cards, offsides).
// Full event list for these markets comes only from GetGame(matchID), not from the league matches list.
// Supports standard total (T=9 over, T=10 under) and handicap (T=7, T=8), plus alternative encodings (e.g. T=794/795).
func parseStatisticalGroup(eventsByType map[string]*models.Event, matchID string, ge GroupEvent, now time.Time, standardEventType string) {
	eventID := fmt.Sprintf("%s_1xbet_%s", matchID, standardEventType)
	ev := getOrCreateEvent(eventsByType, eventID, matchID, standardEventType, now)

	// Find main line (CE=1 or first non-empty)
	var mainEvents []Event
	for _, eventArray := range ge.E {
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
	if len(mainEvents) == 0 && len(ge.E) > 0 && len(ge.E[0]) > 0 {
		mainEvents = ge.E[0]
	}
	if len(mainEvents) == 0 {
		return
	}

	// Collect all lines (same P can appear in over/under pair); prefer standard T=9/10 and T=7/8
	seenLine := make(map[string]bool)
	for _, e := range mainEvents {
		line := formatLine(e.P)
		switch e.T {
		case 9:
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_over", line, e.C))
			seenLine[line] = true
		case 10:
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_under", line, e.C))
			seenLine[line] = true
		case 7:
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "handicap_home", formatSignedLine(e.P), e.C))
		case 8:
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "handicap_away", formatSignedLine(-e.P), e.C))
		}
	}
	// Alternative encoding: two outcomes (e.g. T=794/795) as over/under for one line
	if len(ev.Outcomes) == 0 && len(mainEvents) >= 2 {
		line := formatLine(mainEvents[0].P)
		if line == "0" && mainEvents[1].P != 0 {
			line = formatLine(mainEvents[1].P)
		}
		ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_over", line, mainEvents[0].C))
		ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_under", line, mainEvents[1].C))
	}
	// Handle non-standard T values: if we have two events with same P but different T, treat as over/under
	if len(ev.Outcomes) == 0 && len(mainEvents) >= 2 {
		// Check if first two events have same P (or both P=0/None) but different T
		p0 := mainEvents[0].P
		p1 := mainEvents[1].P
		t0 := mainEvents[0].T
		t1 := mainEvents[1].T
		if (p0 == p1 || (p0 == 0 && p1 == 0)) && t0 != t1 {
			// Treat as over/under pair
			line := formatLine(p0)
			if line == "0" && len(mainEvents) > 2 && mainEvents[2].P != 0 {
				line = formatLine(mainEvents[2].P)
			}
			// Use first event as over, second as under (arbitrary but consistent)
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_over", line, mainEvents[0].C))
			ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_under", line, mainEvents[1].C))
		}
	}
	// If multiple rows with different P (several totals), merge: each row can be over/under
	if len(ev.Outcomes) == 0 && len(ge.E) > 1 {
		for _, eventArray := range ge.E {
			if len(eventArray) < 2 {
				continue
			}
			line := formatLine(eventArray[0].P)
			if eventArray[0].P == 0 && eventArray[1].P != 0 {
				line = formatLine(eventArray[1].P)
			}
			for _, e := range eventArray {
				if e.T == 9 || e.T == 794 {
					ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_over", line, e.C))
					break
				}
			}
			for _, e := range eventArray {
				if e.T == 10 || e.T == 795 {
					ev.Outcomes = append(ev.Outcomes, newOutcome(eventID, "total_under", line, e.C))
					break
				}
			}
		}
	}
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
// Uses 1 decimal place for better readability (e.g., 6.5 instead of 6.500000)
func formatLine(p float64) string {
	return strconv.FormatFloat(p, 'f', 1, 64)
}

// formatSignedLine formats a signed line value
func formatSignedLine(p float64) string {
	if p == 0 {
		return "0"
	}
	if p > 0 {
		return "+" + strconv.FormatFloat(p, 'f', 1, 64)
	}
	return strconv.FormatFloat(p, 'f', 1, 64)
}
