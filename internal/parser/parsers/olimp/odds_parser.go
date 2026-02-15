package olimp

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

const bookmakerName = "Olimp"

// groupName to standard event type (corners, fouls, yellow_cards, offsides).
func statisticalEventType(groupName string) string {
	g := strings.ToLower(groupName)
	switch {
	case strings.Contains(g, "углов") || strings.Contains(g, "corner"):
		return string(models.StandardEventCorners)
	case strings.Contains(g, "фол") || strings.Contains(g, "нарушен") || strings.Contains(g, "foul"):
		return string(models.StandardEventFouls)
	case strings.Contains(g, "желт") || strings.Contains(g, "карточ") || strings.Contains(g, "yellow") || strings.Contains(g, "жк"):
		return string(models.StandardEventYellowCards)
	case strings.Contains(g, "офсайд") || strings.Contains(g, "offside"):
		return string(models.StandardEventOffsides)
	default:
		return ""
	}
}

// ParseEvent builds models.Match from OlimpEvent (full line from step 3: main, totals, handicaps, corners, fouls, yellow cards, offsides).
func ParseEvent(ev *OlimpEvent, leagueName string) *models.Match {
	if ev == nil {
		return nil
	}
	homeTeam := ev.Team1Name
	awayTeam := ev.Team2Name
	if homeTeam == "" || awayTeam == "" {
		if ev.Names != nil && ev.Names["2"] != "" {
			parts := strings.SplitN(ev.Names["2"], " - ", 2)
			if len(parts) == 2 {
				homeTeam, awayTeam = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			}
		}
		if homeTeam == "" || awayTeam == "" {
			slog.Debug("olimp: skip event (no team names)", "event_id", ev.ID)
			return nil
		}
	}
	startTime := time.Unix(ev.StartDateTime, 0).UTC()
	if startTime.Before(time.Now().UTC()) {
		slog.Debug("olimp: skip past match", "event_id", ev.ID)
		return nil
	}
	matchID := models.CanonicalMatchID(homeTeam, awayTeam, startTime)
	now := time.Now().UTC()
	match := &models.Match{
		ID:         matchID,
		Name:       fmt.Sprintf("%s vs %s", homeTeam, awayTeam),
		HomeTeam:   homeTeam,
		AwayTeam:   awayTeam,
		StartTime:  startTime,
		Sport:      "football",
		Tournament: leagueName,
		Bookmaker:  bookmakerName,
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	// Group outcomes: main (RESULT), totals/handicaps (main_match), statistical (corners, fouls, yellow cards, offsides)
	var mainOutcomes []models.Outcome
	totalsByParam := make(map[string][]models.Outcome)
	handicapsByParam := make(map[string][]models.Outcome)
	// statistical: eventType -> param -> outcomes (total over/under or exact)
	statisticalByType := make(map[string]map[string][]models.Outcome)
	for _, o := range ev.Outcomes {
		odds, _ := strconv.ParseFloat(o.Probability, 64)
		if odds <= 0 {
			continue
		}
		out := models.Outcome{
			ID:        o.ID,
			EventID:   matchID + "_main",
			Parameter: o.Param,
			Odds:      odds,
			Bookmaker: bookmakerName,
			CreatedAt: now,
			UpdatedAt: now,
		}
		statType := statisticalEventType(o.GroupName)
		if statType != "" {
			// Угловые, фолы, ЖК, офсайды — тоталы (больше/меньше) или точное число
			if statisticalByType[statType] == nil {
				statisticalByType[statType] = make(map[string][]models.Outcome)
			}
			param := o.Param
			if param == "" {
				param = "0"
			}
			lowerName := strings.ToLower(o.UnprocessedName)
			if strings.Contains(o.ShortName, "Б") || strings.Contains(lowerName, "бол") {
				out.OutcomeType = string(models.OutcomeTypeTotalOver)
				statisticalByType[statType][param] = append(statisticalByType[statType][param], out)
			} else if strings.Contains(o.ShortName, "М") || strings.Contains(lowerName, "мен") {
				out.OutcomeType = string(models.OutcomeTypeTotalUnder)
				statisticalByType[statType][param] = append(statisticalByType[statType][param], out)
			} else {
				out.OutcomeType = string(models.OutcomeTypeExactCount)
				statisticalByType[statType]["_"] = append(statisticalByType[statType]["_"], out)
			}
			continue
		}
		switch o.TableType {
		case "RESULT":
			switch o.ShortName {
			case "П1", "1":
				out.OutcomeType = string(models.OutcomeTypeHomeWin)
				mainOutcomes = append(mainOutcomes, out)
			case "Х", "X":
				out.OutcomeType = string(models.OutcomeTypeDraw)
				mainOutcomes = append(mainOutcomes, out)
			case "П2", "2":
				out.OutcomeType = string(models.OutcomeTypeAwayWin)
				mainOutcomes = append(mainOutcomes, out)
			}
		case "TOTAL":
			param := o.Param
			if param == "" {
				param = "0"
			}
			lowerName := strings.ToLower(o.UnprocessedName)
			if strings.Contains(o.ShortName, "Б") || strings.Contains(lowerName, "бол") {
				out.OutcomeType = string(models.OutcomeTypeTotalOver)
				totalsByParam[param] = append(totalsByParam[param], out)
			} else if strings.Contains(o.ShortName, "М") || strings.Contains(lowerName, "мен") {
				out.OutcomeType = string(models.OutcomeTypeTotalUnder)
				totalsByParam[param] = append(totalsByParam[param], out)
			}
		case "HANDICAP":
			out.OutcomeType = string(models.OutcomeTypeExactCount)
			handicapsByParam[o.Param] = append(handicapsByParam[o.Param], out)
		}
	}
	if len(mainOutcomes) >= 3 {
		match.Events = append(match.Events, models.Event{
			ID:         matchID + "_main",
			MatchID:    matchID,
			EventType:  string(models.StandardEventMainMatch),
			MarketName: models.GetMarketName(models.StandardEventMainMatch),
			Bookmaker:  bookmakerName,
			Outcomes:   mainOutcomes,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	for param, outcomes := range totalsByParam {
		if len(outcomes) < 2 {
			continue
		}
		evID := matchID + "_total_" + param
		match.Events = append(match.Events, models.Event{
			ID:         evID,
			MatchID:    matchID,
			EventType:  string(models.StandardEventMainMatch),
			MarketName: "Total " + param,
			Bookmaker:  bookmakerName,
			Outcomes:   outcomes,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	for param, outcomes := range handicapsByParam {
		if len(outcomes) < 2 {
			continue
		}
		evID := matchID + "_handicap_" + param
		match.Events = append(match.Events, models.Event{
			ID:         evID,
			MatchID:    matchID,
			EventType:  string(models.StandardEventMainMatch),
			MarketName: "Handicap " + param,
			Bookmaker:  bookmakerName,
			Outcomes:   outcomes,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	// Статистика: угловые, фолы, ЖК, офсайды
	for eventType, byParam := range statisticalByType {
		baseName := models.GetMarketName(models.StandardEventType(eventType))
		for param, outcomes := range byParam {
			if len(outcomes) == 0 {
				continue
			}
			evID := matchID + "_" + eventType
			displayName := baseName
			if param != "_" {
				evID += "_" + param
				displayName = baseName + " " + param
			}
			for i := range outcomes {
				outcomes[i].EventID = evID
			}
			match.Events = append(match.Events, models.Event{
				ID:         evID,
				MatchID:    matchID,
				EventType:  eventType,
				MarketName: displayName,
				Bookmaker:  bookmakerName,
				Outcomes:   outcomes,
				CreatedAt:  now,
				UpdatedAt:  now,
			})
		}
	}
	if len(match.Events) == 0 {
		slog.Debug("olimp: match has no events", "match", match.Name, "event_id", ev.ID)
		return nil
	}
	return match
}
