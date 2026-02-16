// Тестовый скрипт для отладки парсинга Zenit: загружает один матч, выводит сырые oddKey/O/T из t_b
// и результат текущей логики (везде Exact Count?). Запуск из корня репо:
//
//	go run ./cmd/zenit-parse-test
//	go run ./cmd/zenit-parse-test -config configs/production.yaml -save
//	go run ./cmd/zenit-parse-test -from zenit_match_raw.json   # разбор без сети
//
// Флаг -save сохраняет сырой JSON ответа матча в zenit_match_raw.json.
// Флаг -from=file разбирает уже сохранённый JSON (не требует сеть и imprint_hash).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers/zenit"
	pkgconfig "github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

func main() {
	configPath := flag.String("config", "configs/production.yaml", "path to config yaml")
	saveRaw := flag.Bool("save", false, "save raw match JSON to zenit_match_raw.json")
	fromFile := flag.String("from", "", "parse from saved JSON file (no network)")
	flag.Parse()

	if err := run(*configPath, *saveRaw, *fromFile); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(configPath string, saveRaw bool, fromFile string) error {
	var matchResp *zenit.LineResponse
	var gameID int

	if fromFile != "" {
		// Режим офлайн: загрузить JSON и извлечь gameID из первого ключа в games
		data, err := os.ReadFile(fromFile)
		if err != nil {
			return fmt.Errorf("read %s: %w", fromFile, err)
		}
		matchResp = &zenit.LineResponse{}
		if err := json.Unmarshal(data, matchResp); err != nil {
			return fmt.Errorf("parse JSON: %w", err)
		}
		for k := range matchResp.Games {
			gameID, _ = strconv.Atoi(k)
			break
		}
		if gameID == 0 {
			return fmt.Errorf("no games in JSON")
		}
		fmt.Println("=== Zenit parse test (from saved JSON) ===")
		fmt.Printf("Loaded from %s, gameID=%d\n\n", fromFile, gameID)
	} else {
		cfg, err := pkgconfig.Load(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		z := &cfg.Parser.Zenit
		if z.ImprintHash == "" {
			return fmt.Errorf("parser.zenit.imprint_hash is required (get from browser DevTools)")
		}
		timeout := z.Timeout
		if timeout <= 0 {
			timeout = cfg.Parser.Timeout
		}
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		client := zenit.NewClient(z.BaseURL, z.ImprintHash, z.FrontVersion, z.SportID, timeout, z.ProxyList)
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()

		fmt.Println("=== Zenit parse test: fetch one match and dump t_b oddKeys ===")
		fmt.Println()

		page, err := client.GetLinePage(ctx, 0)
		if err != nil {
			return fmt.Errorf("GetLinePage: %w", err)
		}
		var rid, tid, lid int
		for _, league := range page.League {
			for _, gid := range league.Games {
				gameID = gid
				lid = league.ID
				rid = league.Rid
				tid = league.Tid
				break
			}
			if gameID != 0 {
				break
			}
		}
		if gameID == 0 {
			return fmt.Errorf("no games on first page")
		}
		fmt.Printf("First game on page: gameID=%d, lid=%d, rid=%d, tid=%d\n", gameID, lid, rid, tid)

		matchResp, err = client.GetMatch(ctx, rid, tid, lid, gameID)
		if err != nil {
			return fmt.Errorf("GetMatch: %w", err)
		}
		if saveRaw {
			raw, _ := json.MarshalIndent(matchResp, "", "  ")
			if err := os.WriteFile("zenit_match_raw.json", raw, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not save raw JSON: %v\n", err)
			} else {
				fmt.Println("Saved zenit_match_raw.json")
			}
		}
	}

	gameIDStr := strconv.Itoa(gameID)
	game, ok := matchResp.Games[gameIDStr]
	if !ok {
		for k, g := range matchResp.Games {
			gameIDStr = k
			game = g
			break
		}
	}
	homeTeam := getTeamName(&matchResp.Dict, game.C1ID)
	awayTeam := getTeamName(&matchResp.Dict, game.C2ID)
	fmt.Printf("Match: %s vs %s (start %s)\n\n", homeTeam, awayTeam, time.Unix(game.Time, 0).UTC().Format(time.RFC3339))

	// 3) Дамп всех исходов из t_b с oddKey, O, T, param и текущим InferOutcomeType
	tbBlock, hasTB := matchResp.TB[gameIDStr]
	if !hasTB || tbBlock.Data.Data == nil {
		fmt.Println("No t_b block for this game.")
	} else {
		rows := zenit.DumpTBOddRows(&tbBlock)
		fmt.Printf("--- t_b: %d odds rows ---\n", len(rows))
		fmt.Println("TableID           | OddKey              | Param   | O   | T   | Odds   | Inferred")
		fmt.Println(strings.Repeat("-", 95))
		byTable := make(map[string]int)
		for _, r := range rows {
			byTable[r.TableID]++
			fmt.Printf("%-18s | %-19s | %-7s | %-3s | %-3s | %6.2f | %s\n",
				trunc(r.TableID, 18), trunc(r.OddKey, 19), r.Param, r.O, r.T, r.Odds, r.Inferred)
		}
		fmt.Println()
		fmt.Println("By tableID (count):")
		for id, cnt := range byTable {
			fmt.Printf("  %s: %d\n", id, cnt)
		}
	}

	// 4) Повторяем текущую логику парсера: ParseMatch и печатаем события/исходы
	match := zenit.ParseMatch(matchResp, gameID)
	if match == nil {
		fmt.Println("\nParseMatch returned nil (no events?).")
		return nil
	}
	fmt.Println("\n--- ParseMatch result (events and outcome_type) ---")
	for _, ev := range match.Events {
		fmt.Printf("\nEvent: %s  event_type=%s  market_name=%s\n", ev.ID, ev.EventType, ev.MarketName)
		for _, o := range ev.Outcomes {
			name := models.GetOutcomeTypeName(models.StandardOutcomeType(o.OutcomeType))
			fmt.Printf("  outcome_type=%s (%s)  parameter=%q  odds=%.2f\n", o.OutcomeType, name, o.Parameter, o.Odds)
		}
	}

	// 5) Сводка: сколько исходов с типом exact_count
	var exactCount int
	for _, ev := range match.Events {
		for _, o := range ev.Outcomes {
			if o.OutcomeType == string(models.OutcomeTypeExactCount) {
				exactCount++
			}
		}
	}
	fmt.Printf("\n--- Summary: %d outcomes are Exact Count (of %d total in main_match–related)\n",
		exactCount, countMainOutcomes(match))
	return nil
}

func getTeamName(d *zenit.Dict, cmdID int) string {
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

func trunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}

func countMainOutcomes(m *models.Match) int {
	n := 0
	for _, ev := range m.Events {
		if ev.EventType == string(models.StandardEventMainMatch) {
			n += len(ev.Outcomes)
		}
	}
	return n
}
