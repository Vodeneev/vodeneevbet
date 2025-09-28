package fonbet

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

type JSONParser struct{}

func NewJSONParser() *JSONParser {
	return &JSONParser{}
}

func (p *JSONParser) ParseEvents(jsonData []byte) ([]FonbetEvent, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	// First, create a map of parent events to get team names
	parentEvents := make(map[int64]FonbetAPIEvent)
	for _, event := range response.Events {
		if event.Level == 1 && event.Team1 != "" && event.Team2 != "" {
			parentEvents[event.ID] = event
		}
	}
	
	var events []FonbetEvent
	for _, event := range response.Events {
		// Filter out events where one of the teams is 0 (missing team)
		if event.Team1ID == 0 || event.Team2ID == 0 {
			continue
		}
		
		// Only include supported events
		if p.isSupportedEvent(event) {
			homeTeam := event.Team1
			awayTeam := event.Team2
			
			// For statistical events (Level > 1), get team names from parent
			if event.Level > 1 && event.ParentID > 0 {
				if parent, exists := parentEvents[event.ParentID]; exists {
					homeTeam = parent.Team1
					awayTeam = parent.Team2
				}
			}
			
			events = append(events, FonbetEvent{
				ID:         fmt.Sprintf("%d", event.ID),
				Name:       event.Name,
				HomeTeam:   homeTeam,
				AwayTeam:   awayTeam,
				StartTime:  time.Unix(event.StartTime, 0),
				Category:   "football",
				Tournament: "Unknown Tournament",
				Kind:       event.Kind,
				RootKind:   event.RootKind,
				Level:      event.Level,
				ParentID:   event.ParentID,
			})
		}
	}
	
	return events, nil
}

func (p *JSONParser) ParseFactors(jsonData []byte) ([]FonbetFactorGroup, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	return response.CustomFactors, nil
}

// ParseCornerEvents finds corner events (kind: 400100) from the response
func (p *JSONParser) ParseCornerEvents(jsonData []byte) ([]FonbetAPIEvent, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	var cornerEvents []FonbetAPIEvent
	for _, event := range response.Events {
		// Filter out events where one of the teams is 0 (missing team)
		if event.Team1ID == 0 || event.Team2ID == 0 {
			continue
		}
		
		if p.isCornerEvent(event) {
			cornerEvents = append(cornerEvents, event)
		}
	}
	
	return cornerEvents, nil
}

// ParseYellowCardEvents finds yellow card events (kind: 400200) from the response
func (p *JSONParser) ParseYellowCardEvents(jsonData []byte) ([]FonbetAPIEvent, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	var yellowCardEvents []FonbetAPIEvent
	for _, event := range response.Events {
		// Filter out events where one of the teams is 0 (missing team)
		if event.Team1ID == 0 || event.Team2ID == 0 {
			continue
		}
		
		if p.isYellowCardEvent(event) {
			yellowCardEvents = append(yellowCardEvents, event)
		}
	}
	
	return yellowCardEvents, nil
}

// ParseFoulEvents finds foul events (kind: 400300) from the response
func (p *JSONParser) ParseFoulEvents(jsonData []byte) ([]FonbetAPIEvent, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	var foulEvents []FonbetAPIEvent
	for _, event := range response.Events {
		// Filter out events where one of the teams is 0 (missing team)
		if event.Team1ID == 0 || event.Team2ID == 0 {
			continue
		}
		
		if p.isFoulEvent(event) {
			foulEvents = append(foulEvents, event)
		}
	}
	
	return foulEvents, nil
}

// ParseShotsOnTargetEvents finds shots on target events (kind: 400400) from the response
func (p *JSONParser) ParseShotsOnTargetEvents(jsonData []byte) ([]FonbetAPIEvent, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	var shotsEvents []FonbetAPIEvent
	for _, event := range response.Events {
		// Filter out events where one of the teams is 0 (missing team)
		if event.Team1ID == 0 || event.Team2ID == 0 {
			continue
		}
		
		if p.isShotsOnTargetEvent(event) {
			shotsEvents = append(shotsEvents, event)
		}
	}
	
	return shotsEvents, nil
}

// ParseOffsideEvents finds offside events (kind: 400500) from the response
func (p *JSONParser) ParseOffsideEvents(jsonData []byte) ([]FonbetAPIEvent, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	var offsideEvents []FonbetAPIEvent
	for _, event := range response.Events {
		// Filter out events where one of the teams is 0 (missing team)
		if event.Team1ID == 0 || event.Team2ID == 0 {
			continue
		}
		
		if p.isOffsideEvent(event) {
			offsideEvents = append(offsideEvents, event)
		}
	}
	
	return offsideEvents, nil
}

// ParseThrowInEvents finds throw-in events (kind: 401000) from the response
func (p *JSONParser) ParseThrowInEvents(jsonData []byte) ([]FonbetAPIEvent, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	var throwInEvents []FonbetAPIEvent
	for _, event := range response.Events {
		// Filter out events where one of the teams is 0 (missing team)
		if event.Team1ID == 0 || event.Team2ID == 0 {
			continue
		}
		
		if p.isThrowInEvent(event) {
			throwInEvents = append(throwInEvents, event)
		}
	}
	
	return throwInEvents, nil
}


// isMainMatch determines if an event is a main football match
func (p *JSONParser) isMainMatch(event FonbetAPIEvent) bool {
	// Main matches should have both team names
	if event.Team1 == "" || event.Team2 == "" {
		return false
	}
	
	// Additional checks for main matches
	// Main matches typically have level 0 or 1 (not sub-events)
	if event.Level > 1 {
		return false
	}
	
	return true
}

// EventType represents a standardized event type (alias for StandardEventType)
type EventType = models.StandardEventType

const (
	EventTypeMainMatch      EventType = models.StandardEventMainMatch
	EventTypeCorners        EventType = models.StandardEventCorners
	EventTypeYellowCards    EventType = models.StandardEventYellowCards
	EventTypeFouls          EventType = models.StandardEventFouls
	EventTypeShotsOnTarget  EventType = models.StandardEventShotsOnTarget
	EventTypeOffsides       EventType = models.StandardEventOffsides
	EventTypeThrowIns       EventType = models.StandardEventThrowIns
)

// supportedEvents defines which event types are supported by this parser
var supportedEvents = map[int64]EventType{
	1:      EventTypeMainMatch,
	400100: EventTypeCorners,
	400200: EventTypeYellowCards,
	400300: EventTypeFouls,
	400400: EventTypeShotsOnTarget,
	400500: EventTypeOffsides,
	401000: EventTypeThrowIns,
}

// isSupportedEvent checks if an event type is supported by this parser
func (p *JSONParser) isSupportedEvent(event FonbetAPIEvent) bool {
	_, exists := supportedEvents[event.Kind]
	return exists
}

// getEventType returns the standardized event type for a given event
func (p *JSONParser) getEventType(event FonbetAPIEvent) EventType {
	if eventType, exists := supportedEvents[event.Kind]; exists {
		return eventType
	}
	return EventTypeMainMatch // Default fallback
}

// isStatisticalEvent checks if an event is any statistical event (RootKind: 400000)
func (p *JSONParser) isStatisticalEvent(event FonbetAPIEvent) bool {
	return event.RootKind == 400000
}

// Legacy methods for backward compatibility
func (p *JSONParser) isCornerEvent(event FonbetAPIEvent) bool {
	return p.getEventType(event) == EventTypeCorners
}

func (p *JSONParser) isYellowCardEvent(event FonbetAPIEvent) bool {
	return p.getEventType(event) == EventTypeYellowCards
}

func (p *JSONParser) isFoulEvent(event FonbetAPIEvent) bool {
	return p.getEventType(event) == EventTypeFouls
}

func (p *JSONParser) isShotsOnTargetEvent(event FonbetAPIEvent) bool {
	return p.getEventType(event) == EventTypeShotsOnTarget
}

func (p *JSONParser) isOffsideEvent(event FonbetAPIEvent) bool {
	return p.getEventType(event) == EventTypeOffsides
}

func (p *JSONParser) isThrowInEvent(event FonbetAPIEvent) bool {
	return p.getEventType(event) == EventTypeThrowIns
}

// EventMapper interface implementation

// GetStandardEventType maps a Fonbet event ID to a standard event type
func (p *JSONParser) GetStandardEventType(eventID int64) models.StandardEventType {
	if eventType, exists := supportedEvents[eventID]; exists {
		return models.StandardEventType(eventType)
	}
	return models.StandardEventMainMatch // Default fallback
}

// IsSupportedEvent checks if an event type is supported by Fonbet parser
func (p *JSONParser) IsSupportedEvent(eventID int64) bool {
	_, exists := supportedEvents[eventID]
	return exists
}

// GetSupportedEvents returns all supported event types for Fonbet
func (p *JSONParser) GetSupportedEvents() map[int64]models.StandardEventType {
	result := make(map[int64]models.StandardEventType)
	for kind, eventType := range supportedEvents {
		result[kind] = models.StandardEventType(eventType)
	}
	return result
}


