package fonbet

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// EventProcessor handles processing events
type EventProcessor struct {
	storage     interfaces.Storage
	eventFetcher interfaces.EventFetcher
	oddsParser  interfaces.OddsParser
	matchBuilder interfaces.MatchBuilder
}

// NewEventProcessor creates a new event processor
func NewEventProcessor(
	storage interfaces.Storage,
	eventFetcher interfaces.EventFetcher,
	oddsParser interfaces.OddsParser,
	matchBuilder interfaces.MatchBuilder,
) interfaces.EventProcessor {
	return &EventProcessor{
		storage:      storage,
		eventFetcher: eventFetcher,
		oddsParser:   oddsParser,
		matchBuilder: matchBuilder,
	}
}

// ProcessEvent processes a single event
func (p *EventProcessor) ProcessEvent(event interface{}) error {
	// Type assertion to get the actual event
	fonbetEvent, ok := event.(FonbetEvent)
	if !ok {
		return fmt.Errorf("invalid event type")
	}

	// Convert string ID to int64 for API call
	eventID, err := strconv.ParseInt(fonbetEvent.ID, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse event ID %s: %w", fonbetEvent.ID, err)
	}

	// Get factors for this specific event
	factorData, err := p.eventFetcher.FetchEventFactors(eventID)
	if err != nil {
		return fmt.Errorf("failed to get factors for event %s: %w", fonbetEvent.ID, err)
	}

	// Parse factors from the event-specific response
	factors, err := p.oddsParser.ParseFactors(factorData)
	if err != nil {
		return fmt.Errorf("failed to parse factors for event %s: %w", fonbetEvent.ID, err)
	}

	// Find factors for this specific event
	var eventFactors []FonbetFactor
	for _, factor := range factors {
		if f, ok := factor.(FonbetFactorGroup); ok {
			if f.EventID == eventID {
				eventFactors = f.Factors
				break
			}
		}
	}

	if len(eventFactors) == 0 {
		return fmt.Errorf("no factors found for event %s", fonbetEvent.ID)
	}

	// Parse odds based on event type
	oddsParser := &OddsParser{}
	odds := oddsParser.ParseEventOdds(fonbetEvent, eventFactors)
	if len(odds) > 0 {
		// Create hierarchical match structure
		match, err := p.matchBuilder.BuildEvent(fonbetEvent, odds)
		if err != nil {
			return fmt.Errorf("failed to build match: %w", err)
		}

		// Store using new hierarchical structure
		if match != nil {
			if matchModel, ok := (*match).(*models.Match); ok {
				if err := p.storage.StoreMatch(context.Background(), matchModel); err != nil {
					return fmt.Errorf("failed to store match: %w", err)
				}
			}
		}
	}

	return nil
}

// ProcessEvents processes multiple events
func (p *EventProcessor) ProcessEvents(events []interface{}) error {
	for _, event := range events {
		if err := p.ProcessEvent(event); err != nil {
			// Log error but continue with other events
			fmt.Printf("Error processing event: %v\n", err)
			continue
		}
	}
	return nil
}

// ProcessSportEvents processes events for a specific sport
func (p *EventProcessor) ProcessSportEvents(sport string) error {
	// Fetch events for the sport
	eventsData, err := p.eventFetcher.FetchEvents(sport)
	if err != nil {
		return fmt.Errorf("failed to fetch events for sport %s: %w", sport, err)
	}

	// Parse events
	events, err := p.oddsParser.ParseEvents(eventsData)
	if err != nil {
		return fmt.Errorf("failed to parse events: %w", err)
	}

	// Group events by match
	eventsByMatch := p.groupEventsByMatch(events)

	// Process each match with all its events
	for matchID, matchEvents := range eventsByMatch {
		if len(matchEvents) == 0 {
			continue
		}

		// The first event is always the main match (Level 1)
		mainEvent := matchEvents[0]
		statisticalEvents := matchEvents[1:] // All other events are statistical

		// Process the match with all its events
		if err := p.processMatchWithEvents(mainEvent, statisticalEvents); err != nil {
			fmt.Printf("Failed to process match %s: %v\n", matchID, err)
			continue
		}
	}

	return nil
}

// groupEventsByMatch groups events by their parent match ID
func (p *EventProcessor) groupEventsByMatch(events []interface{}) map[string][]interface{} {
	groups := make(map[string][]interface{})
	
	// First, find all main matches (Level 1)
	mainMatches := make(map[string]interface{})
	for _, event := range events {
		if fonbetEvent, ok := event.(FonbetAPIEvent); ok && fonbetEvent.Level == 1 {
			mainMatches[fmt.Sprintf("%d", fonbetEvent.ID)] = event
		}
	}
	
	// Then, for each main match, find all related events
	for matchID, mainMatch := range mainMatches {
		// Add the main match itself
		groups[matchID] = append(groups[matchID], mainMatch)
		
		// Find all statistical events for this match
		for _, event := range events {
			if fonbetEvent, ok := event.(FonbetAPIEvent); ok && fonbetEvent.Level > 1 && fonbetEvent.ParentID > 0 {
				parentID := fmt.Sprintf("%d", fonbetEvent.ParentID)
				if parentID == matchID {
					groups[matchID] = append(groups[matchID], event)
				}
			}
		}
	}
	
	return groups
}

// processMatchWithEvents processes a main match with all its statistical events
func (p *EventProcessor) processMatchWithEvents(mainEvent interface{}, statisticalEvents []interface{}) error {
	// Convert to FonbetEvent for processing
	mainFonbetEvent, ok := mainEvent.(FonbetAPIEvent)
	if !ok {
		return fmt.Errorf("invalid main event type")
	}

	// Get factors for main event
	mainFactors, err := p.getEventFactors(mainFonbetEvent.ID)
	if err != nil {
		fmt.Printf("Failed to get factors for main event %d: %v\n", mainFonbetEvent.ID, err)
		mainFactors = []FonbetFactor{}
	}

	// Create hierarchical match structure
	match, err := p.buildHierarchicalMatch(mainFonbetEvent, statisticalEvents, mainFactors)
	if err != nil {
		return fmt.Errorf("failed to build hierarchical match: %w", err)
	}

	// Store the match
	if err := p.storage.StoreMatch(context.Background(), match); err != nil {
		return fmt.Errorf("failed to store match: %w", err)
	}

	fmt.Printf("Successfully stored match %d with %d events\n", mainFonbetEvent.ID, len(match.Events))
	return nil
}

// getEventFactors gets factors for a specific event
func (p *EventProcessor) getEventFactors(eventID int64) ([]FonbetFactor, error) {
	factorData, err := p.eventFetcher.FetchEventFactors(eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to get factors: %w", err)
	}

	factors, err := p.oddsParser.ParseFactors(factorData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse factors: %w", err)
	}

	// Find factors for this specific event
	for _, factor := range factors {
		if group, ok := factor.(FonbetFactorGroup); ok {
			if group.EventID == eventID {
				return group.Factors, nil
			}
		}
	}

	return []FonbetFactor{}, nil
}

// buildHierarchicalMatch builds a hierarchical match structure
func (p *EventProcessor) buildHierarchicalMatch(mainEvent FonbetAPIEvent, statisticalEvents []interface{}, mainFactors []FonbetFactor) (*models.Match, error) {
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

	// Convert statistical events
	statEvents := make([]FonbetEvent, len(statisticalEvents))
	for i, event := range statisticalEvents {
		if apiEvent, ok := event.(FonbetAPIEvent); ok {
			statEvents[i] = FonbetEvent{
				ID:         fmt.Sprintf("%d", apiEvent.ID),
				Name:       apiEvent.Name,
				HomeTeam:   apiEvent.Team1,
				AwayTeam:   apiEvent.Team2,
				StartTime:  time.Unix(apiEvent.StartTime, 0),
				Category:   "football",
				Tournament: "Unknown Tournament",
				Kind:       apiEvent.Kind,
				RootKind:   apiEvent.RootKind,
				Level:      apiEvent.Level,
				ParentID:   apiEvent.ParentID,
			}
		}
	}

	// Use match builder to create the match
	matchBuilder := NewMatchBuilder("Fonbet")
	match, err := matchBuilder.BuildMatch(mainFonbetEvent, statEvents, mainFactors)
	if err != nil {
		return nil, err
	}

	if matchModel, ok := (*match).(*models.Match); ok {
		return matchModel, nil
	}

	return nil, fmt.Errorf("failed to convert match")
}
