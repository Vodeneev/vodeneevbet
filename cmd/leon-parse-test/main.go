// Тестовый скрипт для отладки парсинга Leon (leon.ru).
// Цепочка: sports → лиги (футбол) → events по одной лиге → event/all по первому матчу.
//
//	go run ./cmd/leon-parse-test
//	go run ./cmd/leon-parse-test -league 1970324836975847
//	go run ./cmd/leon-parse-test -event 1970324850132212
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers/leon"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

func main() {
	leagueID := flag.Int64("league", 0, "league ID to fetch events (default: first football league from sports)")
	eventID := flag.Int64("event", 0, "event ID to fetch single event (default: first from league)")
	verbose := flag.Bool("v", false, "verbose (dump raw JSON)")
	flag.Parse()

	if err := run(*leagueID, *eventID, *verbose); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(leagueID, eventID int64, verbose bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := leon.NewClient("", 30*time.Second)

	// 1) Получить все лиги (sports)
	slog.Info("Fetching sports...")
	sports, err := client.GetSports(ctx)
	if err != nil {
		return fmt.Errorf("GetSports: %w", err)
	}
	slog.Info("Sports loaded", "count", len(sports))

	leagueIDs := leon.CollectLeagueIDs(sports, "Soccer")
	if len(leagueIDs) == 0 {
		return fmt.Errorf("no football leagues with prematch found")
	}
	slog.Info("Football leagues (prematch>0)", "count", len(leagueIDs))

	if leagueID == 0 {
		leagueID = leagueIDs[0]
		slog.Info("Using first league", "league_id", leagueID)
	}

	// 2) Матчи лиги
	slog.Info("Fetching league events...", "league_id", leagueID)
	eventsResp, err := client.GetLeagueEvents(ctx, leagueID)
	if err != nil {
		return fmt.Errorf("GetLeagueEvents: %w", err)
	}
	slog.Info("League events", "total", eventsResp.TotalCount, "events_len", len(eventsResp.Events))

	if len(eventsResp.Events) == 0 {
		fmt.Println("No events in league.")
		return nil
	}

	leagueName := "Leon League"
	if eventsResp.Events[0].League.Name != "" {
		leagueName = eventsResp.Events[0].League.Name
	}

	// 3) Парсим из списка (events уже содержат markets в ответе events/all)
	fmt.Println("\n--- Parse from league list (first 3 events) ---")
	for i, ev := range eventsResp.Events {
		if i >= 3 {
			break
		}
		m := leon.LeonEventToMatch(&ev, leagueName)
		if m == nil {
			fmt.Printf("  [%d] event_id=%d — skip (no teams or past)\n", i+1, ev.ID)
			continue
		}
		fmt.Printf("  [%d] %s | %s vs %s | events=%d\n", i+1, m.StartTime.Format("2006-01-02 15:04"), m.HomeTeam, m.AwayTeam, len(m.Events))
		for _, e := range m.Events {
			fmt.Printf("       %s: outcomes=%d\n", e.EventType, len(e.Outcomes))
			for _, o := range e.Outcomes {
				fmt.Printf("         %s %q -> %.2f\n", o.OutcomeType, o.Parameter, o.Odds)
			}
		}
	}

	// 4) Один матч по event/all (полная линия)
	if eventID == 0 {
		eventID = eventsResp.Events[0].ID
	}
	slog.Info("Fetching single event (full line)...", "event_id", eventID)
	fullEv, err := client.GetEvent(ctx, eventID)
	if err != nil {
		return fmt.Errorf("GetEvent: %w", err)
	}
	if verbose {
		raw, _ := json.MarshalIndent(fullEv, "", "  ")
		fmt.Println("\n--- Raw event/all (first 4000 chars) ---")
		s := string(raw)
		if len(s) > 4000 {
			s = s[:4000] + "..."
		}
		fmt.Println(s)
	}

	m := leon.LeonEventToMatch(fullEv, leagueName)
	if m == nil {
		fmt.Println("\nLeonEventToMatch returned nil for full event.")
		return nil
	}
	fmt.Println("\n--- Parsed match from event/all ---")
	fmt.Printf("ID: %s\n", m.ID)
	fmt.Printf("Name: %s\n", m.Name)
	fmt.Printf("Home/Away: %s / %s\n", m.HomeTeam, m.AwayTeam)
	fmt.Printf("Start: %s\n", m.StartTime.Format(time.RFC3339))
	fmt.Printf("Tournament: %s\n", m.Tournament)
	fmt.Printf("Bookmaker: %s\n", m.Bookmaker)
	fmt.Printf("Events: %d\n", len(m.Events))
	for _, e := range m.Events {
		fmt.Printf("\n  Event: %s (%s)\n", e.EventType, e.MarketName)
		for _, o := range e.Outcomes {
			name := models.GetOutcomeTypeName(models.StandardOutcomeType(o.OutcomeType))
			fmt.Printf("    %s (%s) param=%q odds=%.2f\n", o.OutcomeType, name, o.Parameter, o.Odds)
		}
	}
	return nil
}
