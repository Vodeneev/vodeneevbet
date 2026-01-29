package fonbet

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// OptimizedEventProcessor handles processing events without additional HTTP requests
type OptimizedEventProcessor struct {
	storage      interfaces.Storage
	eventFetcher interfaces.EventFetcher
	oddsParser   interfaces.OddsParser
	matchBuilder interfaces.MatchBuilder
}

// NewOptimizedEventProcessor creates a new optimized event processor
func NewOptimizedEventProcessor(
	storage interfaces.Storage,
	eventFetcher interfaces.EventFetcher,
	oddsParser interfaces.OddsParser,
	matchBuilder interfaces.MatchBuilder,
) interfaces.EventProcessor {
	return &OptimizedEventProcessor{
		storage:      storage,
		eventFetcher: eventFetcher,
		oddsParser:   oddsParser,
		matchBuilder: matchBuilder,
	}
}

// ProcessSportEvents processes events for a specific sport using data from main API response
func (p *OptimizedEventProcessor) ProcessSportEvents(sport string) error {
	startTime := time.Now()
	slog.Info("Starting optimized processing", "sport", sport)

	// Fetch events for the sport (single HTTP request)
	fetchStart := time.Now()
	eventsData, err := p.eventFetcher.FetchEvents(sport)
	if err != nil {
		return fmt.Errorf("failed to fetch events for sport %s: %w", sport, err)
	}
	fetchDuration := time.Since(fetchStart)
	slog.Debug("HTTP fetch completed", "duration", fetchDuration)

	// Parse the complete API response
	parseStart := time.Now()
	var apiResponse FonbetAPIResponse
	if err := json.Unmarshal(eventsData, &apiResponse); err != nil {
		return fmt.Errorf("failed to unmarshal API response: %w", err)
	}
	parseDuration := time.Since(parseStart)
	slog.Debug("JSON parsing completed", "duration", parseDuration)

	slog.Debug("Found events and factor groups", "events", len(apiResponse.Events), "factor_groups", len(apiResponse.CustomFactors))

	// Group events by match (Level 1 events are main matches)
	groupStart := time.Now()
	eventsByMatch := p.groupEventsByMatchFromAPI(apiResponse.Events)
	groupDuration := time.Since(groupStart)
	slog.Debug("Event grouping completed", "duration", groupDuration)

	slog.Info(fmt.Sprintf("Fonbet: Found main matches %d", len(eventsByMatch)))

	// Process each match with all its events and factors
	processStart := time.Now()
	processedCount := 0
	for matchID, matchEvents := range eventsByMatch {
		if len(matchEvents) == 0 {
			continue
		}

		matchStart := time.Now()

		// The first event is always the main match (Level 1)
		mainEvent := matchEvents[0]
		statisticalEvents := matchEvents[1:] // All other events are statistical

		// Get factors for this match from the main response
		factorStart := time.Now()
		matchFactors := p.getFactorsForMatch(matchID, apiResponse.CustomFactors)
		factorDuration := time.Since(factorStart)

		// Process the match with all its events and factors
		buildStart := time.Now()
		if err := p.processMatchWithEventsAndFactors(mainEvent, statisticalEvents, matchFactors); err != nil {
			slog.Warn("Failed to process match", "match_id", matchID, "error", err)
			continue
		}
		buildDuration := time.Since(buildStart)

		matchDuration := time.Since(matchStart)
		processedCount++

		// Выводим замеры для каждого матча
		slog.Debug("Match processed", "match_id", matchID, "total", matchDuration, "factors", factorDuration, "build", buildDuration)

		// Прерываем через 10 матчей для анализа
		if processedCount >= 10 {
			slog.Debug("Stopping after matches for analysis", "count", processedCount)
			break
		}
	}
	processDuration := time.Since(processStart)

	totalDuration := time.Since(startTime)
	slog.Info(fmt.Sprintf("Fonbet: Successfully processed matches %d (%s)", processedCount, sport))
	slog.Debug("Total timing", "fetch", fetchDuration, "parse", parseDuration, "group", groupDuration, "process", processDuration, "total", totalDuration)

	return nil
}

// groupEventsByMatchFromAPI groups events by their parent match ID from API response
func (p *OptimizedEventProcessor) groupEventsByMatchFromAPI(events []FonbetAPIEvent) map[string][]FonbetAPIEvent {
	groups := make(map[string][]FonbetAPIEvent)

	// First, find all main matches (Level 1)
	mainMatches := make(map[string]FonbetAPIEvent)
	for _, event := range events {
		if event.Level == 1 {
			matchID := fmt.Sprintf("%d", event.ID)
			mainMatches[matchID] = event
		}
	}

	// Then, for each main match, find all related events
	for matchID, mainMatch := range mainMatches {
		// Add the main match itself
		groups[matchID] = append(groups[matchID], mainMatch)

		// Find all statistical events for this match
		for _, event := range events {
			if event.Level > 1 && event.ParentID > 0 {
				parentID := fmt.Sprintf("%d", event.ParentID)
				if parentID == matchID {
					groups[matchID] = append(groups[matchID], event)
				}
			}
		}
	}

	return groups
}

// getFactorsForMatch gets factors for a specific match from the main API response
func (p *OptimizedEventProcessor) getFactorsForMatch(matchID string, customFactors []FonbetFactorGroup) []FonbetFactor {
	matchIDInt, err := strconv.ParseInt(matchID, 10, 64)
	if err != nil {
		return []FonbetFactor{}
	}

	// Find factors for this specific match
	for _, factorGroup := range customFactors {
		if factorGroup.EventID == matchIDInt {
			return factorGroup.Factors
		}
	}

	return []FonbetFactor{}
}

// processMatchWithEventsAndFactors processes a main match with all its statistical events and factors
func (p *OptimizedEventProcessor) processMatchWithEventsAndFactors(
	mainEvent FonbetAPIEvent,
	statisticalEvents []FonbetAPIEvent,
	factors []FonbetFactor,
) error {
	convertStart := time.Now()

	// Convert main event to FonbetEvent
	mainFonbetEvent := FonbetEvent{
		ID:         fmt.Sprintf("%d", mainEvent.ID),
		Name:       mainEvent.Name,
		HomeTeam:   mainEvent.Team1,
		AwayTeam:   mainEvent.Team2,
		StartTime:  time.Unix(mainEvent.StartTime, 0),
		Category:   "football",
		Tournament: "Unknown Tournament",
		Kind:       mainEvent.Kind,
		RootKind:   mainEvent.RootKind,
		Level:      mainEvent.Level,
		ParentID:   mainEvent.ParentID,
	}

	// Convert statistical events to interface{}
	statEvents := make([]interface{}, len(statisticalEvents))
	for i, event := range statisticalEvents {
		fonbetEvent := FonbetEvent{
			ID:         fmt.Sprintf("%d", event.ID),
			Name:       event.Name,
			HomeTeam:   event.Team1,
			AwayTeam:   event.Team2,
			StartTime:  time.Unix(event.StartTime, 0),
			Category:   "football",
			Tournament: "Unknown Tournament",
			Kind:       event.Kind,
			RootKind:   event.RootKind,
			Level:      event.Level,
			ParentID:   event.ParentID,
		}
		statEvents[i] = fonbetEvent
	}

	// Convert factors to interface{}
	factorsInterface := make([]interface{}, len(factors))
	for i, factor := range factors {
		factorsInterface[i] = factor
	}

	convertDuration := time.Since(convertStart)

	// Use match builder to create the match
	buildStart := time.Now()
	matchBuilder := NewMatchBuilder("Fonbet")
	match, err := matchBuilder.BuildMatch(mainFonbetEvent, statEvents, factorsInterface)
	if err != nil {
		return fmt.Errorf("failed to build match: %w", err)
	}
	buildDuration := time.Since(buildStart)

	if matchModel, ok := (*match).(*models.Match); ok {
		// Storage is optional: allow running without external storage.
		if p.storage == nil {
			slog.Debug("Storage is not configured, skipping store", "match_id", mainEvent.ID)
			return nil
		}
		// Store the match
		storeStart := time.Now()
		if err := p.storage.StoreMatch(context.Background(), matchModel); err != nil {
			return fmt.Errorf("failed to store match: %w", err)
		}
		storeDuration := time.Since(storeStart)

		slog.Debug("Match processed", "match_id", mainEvent.ID, "convert", convertDuration, "build", buildDuration, "store", storeDuration, "events", len(matchModel.Events), "factors", len(factors))
		return nil
	}

	return fmt.Errorf("failed to convert match")
}

func (p *OptimizedEventProcessor) ProcessEvent(event interface{}) error {
	return fmt.Errorf("ProcessEvent not supported in optimized processor")
}

func (p *OptimizedEventProcessor) ProcessEvents(events []interface{}) error {
	return fmt.Errorf("ProcessEvents not supported in optimized processor")
}
