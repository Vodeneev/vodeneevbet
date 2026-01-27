package health

import (
	"log"
	"sort"
	"sync"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

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
