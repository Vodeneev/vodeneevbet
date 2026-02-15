package health

import (
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// getBookmakerFromEvents extracts bookmaker name from events (first non-empty bookmaker)
func getBookmakerFromEvents(events []models.Event) string {
	for _, ev := range events {
		if ev.Bookmaker != "" {
			return ev.Bookmaker
		}
	}
	return ""
}

// MergeMatchLists merges multiple match lists by match ID (same logic as AddMatch).
// Used by parser-orchestrator to aggregate matches from bookmaker services.
func MergeMatchLists(lists [][]models.Match) []models.Match {
	byID := make(map[string]*models.Match)
	for _, list := range lists {
		for i := range list {
			match := &list[i]
			mergeMatchInto(byID, match)
		}
	}
	out := make([]models.Match, 0, len(byID))
	for _, m := range byID {
		matchCopy := *m
		eventsCopy := make([]models.Event, len(m.Events))
		copy(eventsCopy, m.Events)
		matchCopy.Events = eventsCopy
		out = append(out, matchCopy)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// mergeMatchInto merges one match into the map (by match ID, merge events).
func mergeMatchInto(byID map[string]*models.Match, match *models.Match) {
	if existing, ok := byID[match.ID]; ok {
		existingEvents := make(map[string]*models.Event)
		for i := range existing.Events {
			existingEvents[existing.Events[i].ID] = &existing.Events[i]
		}
		for _, newEvent := range match.Events {
			if existingEvent, exists := existingEvents[newEvent.ID]; exists {
				existingOutcomes := make(map[string]*models.Outcome)
				for i := range existingEvent.Outcomes {
					existingOutcomes[existingEvent.Outcomes[i].ID] = &existingEvent.Outcomes[i]
				}
				for _, newOutcome := range newEvent.Outcomes {
					if existingOutcome, outcomeExists := existingOutcomes[newOutcome.ID]; outcomeExists {
						existingOutcome.Odds = newOutcome.Odds
						existingOutcome.UpdatedAt = newOutcome.UpdatedAt
					} else {
						existingEvent.Outcomes = append(existingEvent.Outcomes, newOutcome)
					}
				}
				existingEvent.UpdatedAt = newEvent.UpdatedAt
			} else {
				existing.Events = append(existing.Events, newEvent)
			}
		}
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
		// Set bookmaker from events if match.Bookmaker is empty
		if existing.Bookmaker == "" {
			existing.Bookmaker = getBookmakerFromEvents(existing.Events)
		}
	} else {
		matchCopy := *match
		eventsCopy := make([]models.Event, len(match.Events))
		copy(eventsCopy, match.Events)
		matchCopy.Events = eventsCopy
		// Set bookmaker from events if match.Bookmaker is empty
		if matchCopy.Bookmaker == "" {
			matchCopy.Bookmaker = getBookmakerFromEvents(matchCopy.Events)
		}
		byID[match.ID] = &matchCopy
	}
}

// InMemoryMatchStore stores matches in memory for fast API access
type InMemoryMatchStore struct {
	mu      sync.RWMutex
	matches map[string]*models.Match // key: match_id
}

var globalMatchStore *InMemoryMatchStore

func init() {
	globalMatchStore = &InMemoryMatchStore{
		matches: make(map[string]*models.Match),
	}
	initEsportsStore()
}

// AddMatch adds or updates a match in the in-memory store
func AddMatch(match *models.Match) {
	if globalMatchStore == nil {
		return
	}
	globalMatchStore.mu.Lock()
	defer globalMatchStore.mu.Unlock()

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

	mergeMatchInto(globalMatchStore.matches, match)
	totalMatches := len(globalMatchStore.matches)
	if slog.Default().Enabled(nil, slog.LevelDebug) {
		slog.Debug("Stored match", "match_id", match.ID, "bookmakers", bookmakerList, "total_matches_in_store", totalMatches)
	}
}

// GetMatches returns all matches from in-memory store
func GetMatches() []models.Match {
	if globalMatchStore == nil {
		return []models.Match{}
	}

	globalMatchStore.mu.RLock()
	defer globalMatchStore.mu.RUnlock()

	storeSize := len(globalMatchStore.matches)
	matches := make([]models.Match, 0, storeSize)
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

	slog.Debug("Retrieved matches from store", "count", len(matches), "store_size", storeSize)
	return matches
}

// GetMatchesByName returns matches whose name contains the given substring (case-insensitive).
// Name is matched against Match.Name; also against "HomeTeam - AwayTeam" and "HomeTeam vs AwayTeam".
// Returns all matching matches with full events and outcomes (coefficients).
func GetMatchesByName(nameQuery string) []models.Match {
	if globalMatchStore == nil || strings.TrimSpace(nameQuery) == "" {
		return []models.Match{}
	}

	globalMatchStore.mu.RLock()
	defer globalMatchStore.mu.RUnlock()

	q := strings.ToLower(strings.TrimSpace(nameQuery))
	out := make([]models.Match, 0)

	for _, match := range globalMatchStore.matches {
		name := strings.ToLower(match.Name)
		home := strings.ToLower(match.HomeTeam)
		away := strings.ToLower(match.AwayTeam)
		homeAway := home + " - " + away
		homeVsAway := home + " vs " + away
		if strings.Contains(name, q) || strings.Contains(homeAway, q) || strings.Contains(homeVsAway, q) ||
			strings.Contains(home, q) || strings.Contains(away, q) {
			matchCopy := *match
			eventsCopy := make([]models.Event, len(match.Events))
			copy(eventsCopy, match.Events)
			matchCopy.Events = eventsCopy
			out = append(out, matchCopy)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})

	return out
}

// ClearMatches clears all matches from the in-memory store
// Should be called at the start of each parsing cycle to ensure fresh data
func ClearMatches() {
	if globalMatchStore == nil {
		return
	}
	globalMatchStore.mu.Lock()
	defer globalMatchStore.mu.Unlock()

	clearedCount := len(globalMatchStore.matches)
	globalMatchStore.matches = make(map[string]*models.Match)
	slog.Info("Cleared matches from in-memory store", "cleared_count", clearedCount)
}

// --- Esports store (киберспорт, отдельно от футбола) ---

var globalEsportsStore *InMemoryEsportsStore

func initEsportsStore() {
	if globalEsportsStore == nil {
		globalEsportsStore = &InMemoryEsportsStore{
			matches: make(map[string]*models.EsportsMatch),
		}
	}
}

// InMemoryEsportsStore stores esports matches in memory
type InMemoryEsportsStore struct {
	mu      sync.RWMutex
	matches map[string]*models.EsportsMatch
}

// MergeEsportsMatchLists merges multiple esports match lists by match ID
func MergeEsportsMatchLists(lists [][]models.EsportsMatch) []models.EsportsMatch {
	byID := make(map[string]*models.EsportsMatch)
	for _, list := range lists {
		for i := range list {
			m := &list[i]
			mergeEsportsMatchInto(byID, m)
		}
	}
	out := make([]models.EsportsMatch, 0, len(byID))
	for _, m := range byID {
		matchCopy := *m
		marketsCopy := make([]models.EsportsMarket, len(m.Markets))
		copy(marketsCopy, m.Markets)
		matchCopy.Markets = marketsCopy
		out = append(out, matchCopy)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func mergeEsportsMatchInto(byID map[string]*models.EsportsMatch, m *models.EsportsMatch) {
	if existing, ok := byID[m.ID]; ok {
		existingMarkets := make(map[string]*models.EsportsMarket)
		for i := range existing.Markets {
			existingMarkets[existing.Markets[i].ID] = &existing.Markets[i]
		}
		for _, newMarket := range m.Markets {
			if ex, exists := existingMarkets[newMarket.ID]; exists {
				exOutcomes := make(map[string]*models.EsportsOutcome)
				for i := range ex.Outcomes {
					exOutcomes[ex.Outcomes[i].ID] = &ex.Outcomes[i]
				}
				for _, o := range newMarket.Outcomes {
					if eo, ok := exOutcomes[o.ID]; ok {
						eo.Odds = o.Odds
						eo.UpdatedAt = o.UpdatedAt
					} else {
						ex.Outcomes = append(ex.Outcomes, o)
					}
				}
				ex.UpdatedAt = newMarket.UpdatedAt
		} else {
			marketCopy := newMarket
			marketCopy.Outcomes = make([]models.EsportsOutcome, len(newMarket.Outcomes))
			copy(marketCopy.Outcomes, newMarket.Outcomes)
			existing.Markets = append(existing.Markets, marketCopy)
		}
		}
		existing.UpdatedAt = m.UpdatedAt
		if m.Bookmaker != "" {
			existing.Bookmaker = m.Bookmaker
		}
	} else {
		matchCopy := *m
		marketsCopy := make([]models.EsportsMarket, len(m.Markets))
		copy(marketsCopy, m.Markets)
		matchCopy.Markets = marketsCopy
		byID[m.ID] = &matchCopy
	}
}

// AddEsportsMatch adds or updates an esports match in the in-memory store
func AddEsportsMatch(match *models.EsportsMatch) {
	initEsportsStore()
	globalEsportsStore.mu.Lock()
	defer globalEsportsStore.mu.Unlock()
	mergeEsportsMatchInto(globalEsportsStore.matches, match)
	if slog.Default().Enabled(nil, slog.LevelDebug) {
		slog.Debug("Stored esports match", "match_id", match.ID, "discipline", match.Discipline, "total", len(globalEsportsStore.matches))
	}
}

// GetEsportsMatches returns all esports matches from in-memory store
func GetEsportsMatches() []models.EsportsMatch {
	initEsportsStore()
	globalEsportsStore.mu.RLock()
	defer globalEsportsStore.mu.RUnlock()
	matches := make([]models.EsportsMatch, 0, len(globalEsportsStore.matches))
	for _, m := range globalEsportsStore.matches {
		matchCopy := *m
		marketsCopy := make([]models.EsportsMarket, len(m.Markets))
		copy(marketsCopy, m.Markets)
		matchCopy.Markets = marketsCopy
		matches = append(matches, matchCopy)
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].UpdatedAt.After(matches[j].UpdatedAt)
	})
	return matches
}
