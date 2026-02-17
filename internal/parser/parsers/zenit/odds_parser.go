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
// API returns camelCase / no spaces (e.g. "Тоталы", "Форы", "Нарушения"); we support both.
var tableIDToEventType = map[string]string{
	"Угловые":            string(models.StandardEventCorners),
	"УгловыеМатч":        string(models.StandardEventCorners),
	"Фолы":               string(models.StandardEventFouls),
	"Нарушения":          string(models.StandardEventFouls),
	"Желтые карточки":    string(models.StandardEventYellowCards),
	"ЖелтыеКарточки":     string(models.StandardEventYellowCards),
	"Офсайды":            string(models.StandardEventOffsides),
	"Удары в створ":      string(models.StandardEventShotsOnTarget),
	"УдарыВСтвор":        string(models.StandardEventShotsOnTarget),
	"Тоталы":             string(models.StandardEventMainMatch),
	"ТоталМатча":         string(models.StandardEventMainMatch),
	"Форы":               string(models.StandardEventMainMatch),
	"Штанги/перекладины": "",
	"Замены":             "",
	"Видеопросмотры":     "",
	"Игроки":             "",
	"Сэйвы":              "",
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

	// Extended markets from t_b (totals, handicaps, corners, fouls, etc.)
	tbBlock, hasTB := resp.TB[gameIDStr]
	if hasTB && tbBlock.Data.Data != nil {
		tbEvents, mainOutcomes := parseTBBlock(matchID, &tbBlock)
		if len(mainOutcomes) > 0 && len(match.Events) > 0 {
			match.Events[0].Outcomes = append(match.Events[0].Outcomes, mainOutcomes...)
		}
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

// oddRow holds one odds row extracted from t_b tree (O, T from API distinguish over/under or handicap side).
type oddRow struct {
	ID     string
	OddKey string
	O      string
	T      string
	Odds   float64
}

// parseTBBlock walks t_b[gameID].data.data (tid -> block) and collects events by tableID.
// Returns (other events, main_match outcomes to merge into the first event).
func parseTBBlock(matchID string, block *TBBlock) (events []models.Event, mainMatchOutcomes []models.Outcome) {
	if block == nil || block.Data.Data == nil {
		return nil, nil
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

	now := time.Now()
	for tableID, rows := range byTable {
		if len(rows) == 0 {
			continue
		}
		eventType := tableIDToEventType[tableID]
		if eventType == "" {
			continue
		}
		var outcomes []models.Outcome
		
		// Group outcomes by parameter for totals/handicaps when T="cf"
		// When T="cf", we need to infer over/under from pairs of outcomes with same param
		byParam := make(map[string][]oddRow)
		for _, r := range rows {
			if r.Odds <= 0 {
				continue
			}
			param := parseParamFromOddKey(r.OddKey)
			byParam[param] = append(byParam[param], r)
		}
		
		// Process outcomes: for totals with T="cf", infer type from pairs
		for param, paramRows := range byParam {
			// Check if this tableID should have totals (over/under pairs)
			// Only for main totals, not individual team totals (ИндТоталы)
			isMainTotalMarket := (tableID == "Тоталы" || tableID == "ТоталМатча") ||
				eventType == string(models.StandardEventCorners) ||
				eventType == string(models.StandardEventFouls) ||
				eventType == string(models.StandardEventYellowCards) ||
				eventType == string(models.StandardEventOffsides)
			
			// For totals with T="cf": if exactly 2 outcomes with same param, treat as over/under pair
			if isMainTotalMarket && len(paramRows) == 2 && paramRows[0].T == "cf" && paramRows[1].T == "cf" {
				// Two outcomes with same param and T="cf" - likely over/under pair
				// For Zenit API: when T="cf", outcomes come in pairs for totals
				// Based on data analysis: order in array determines over/under
				// First outcome = over, second = under (verified with real data)
				r1, r2 := paramRows[0], paramRows[1]
				evID := matchID + "_main"
				if eventType != string(models.StandardEventMainMatch) {
					evID = matchID + "_" + tableID
				}
				// Use order: first = over, second = under
				outcomes = append(outcomes, models.Outcome{
					ID:          r1.ID,
					EventID:     evID,
					OutcomeType: string(models.OutcomeTypeTotalOver),
					Parameter:   param,
					Odds:        r1.Odds,
					Bookmaker:   bookmakerName,
					CreatedAt:   now,
					UpdatedAt:   now,
				})
				outcomes = append(outcomes, models.Outcome{
					ID:          r2.ID,
					EventID:     evID,
					OutcomeType: string(models.OutcomeTypeTotalUnder),
					Parameter:   param,
					Odds:        r2.Odds,
					Bookmaker:   bookmakerName,
					CreatedAt:   now,
					UpdatedAt:   now,
				})
			} else {
				// Single outcome or not a total market - use standard inference
				for _, r := range paramRows {
					param := parseParamFromOddKey(r.OddKey)
					outcomeType := InferOutcomeType(r.OddKey, param, tableID, r.O, r.T)
					evID := matchID + "_main"
					if eventType != string(models.StandardEventMainMatch) {
						evID = matchID + "_" + tableID
					}
					outcomes = append(outcomes, models.Outcome{
						ID:          r.ID,
						EventID:     evID,
						OutcomeType: outcomeType,
						Parameter:   param,
						Odds:        r.Odds,
						Bookmaker:   bookmakerName,
						CreatedAt:   now,
						UpdatedAt:   now,
					})
				}
			}
		}
		if len(outcomes) == 0 {
			continue
		}
		if eventType == string(models.StandardEventMainMatch) {
			mainMatchOutcomes = append(mainMatchOutcomes, outcomes...)
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
	return events, mainMatchOutcomes
}

func collectOddsFromCh(ch []TBChNode, tableID string, byTable map[string][]oddRow) {
	for _, node := range ch {
		if node.ID != "" && node.OddKey != "" {
			odds := flToFloat(node.H)
			if odds > 0 {
				byTable[tableID] = append(byTable[tableID], oddRow{
					ID: node.ID, OddKey: node.OddKey, O: node.O, T: node.T, Odds: odds,
				})
			}
		}
		if len(node.Ch) > 0 {
			collectOddsFromCh(node.Ch, tableID, byTable)
		}
	}
}

// ParseParamFromOddKey extracts parameter from oddKey e.g. "22790570|11|2" -> "2", "22790570|9|-3.5" -> "-3.5".
// Exported for debug/test scripts.
func ParseParamFromOddKey(oddKey string) string {
	parts := strings.Split(oddKey, "|")
	if len(parts) >= 3 {
		return parts[2]
	}
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func parseParamFromOddKey(oddKey string) string {
	return ParseParamFromOddKey(oddKey)
}

// InferOutcomeType maps Zenit oddKey+param+tableID+O+T to standard outcome type.
// tableID: "Тоталы", "ТоталМатча" = totals; "Форы" = handicaps (exact_count); corners/fouls/cards = statistical totals.
// O and T are API outcome codes: typically "1" = over / home, "2" = under / away (or "9"/"10" in some APIs).
// Exported for debug/test scripts.
func InferOutcomeType(oddKey, param, tableID, o, t string) string {
	parts := strings.Split(oddKey, "|")
	if len(parts) < 2 {
		return string(models.OutcomeTypeExactCount)
	}
	if param == "" {
		return string(models.OutcomeTypeExactCount)
	}
	code := o
	if code == "" {
		code = t
	}
	// Totals (main match total goals): O/T "1" = under, "2" = over (Zenit API convention - reversed)
	switch tableID {
	case "Тоталы", "ТоталМатча":
		if code == "1" || code == "9" {
			return string(models.OutcomeTypeTotalUnder)
		}
		if code == "2" || code == "10" {
			return string(models.OutcomeTypeTotalOver)
		}
		return string(models.OutcomeTypeExactCount)
	case "Форы":
		// Handicap: one outcome per line, parameter is the line; we keep exact_count (no handicap_home/away in models).
		return string(models.OutcomeTypeExactCount)
	default:
		// Statistical (corners, fouls, yellow cards, etc.): same convention, 1=under, 2=over (reversed)
		if code == "1" || code == "9" {
			return string(models.OutcomeTypeTotalUnder)
		}
		if code == "2" || code == "10" {
			return string(models.OutcomeTypeTotalOver)
		}
		return string(models.OutcomeTypeExactCount)
	}
}

// DebugOddRow holds one odds row from t_b with raw API fields (OddKey, O, T) for debugging.
type DebugOddRow struct {
	TableID  string
	OddKey   string
	Param    string
	O        string // API outcome code, may indicate over/under (e.g. "1"/"2")
	T        string
	Odds     float64
	Inferred string // InferOutcomeType(OddKey, Param)
}

// DumpTBOddRows walks t_b block and returns all odds rows with tableID, oddKey, O, T, param and inferred outcome type.
// Used by cmd/zenit-parse-test to inspect why everything becomes Exact Count.
func DumpTBOddRows(block *TBBlock) []DebugOddRow {
	if block == nil || block.Data.Data == nil {
		return nil
	}
	var out []DebugOddRow
	for tidStr, tidBlock := range block.Data.Data {
		tableID := "tid_" + tidStr
		if tidBlock.Data != nil && tidBlock.Data.TableID != "" {
			tableID = tidBlock.Data.TableID
		}
		collectDebugOddsFromCh(tidBlock.Ch, tableID, &out)
	}
	for i := range out {
		out[i].Param = ParseParamFromOddKey(out[i].OddKey)
		out[i].Inferred = InferOutcomeType(out[i].OddKey, out[i].Param, out[i].TableID, out[i].O, out[i].T)
	}
	return out
}

func collectDebugOddsFromCh(ch []TBChNode, tableID string, out *[]DebugOddRow) {
	for _, node := range ch {
		if node.ID != "" && node.OddKey != "" {
			odds := flToFloat(node.H)
			if odds > 0 {
				*out = append(*out, DebugOddRow{
					TableID: tableID,
					OddKey:  node.OddKey,
					O:       node.O,
					T:       node.T,
					Odds:    odds,
				})
			}
		}
		if len(node.Ch) > 0 {
			collectDebugOddsFromCh(node.Ch, tableID, out)
		}
	}
}

// TBTableSummary is used by DumpTBBlockForDebug.
type TBTableSummary struct {
	TID      string
	TableID  string
	OddsCnt  int
	MappedTo string // standard event type if tableID is in tableIDToEventType
}

// DumpTBBlockForDebug walks t_b block and returns per-tid summary (tableID, odds count, mapping).
// Used by cmd/debug-zenit-tb to find why extended markets are missing.
func DumpTBBlockForDebug(block *TBBlock) []TBTableSummary {
	if block == nil || block.Data.Data == nil {
		return nil
	}
	var out []TBTableSummary
	for tidStr, tidBlock := range block.Data.Data {
		tableID := "tid_" + tidStr
		if tidBlock.Data != nil && tidBlock.Data.TableID != "" {
			tableID = tidBlock.Data.TableID
		}
		cnt := countOddsInCh(tidBlock.Ch)
		mapped := tableIDToEventType[tableID]
		out = append(out, TBTableSummary{TID: tidStr, TableID: tableID, OddsCnt: cnt, MappedTo: mapped})
	}
	return out
}

func countOddsInCh(ch []TBChNode) int {
	n := 0
	for _, node := range ch {
		if node.ID != "" && node.OddKey != "" && flToFloat(node.H) > 0 {
			n++
		}
		n += countOddsInCh(node.Ch)
	}
	return n
}
