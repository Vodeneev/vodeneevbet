package fonbet

import (
	"context"
	"encoding/json"
	"fmt"
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
	fmt.Printf("🚀 Starting optimized processing for sport: %s\n", sport)
	
	// Fetch events for the sport (single HTTP request)
	fetchStart := time.Now()
	eventsData, err := p.eventFetcher.FetchEvents(sport)
	if err != nil {
		return fmt.Errorf("failed to fetch events for sport %s: %w", sport, err)
	}
	fetchDuration := time.Since(fetchStart)
	fmt.Printf("⏱️  HTTP fetch took: %v\n", fetchDuration)

	// Parse the complete API response
	parseStart := time.Now()
	var apiResponse FonbetAPIResponse
	if err := json.Unmarshal(eventsData, &apiResponse); err != nil {
		return fmt.Errorf("failed to unmarshal API response: %w", err)
	}
	parseDuration := time.Since(parseStart)
	fmt.Printf("⏱️  JSON parsing took: %v\n", parseDuration)

	fmt.Printf("📊 Found %d events and %d factor groups in API response\n", 
		len(apiResponse.Events), len(apiResponse.CustomFactors))

	// Group events by match (Level 1 events are main matches)
	groupStart := time.Now()
	eventsByMatch := p.groupEventsByMatchFromAPI(apiResponse.Events)
	groupDuration := time.Since(groupStart)
	fmt.Printf("⏱️  Event grouping took: %v\n", groupDuration)
	
	fmt.Printf("🏆 Found %d main matches\n", len(eventsByMatch))

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
			fmt.Printf("❌ Failed to process match %s: %v\n", matchID, err)
			continue
		}
		buildDuration := time.Since(buildStart)
		
		matchDuration := time.Since(matchStart)
		processedCount++
		
		// Выводим замеры для каждого матча
		fmt.Printf("⏱️  Match %s: total=%v, factors=%v, build=%v\n", 
			matchID, matchDuration, factorDuration, buildDuration)
		
		// Прерываем через 10 матчей для анализа
		if processedCount >= 10 {
			fmt.Printf("🛑 Stopping after %d matches for analysis\n", processedCount)
			break
		}
	}
	processDuration := time.Since(processStart)
	
	totalDuration := time.Since(startTime)
	fmt.Printf("✅ Successfully processed %d matches for sport: %s\n", processedCount, sport)
	fmt.Printf("⏱️  Total timing: fetch=%v, parse=%v, group=%v, process=%v, total=%v\n", 
		fetchDuration, parseDuration, groupDuration, processDuration, totalDuration)
	
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
		// Storage is optional: allow running without YDB.
		if p.storage == nil {
			fmt.Printf("⚠️  Storage is not configured, skipping store (match %d)\n", mainEvent.ID)
			return nil
		}
		// Store the match
		storeStart := time.Now()
		if err := p.storage.StoreMatch(context.Background(), matchModel); err != nil {
			return fmt.Errorf("failed to store match: %w", err)
		}
		storeDuration := time.Since(storeStart)

		fmt.Printf("✅ Match %d: convert=%v, build=%v, store=%v, events=%d, factors=%d\n", 
			mainEvent.ID, convertDuration, buildDuration, storeDuration, len(matchModel.Events), len(factors))
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
