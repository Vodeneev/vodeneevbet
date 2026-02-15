// Тестовый скрипт для отладки парсинга API Олимп.
// Запуск: go run ./cmd/olimp-test
//
// Реальная структура API (ответы — массив из одного объекта с payload):
// 1. sports-with-categories-with-competitions?vids=1 → [0].payload.categoriesWithCompetitions[].competitions[].id (string)
// 2. competitions-with-events?vids[]=id: → [0].payload.events[] (id, team1Name, team2Name, startDateTime, outcomes[])
// 3. events?vids[]=eventId:&main=false → [0].payload (один матч с outcomes[])
// У step 2 и 3 обязателен Referer (иначе 400).
package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	baseURL  = "https://www.olimp.bet/api/v4/0/line"
	sportID  = 1
	referer  = "https://www.olimp.bet/line/futbol-1/"
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"
)

func main() {
	saveRaw := flag.Bool("save", false, "save raw JSON to ./olimp_raw_*.json")
	limitLeagues := flag.Int("leagues", 2, "max leagues (0=all)")
	limitEvents := flag.Int("events", 5, "max events per league (0=all)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 20 * time.Second}

	// ——— Шаг 1: лиги (футбол). Ответ: массив [ { payload: { categoriesWithCompetitions: [ { competitions: [] } ] } } ] ———
	fmt.Println("=== 1. sports-with-categories-with-competitions (vids=1) ===")
	u1 := baseURL + "/sports-with-categories-with-competitions?vids=1"
	body1, err := get(ctx, client, u1, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "step1: %v\n", err)
		os.Exit(1)
	}
	if *saveRaw {
		_ = os.WriteFile("olimp_raw_sports.json", body1, 0644)
		fmt.Println("saved olimp_raw_sports.json")
	}

	// API возвращает массив из одного элемента с payload
	var step1Arr []struct {
		Payload *struct {
			ID                         string `json:"id"`
			CategoriesWithCompetitions []struct {
				Competitions []struct {
					ID   string            `json:"id"`
					Name string            `json:"name"`
					Names map[string]string `json:"names"`
				} `json:"competitions"`
			} `json:"categoriesWithCompetitions"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body1, &step1Arr); err != nil {
		fmt.Fprintf(os.Stderr, "parse step1: %v\n", err)
		os.Exit(1)
	}
	var competitionIDs []struct{ id, name string }
	if len(step1Arr) > 0 && step1Arr[0].Payload != nil {
		for _, cat := range step1Arr[0].Payload.CategoriesWithCompetitions {
			for _, c := range cat.Competitions {
				name := c.Name
				if n := c.Names["2"]; n != "" {
					name = n
				}
				competitionIDs = append(competitionIDs, struct{ id, name string }{c.ID, name})
			}
		}
	}
	// Убираем дубликаты по id
	seen := make(map[string]bool)
	var unique []struct{ id, name string }
	for _, c := range competitionIDs {
		if !seen[c.id] {
			seen[c.id] = true
			unique = append(unique, c)
		}
	}
	competitionIDs = unique
	fmt.Printf("football competitions: %d\n", len(competitionIDs))
	if len(competitionIDs) == 0 {
		os.Exit(1)
	}
	if *limitLeagues > 0 && len(competitionIDs) > *limitLeagues {
		competitionIDs = competitionIDs[:*limitLeagues]
	}

	// ——— Шаг 2: матчи по лиге. Ответ: [ { id, payload: { events: [] } } ]. Referer обязателен. ———
	var allEvents []olimpEvent
	for _, c := range competitionIDs {
		fmt.Printf("\n=== 2. competitions-with-events vids[]=%s: ===\n", c.id+":")
		u2 := baseURL + "/competitions-with-events?" + url.Values{"vids[]": {c.id + ":"}}.Encode()
		ref2 := "https://www.olimp.bet/line/futbol-1/"
		body2, err := get(ctx, client, u2, ref2)
		if err != nil {
			fmt.Fprintf(os.Stderr, "step2 %s: %v\n", c.id, err)
			continue
		}
		if *saveRaw {
			_ = os.WriteFile("olimp_raw_comp_"+c.id+".json", body2, 0644)
		}
		events := parseCompetitionsWithEvents(body2)
		fmt.Printf("league %s: events %d\n", c.name, len(events))
		for _, e := range events {
			e.CompetitionName = c.name
			allEvents = append(allEvents, e)
		}
		if *limitEvents > 0 && len(allEvents) >= *limitEvents {
			allEvents = allEvents[:*limitEvents]
			break
		}
	}
	if *limitEvents > 0 && len(allEvents) > *limitEvents {
		allEvents = allEvents[:*limitEvents]
	}
	fmt.Printf("\ntotal events: %d\n", len(allEvents))

	// ——— Шаг 3: полная линия по матчу (опционально, в step2 уже есть исход/тотал/фора) ———
	for i := 0; i < len(allEvents) && i < 2; i++ {
		e := allEvents[i]
		fmt.Printf("\n=== 3. events vids[]=%s: main=false ===\n", e.ID+":")
		u3 := baseURL + "/events?" + url.Values{"vids[]": {e.ID + ":"}, "main": {"false"}}.Encode()
		body3, err := get(ctx, client, u3, referer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "step3 %s: %v\n", e.ID, err)
			continue
		}
		if *saveRaw {
			_ = os.WriteFile("olimp_raw_event_"+e.ID+".json", body3, 0644)
		}
		printEventOutcomes(body3, e.ID)
	}

	fmt.Println("\nDone.")
}

type olimpEvent struct {
	ID               string
	Team1Name        string
	Team2Name        string
	StartDateTime    int64
	CompetitionName  string
	Outcomes         []olimpOutcome
}

type olimpOutcome struct {
	ID          string
	TableType   string
	Probability string
	Param       string
	ShortName   string
}

func parseCompetitionsWithEvents(body []byte) []olimpEvent {
	var arr []struct {
		Payload *struct {
			Events []struct {
				ID            string `json:"id"`
				Team1Name     string `json:"team1Name"`
				Team2Name     string `json:"team2Name"`
				StartDateTime int64  `json:"startDateTime"`
				Outcomes      []struct {
					ID          string `json:"id"`
					TableType   string `json:"tableType"`
					Probability string `json:"probability"`
					Param       string `json:"param"`
					ShortName   string `json:"shortName"`
				} `json:"outcomes"`
			} `json:"events"`
		} `json:"payload"`
	}
	if json.Unmarshal(body, &arr) != nil || len(arr) == 0 || arr[0].Payload == nil {
		return nil
	}
	var out []olimpEvent
	for _, ev := range arr[0].Payload.Events {
		e := olimpEvent{
			ID:            ev.ID,
			Team1Name:     ev.Team1Name,
			Team2Name:     ev.Team2Name,
			StartDateTime: ev.StartDateTime,
		}
		for _, o := range ev.Outcomes {
			e.Outcomes = append(e.Outcomes, olimpOutcome{
				ID:          o.ID,
				TableType:   o.TableType,
				Probability: o.Probability,
				Param:       o.Param,
				ShortName:   o.ShortName,
			})
		}
		out = append(out, e)
	}
	return out
}

func printEventOutcomes(body []byte, eventID string) {
	var arr []struct {
		Payload *struct {
			ID            string `json:"id"`
			Team1Name     string `json:"team1Name"`
			Team2Name     string `json:"team2Name"`
			StartDateTime int64  `json:"startDateTime"`
			Outcomes      []struct {
				ID          string `json:"id"`
				TableType   string `json:"tableType"`
				Probability string `json:"probability"`
				Param       string `json:"param"`
				ShortName   string `json:"shortName"`
			} `json:"outcomes"`
		} `json:"payload"`
	}
	if json.Unmarshal(body, &arr) != nil || len(arr) == 0 || arr[0].Payload == nil {
		fmt.Println("  parse error or empty payload")
		return
	}
	p := arr[0].Payload
	fmt.Printf("  %s - %s, start=%d\n", p.Team1Name, p.Team2Name, p.StartDateTime)
	oddsByType := make(map[string][]string)
	for _, o := range p.Outcomes {
		prob, _ := strconv.ParseFloat(o.Probability, 64)
		oddsByType[o.TableType] = append(oddsByType[o.TableType],
			fmt.Sprintf("%s param=%s k=%.2f", o.ShortName, o.Param, prob))
	}
	for t, lines := range oddsByType {
		fmt.Printf("  %s: %s\n", t, strings.Join(lines, " | "))
	}
}

func get(ctx context.Context, client *http.Client, rawURL, refererURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", userAgent)
	if refererURL != "" {
		req.Header.Set("Referer", refererURL)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	var r io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}
	return io.ReadAll(r)
}
