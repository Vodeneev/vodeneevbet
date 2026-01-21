package health

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/performance"
)

// Global parser registry for on-demand parsing
var (
	globalParsers   []interfaces.Parser
	globalParsersMu sync.RWMutex
)

// RegisterParsers registers parsers for on-demand parsing
func RegisterParsers(parsers []interfaces.Parser) {
	globalParsersMu.Lock()
	defer globalParsersMu.Unlock()
	globalParsers = parsers
}

// triggerParsing triggers parsing for all parsers asynchronously and waits for completion
func triggerParsing(ctx context.Context) error {
	globalParsersMu.RLock()
	parsers := globalParsers
	globalParsersMu.RUnlock()
	
	if len(parsers) == 0 {
		return nil
	}
	
	// Trigger parsing for all parsers in parallel (asynchronously to different bookmakers)
	// Wait for all to complete before returning
	var wg sync.WaitGroup
	errCh := make(chan error, len(parsers))
	
	for _, p := range parsers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.ParseOnce(ctx); err != nil {
				log.Printf("On-demand parsing failed for %s: %v", p.GetName(), err)
				errCh <- err
			}
		}()
	}
	
	wg.Wait()
	close(errCh)
	
	// Check if any parser failed
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	
	return nil
}

// InMemoryMatchStore stores matches in memory for fast API access
type InMemoryMatchStore struct {
	mu      sync.RWMutex
	matches map[string]*models.Match // key: match_id
	maxSize int                      // maximum number of matches to keep
}

var globalMatchStore *InMemoryMatchStore

func init() {
	globalMatchStore = &InMemoryMatchStore{
		matches: make(map[string]*models.Match),
		maxSize: 50000, // Keep last 50000 matches (increased for better coverage)
	}
}

// AddMatch adds or updates a match in the in-memory store
func AddMatch(match *models.Match) {
	if globalMatchStore == nil {
		return
	}
	globalMatchStore.mu.Lock()
	defer globalMatchStore.mu.Unlock()

	// If we exceed max size, remove oldest matches (simple FIFO)
	if len(globalMatchStore.matches) >= globalMatchStore.maxSize {
		// Remove first 10% of matches (simple cleanup)
		removed := 0
		for k := range globalMatchStore.matches {
			if removed >= globalMatchStore.maxSize/10 {
				break
			}
			delete(globalMatchStore.matches, k)
			removed++
		}
	}

	// Detect bookmaker from events
	bookmakers := make(map[string]bool)
	for _, ev := range match.Events {
		if ev.Bookmaker != "" {
			bookmakers[ev.Bookmaker] = true
		}
	}
	bookmakerList := make([]string, 0, len(bookmakers))
	for bk := range bookmakers {
		bookmakerList = append(bookmakerList, bk)
	}

	// Add or update match (UPSERT logic - merge events from different bookmakers)
	if existing, ok := globalMatchStore.matches[match.ID]; ok {
		// Merge events: add new events or update existing ones
		existingEvents := make(map[string]*models.Event)
		for i := range existing.Events {
			existingEvents[existing.Events[i].ID] = &existing.Events[i]
		}

		addedCount := 0
		updatedCount := 0
		// Add new events or update existing ones
		for _, newEvent := range match.Events {
			if existingEvent, exists := existingEvents[newEvent.ID]; exists {
				// Update existing event: merge outcomes (update odds for live matches)
				existingOutcomes := make(map[string]*models.Outcome)
				for i := range existingEvent.Outcomes {
					existingOutcomes[existingEvent.Outcomes[i].ID] = &existingEvent.Outcomes[i]
				}

				// Update or add outcomes
				for _, newOutcome := range newEvent.Outcomes {
					if existingOutcome, outcomeExists := existingOutcomes[newOutcome.ID]; outcomeExists {
						// Update existing outcome (important for live matches - odds change)
						existingOutcome.Odds = newOutcome.Odds
						existingOutcome.UpdatedAt = newOutcome.UpdatedAt
						updatedCount++
					} else {
						// Add new outcome
						existingEvent.Outcomes = append(existingEvent.Outcomes, newOutcome)
						updatedCount++
					}
				}
				existingEvent.UpdatedAt = newEvent.UpdatedAt
			} else {
				// Add new event
				existing.Events = append(existing.Events, newEvent)
				addedCount++
			}
		}

		if addedCount > 0 || updatedCount > 0 {
			log.Printf("✅ Updated match %s: added %d events, updated %d outcomes from %v",
				match.ID, addedCount, updatedCount, bookmakerList)
		}

		// Update metadata
		existing.UpdatedAt = match.UpdatedAt
		if match.Name != "" {
			existing.Name = match.Name
		}
		if match.HomeTeam != "" {
			existing.HomeTeam = match.HomeTeam
		}
		if match.AwayTeam != "" {
			existing.AwayTeam = match.AwayTeam
		}
	} else {
		// Create copy to avoid race conditions
		matchCopy := *match
		eventsCopy := make([]models.Event, len(match.Events))
		copy(eventsCopy, match.Events)
		matchCopy.Events = eventsCopy
		globalMatchStore.matches[match.ID] = &matchCopy
		log.Printf("✅ Added new match %s from %v with %d events",
			match.ID, bookmakerList, len(match.Events))
	}
}

// GetMatches returns all matches from in-memory store
func GetMatches() []models.Match {
	if globalMatchStore == nil {
		return []models.Match{}
	}

	globalMatchStore.mu.RLock()
	defer globalMatchStore.mu.RUnlock()

	matches := make([]models.Match, 0, len(globalMatchStore.matches))
	for _, match := range globalMatchStore.matches {
		// Create copy to avoid race conditions
		matchCopy := *match
		eventsCopy := make([]models.Event, len(match.Events))
		copy(eventsCopy, match.Events)
		matchCopy.Events = eventsCopy
		matches = append(matches, matchCopy)
	}

	// Sort by updated_at descending (most recent first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
	})

	return matches
}

// Run starts a small HTTP server with /ping, /health, /metrics, and /matches endpoints.
// It stops gracefully when ctx is canceled.
// storage parameter is kept for backward compatibility but not used (matches come from in-memory store)
func Run(ctx context.Context, addr string, service string, storage interfaces.Storage) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("pong\n"))
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		tracker := performance.GetTracker()
		metrics := tracker.GetMetrics()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := json.NewEncoder(w).Encode(metrics); err != nil {
			http.Error(w, fmt.Sprintf("failed to encode metrics: %v", err), http.StatusInternalServerError)
			return
		}
	})

	// Add /matches endpoint - reads from in-memory store (faster than YDB)
	mux.HandleFunc("/matches", func(w http.ResponseWriter, r *http.Request) {
		handleMatches(w, r)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	go func() {
		log.Printf("%s: health server listening on %s", service, addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("%s: health server error: %v", service, err)
		}
	}()
}

// handleMatches handles /matches endpoint
// Flow: request -> async parsing to bookmakers -> matching into common model -> response
func handleMatches(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Trigger asynchronous parsing to all bookmakers in parallel
	// Wait for completion to get fresh data
	if err := triggerParsing(r.Context()); err != nil {
		log.Printf("Warning: some parsers failed: %v", err)
		// Continue anyway - return what we have in memory
	}

	// Get all matches from in-memory store (now contains fresh data from parsing)
	matches := GetMatches()

	duration := time.Since(startTime)
	matchCount := len(matches)

	// Add performance headers
	w.Header().Set("X-Query-Duration", duration.String())
	w.Header().Set("X-Matches-Count", fmt.Sprintf("%d", matchCount))
	w.Header().Set("X-Source", "memory") // Indicate data comes from memory, not YDB

	log.Printf("✅ Retrieved %d matches from memory in %v", matchCount, duration)

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"matches": matches,
		"meta": map[string]interface{}{
			"count":    matchCount,
			"duration": duration.String(),
			"source":   "memory", // Data comes from in-memory store, not YDB
		},
	}); err != nil {
		log.Printf("❌ Failed to encode matches: %v", err)
		http.Error(w, fmt.Sprintf("Failed to encode matches: %v", err), http.StatusInternalServerError)
		return
	}
}

// AddrFor returns a consistent default health listen address.
func AddrFor(service string) string {
	// Keep as :8080 inside container; publishing is handled by docker-compose.
	_ = service
	return fmt.Sprintf(":%d", 8080)
}
