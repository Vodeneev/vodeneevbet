package zenit

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

const bookmakerName = "Zenit"

// tableIDToEventType maps Zenit tableID (or category name) to standard event type.
// Russian names from API: Угловые, Фолы, Желтые карточки, Офсайды, Удары в створ.
var tableIDToEventType = map[string]string{
	"Угловые":           string(models.StandardEventCorners),
	"УгловыеМатч":       string(models.StandardEventCorners),
	"Фолы":              string(models.StandardEventFouls),
	"Желтые карточки":   string(models.StandardEventYellowCards),
	"ЖелтыеКарточки":    string(models.StandardEventYellowCards),
	"Офсайды":           string(models.StandardEventOffsides),
	"Удары в створ":     string(models.StandardEventShotsOnTarget),
	"УдарыВСтвор":       string(models.StandardEventShotsOnTarget),
	"Штанги/перекладины": "",
	"Замены":            "",
	"Видеопросмотры":   "",
	"Игроки":            "",
	"Сэйвы":             "",
}

// ParseMatch builds models.Match from a single-match LineResponse (game + dict + t_b).
// Response must contain exactly one game (the requested match) and its t_b block.
func ParseMatch(resp *LineResponse, gameID int) *models.Match {
	if resp == nil || len(resp.Games) == 0 {
		return nil
	}
	gameIDStr := strconv.Itoa(gameID)
	game, ok := resp.Games[gameIDStr]
	if !ok {
		for k, g := range resp.Games {
			gameIDStr = k
			game = g
			break
		}
	}

	homeTeam := getTeamName(&resp.Dict, game.C1ID)
	awayTeam := getTeamName(&resp.Dict, game.C2ID)
	if homeTeam == "" || awayTeam == "" {
		slog.Debug("zenit: skip match (no team names)", "game_id", gameIDStr, "c1_id", game.C1ID, "c2_id", game.C2ID)
		return nil
	}

	startTime := time.Unix(game.Time, 0).UTC()
	if startTime.Before(time.Now().UTC()) {
		slog.Debug("zenit: skip past match", "game_id", gameIDStr, "start", startTime.Format(time.RFC3339))
		return nil
	}

	leagueName := getLeagueName(&resp.Dict, game.Lid)
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

	// Main line from f_l: outcome 1, X, 2 (o "1", "2", "3")
	mainEvent := parseMainLineFromFL(matchID, game.FL)
	if mainEvent != nil {
		match.Events = append(match.Events, *mainEvent)
	}

	// Extended markets from t_b
	tbBlock, hasTB := resp.TB[gameIDStr]
	if hasTB && tbBlock.Data.Data != nil {
		tbEvents := parseTBBlock(matchID, &tbBlock)
		match.Events = append(match.Events, tbEvents...)
	}

	if len(match.Events) == 0 {
		slog.Debug("zenit: match has no events", "match", match.Name, "game_id", gameIDStr)
		return nil
	}

	return match
}

func getTeamName(d *Dict, cmdID int) string {
	if d == nil {
		return ""
	}
	idStr := strconv.Itoa(cmdID)
	if d.Eng != nil && d.Eng.Cmd != nil {
		if n := d.Eng.Cmd[idStr]; n != "" {
			return n
		}
	}
	if d.Cmd != nil {
		return d.Cmd[idStr]
	}
	return ""
}

func getLeagueName(d *Dict, lid int) string {
	if d == nil {
		return ""
	}
	idStr := strconv.Itoa(lid)
	if d.Eng != nil && d.Eng.League != nil {
		if n := d.Eng.League[idStr]; n != "" {
			return n
		}
	}
	if d.League != nil {
		return d.League[idStr]
	}
	return ""
}

func parseMainLineFromFL(matchID string, fl []FLItem) *models.Event {
	var outcomes []models.Outcome
	now := time.Now()
	for _, item := range fl {
		if item.O == "" || item.ID == "" {
			continue
		}
		odds := flToFloat(item.H)
		if odds <= 0 {
			continue
		}
		var outcomeType string
		switch item.O {
		case "1":
			outcomeType = string(models.OutcomeTypeHomeWin)
		case "2":
			outcomeType = string(models.OutcomeTypeDraw)
		case "3":
			outcomeType = string(models.OutcomeTypeAwayWin)
		default:
			continue
		}
		outcomes = append(outcomes, models.Outcome{
			ID:          item.ID,
			EventID:     matchID + "_main",
			OutcomeType: outcomeType,
			Parameter:   "",
			Odds:        odds,
			Bookmaker:   bookmakerName,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	if len(outcomes) == 0 {
		return nil
	}
	return &models.Event{
		ID:         matchID + "_main",
		MatchID:    matchID,
		EventType:  string(models.StandardEventMainMatch),
		MarketName: models.GetMarketName(models.StandardEventMainMatch),
		Bookmaker:  bookmakerName,
		Outcomes:   outcomes,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func flToFloat(h interface{}) float64 {
	switch v := h.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

// oddRow holds one odds row extracted from t_b tree
type oddRow struct {
	ID     string
	OddKey string
	Odds   float64
}

// parseTBBlock walks t_b[gameID].data.data (tid -> block) and collects events by tableID.
func parseTBBlock(matchID string, block *TBBlock) []models.Event {
	if block == nil || block.Data.Data == nil {
		return nil
	}
	byTable := make(map[string][]oddRow)

	for tidStr, tidBlock := range block.Data.Data {
		_ = tidStr
		tableID := ""
		if tidBlock.Data != nil {
			tableID = tidBlock.Data.TableID
		}
		if tableID == "" {
			tableID = "tid_" + tidStr
		}
		collectOddsFromCh(tidBlock.Ch, tableID, byTable)
	}

	var events []models.Event
	now := time.Now()
	for tableID, rows := range byTable {
		if len(rows) == 0 {
			continue
		}
		eventType := tableIDToEventType[tableID]
		if eventType == "" {
			// Skip unsupported markets (or add as generic later)
			continue
		}
		var outcomes []models.Outcome
		for _, r := range rows {
			if r.Odds <= 0 {
				continue
			}
			param := parseParamFromOddKey(r.OddKey)
			outcomeType := inferOutcomeType(r.OddKey, param)
			outcomes = append(outcomes, models.Outcome{
				ID:          r.ID,
				EventID:     matchID + "_" + tableID,
				OutcomeType: outcomeType,
				Parameter:   param,
				Odds:        r.Odds,
				Bookmaker:   bookmakerName,
				CreatedAt:   now,
				UpdatedAt:   now,
			})
		}
		if len(outcomes) == 0 {
			continue
		}
		marketName := models.GetMarketName(models.StandardEventType(eventType))
		if marketName == "Unknown Market" {
			marketName = tableID
		}
		events = append(events, models.Event{
			ID:         matchID + "_" + tableID,
			MatchID:    matchID,
			EventType:  eventType,
			MarketName: marketName,
			Bookmaker:  bookmakerName,
			Outcomes:   outcomes,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	return events
}

func collectOddsFromCh(ch []TBChNode, tableID string, byTable map[string][]oddRow) {
	for _, node := range ch {
		if node.ID != "" && node.OddKey != "" {
			odds := flToFloat(node.H)
			if odds > 0 {
				byTable[tableID] = append(byTable[tableID], oddRow{ID: node.ID, OddKey: node.OddKey, Odds: odds})
			}
		}
		if len(node.Ch) > 0 {
			collectOddsFromCh(node.Ch, tableID, byTable)
		}
	}
}

// parseParamFromOddKey extracts parameter from oddKey e.g. "22790570|11|2" -> "2.5", "22790570|9|-3.5" -> "-3.5"
func parseParamFromOddKey(oddKey string) string {
	parts := strings.Split(oddKey, "|")
	if len(parts) >= 3 {
		return parts[2]
	}
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func inferOutcomeType(oddKey, param string) string {
	parts := strings.Split(oddKey, "|")
	if len(parts) < 2 {
		return string(models.OutcomeTypeExactCount)
	}
	// Zenit: second part often market id; third part param (e.g. 2 for total 2.5, -3.5 for handicap)
	if param == "" {
		return string(models.OutcomeTypeExactCount)
	}
	if strings.Contains(param, ".") || len(parts) >= 3 {
		// Total or handicap: alternate over/under or home/away by position in list; we don't have position here
		return string(models.OutcomeTypeExactCount)
	}
	return string(models.OutcomeTypeExactCount)
}
