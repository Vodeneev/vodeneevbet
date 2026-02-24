// Тестовый скрипт для изучения структуры ответов API по киберспорту (Fonbet, xbet).
// Запуск:
//
//	go run ./cmd/test-esports [flags]
//
// Флаги:
//   -fonbet     проверить Fonbet (sportCategoryId 19=Dota2, 20=CS)
//   -xbet       проверить xbet (sports=40 — киберспорт)
//   -save       сохранить сырой JSON в ./test_esports_*.json
//   -xbet-url   базовый URL xbet (если не задан, используется mirror resolve)
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers/fonbet"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers/xbet1"
)

const (
	fonbetBaseURL = "https://line55w.bk6bba-resources.com/events/list"
	fonbetLang    = "en"
	fonbetVersion = "71181399506"
	scopeMarket   = "1600" // общий scope (футбол), для киберспорта тот же URL — фильтр по sportCategoryId в ответе
	userAgent     = "ValueBetBot/1.0 (https://github.com/Vodeneev/vodeneevbet)"
)

func main() {
	doFonbet := flag.Bool("fonbet", true, "test Fonbet API (sportCategoryId 19, 20)")
	doXbet := flag.Bool("xbet", true, "test xbet API (sports=40)")
	saveRaw := flag.Bool("save", false, "save raw JSON to ./test_esports_*.json")
	xbetBaseURL := flag.String("xbet-url", "", "xbet base URL (e.g. https://1xlite-6173396.bar); if empty, resolve mirror")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if *doFonbet {
		fmt.Println("=== Fonbet: events/list (sports 19=Dota2, 20=CS) ===")
		if err := testFonbet(ctx, *saveRaw); err != nil {
			fmt.Fprintf(os.Stderr, "Fonbet: %v\n", err)
			os.Exit(1)
		}
	}

	if *doXbet {
		fmt.Println("\n=== xbet: GetChamps(40) + Get1x2_VZip (sports=40) ===")
		if err := testXbet(ctx, *xbetBaseURL, *saveRaw); err != nil {
			fmt.Fprintf(os.Stderr, "xbet: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("\nDone.")
}

func testFonbet(ctx context.Context, save bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fonbetBaseURL, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	q.Set("lang", fonbetLang)
	q.Set("version", fonbetVersion)
	q.Set("scopeMarket", scopeMarket)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Encoding", "gzip")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	var body []byte
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return err
		}
		defer gz.Close()
		body, err = io.ReadAll(gz)
	} else {
		body, err = io.ReadAll(resp.Body)
	}
	if err != nil {
		return err
	}

	if save {
		if err := os.WriteFile("test_esports_fonbet_list.json", body, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "save fonbet: %v\n", err)
		} else {
			fmt.Println("saved test_esports_fonbet_list.json")
		}
	}

	var apiResp fonbet.FonbetAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// Вывести все sports с id 19, 20 или sportCategoryId 19, 20
	fmt.Printf("Sports total: %d\n", len(apiResp.Sports))
	for _, s := range apiResp.Sports {
		if s.ID == 19 || s.ID == 20 || s.SportCategoryID == 19 || s.SportCategoryID == 20 {
			fmt.Printf("  id=%d kind=%q name=%q alias=%q sportCategoryId=%d\n",
				s.ID, s.Kind, s.Name, s.Alias, s.SportCategoryID)
		}
	}
	// Все топ-уровни (kind=sport) для справки по alias
	fmt.Println("Top-level sports (kind=sport) with id 1..25:")
	for _, s := range apiResp.Sports {
		if s.Kind == "sport" && s.ID <= 25 {
			fmt.Printf("  id=%d alias=%q name=%q\n", s.ID, s.Alias, s.Name)
		}
	}

	// События с sportId 19 или 20
	var events19, events20 []fonbet.FonbetAPIEvent
	for _, e := range apiResp.Events {
		if e.SportID == 19 {
			events19 = append(events19, e)
		}
		if e.SportID == 20 {
			events20 = append(events20, e)
		}
	}
	fmt.Printf("\nEvents with sportId=19 (Dota2): %d\n", len(events19))
	for i, e := range events19 {
		if i >= 5 {
			fmt.Printf("  ... and %d more\n", len(events19)-5)
			break
		}
		fmt.Printf("  id=%d level=%d name=%q team1=%q team2=%q startTime=%d\n",
			e.ID, e.Level, e.Name, e.Team1, e.Team2, e.StartTime)
	}
	fmt.Printf("Events with sportId=20 (CS): %d\n", len(events20))
	for i, e := range events20 {
		if i >= 5 {
			fmt.Printf("  ... and %d more\n", len(events20)-5)
			break
		}
		fmt.Printf("  id=%d level=%d name=%q team1=%q team2=%q startTime=%d\n",
			e.ID, e.Level, e.Name, e.Team1, e.Team2, e.StartTime)
	}
	return nil
}

func testXbet(ctx context.Context, baseURL string, save bool) error {
	// При baseURL != "" клиент использует его без resolve mirror
	if baseURL == "" {
		baseURL = "https://1xlite-6173396.bar"
		fmt.Println("Using -xbet-url=" + baseURL + " (pass -xbet-url to override)")
	}
	client := xbet1.NewClient(baseURL, "", 30*time.Second, nil)

	const sportID = 40 // киберспорт
	countryID := 1
	virtualSports := true

	champs, err := client.GetChamps(sportID, countryID, virtualSports)
	if err != nil {
		return fmt.Errorf("GetChamps(40): %w", err)
	}
	fmt.Printf("Champs (sport=40): %d\n", len(champs))
	if save {
		champsJSON, _ := json.MarshalIndent(champs, "", "  ")
		_ = os.WriteFile("test_esports_xbet_champs.json", champsJSON, 0644)
		fmt.Println("saved test_esports_xbet_champs.json")
	}
	for i, c := range champs {
		if i >= 15 {
			fmt.Printf("  ... and %d more\n", len(champs)-15)
			break
		}
		sub := ""
		if len(c.SC) > 0 {
			sub = fmt.Sprintf(" (sub-champs: %d)", len(c.SC))
		}
		fmt.Printf("  LI=%d L=%q LE=%q SI=%d%s\n", c.LI, c.L, c.LE, c.SI, sub)
	}

	// Взять первый чемпионат (или первый под-чемпионат) и запросить матчи
	champID := int64(0)
	for _, c := range champs {
		if len(c.SC) > 0 {
			champID = c.SC[0].LI
			break
		}
		champID = c.LI
		break
	}
	if champID == 0 {
		fmt.Println("No championship to fetch matches for.")
		return nil
	}

	matches, err := client.GetMatches(sportID, champID, 40, 4, countryID, virtualSports)
	if err != nil {
		return fmt.Errorf("GetMatches(40, %d): %w", champID, err)
	}
	fmt.Printf("\nMatches (sport=40, champ=%d): %d\n", champID, len(matches))
	if save {
		matchesJSON, _ := json.MarshalIndent(matches, "", "  ")
		_ = os.WriteFile("test_esports_xbet_matches.json", matchesJSON, 0644)
		fmt.Println("saved test_esports_xbet_matches.json")
	}
	for i, m := range matches {
		if i >= 5 {
			fmt.Printf("  ... and %d more\n", len(matches)-5)
			break
		}
		fmt.Printf("  I=%d O1=%q O2=%q L=%q S=%d E=%d\n", m.I, m.O1, m.O2, m.L, m.S, len(m.E))
	}
	return nil
}
