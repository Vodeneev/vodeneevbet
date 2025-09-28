package fonbet

import (
	"encoding/json"
	"fmt"
	"time"
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
	
	var events []FonbetEvent
	for _, event := range response.Events {
		if p.isMainMatch(event) {
			events = append(events, FonbetEvent{
				ID:         fmt.Sprintf("%d", event.ID),
				Name:       event.Name,
				HomeTeam:   event.Team1,
				AwayTeam:   event.Team2,
				StartTime:  time.Unix(event.StartTime, 0),
				Category:   "football",
				Tournament: "Unknown Tournament",
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
		if p.isThrowInEvent(event) {
			throwInEvents = append(throwInEvents, event)
		}
	}
	
	return throwInEvents, nil
}

// ParseAllStatisticalEvents finds all statistical events from the response
func (p *JSONParser) ParseAllStatisticalEvents(jsonData []byte) (map[string][]FonbetAPIEvent, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	statisticalEvents := make(map[string][]FonbetAPIEvent)
	
	for _, event := range response.Events {
		if p.isStatisticalEvent(event) {
			eventType := p.getStatisticalEventType(event)
			if eventType != "" {
				statisticalEvents[eventType] = append(statisticalEvents[eventType], event)
			}
		}
	}
	
	return statisticalEvents, nil
}

// getStatisticalEventType returns the type name for a statistical event
func (p *JSONParser) getStatisticalEventType(event FonbetAPIEvent) string {
	switch event.Kind {
	case 400100:
		return "corners"
	case 400200:
		return "yellow_cards"
	case 400300:
		return "fouls"
	case 400400:
		return "shots_on_target"
	case 400500:
		return "offsides"
	case 401000:
		return "throw_ins"
	default:
		return ""
	}
}

// isMainMatch determines if an event is a main football match (not statistical event)
func (p *JSONParser) isMainMatch(event FonbetAPIEvent) bool {
	// Check if it's a statistical event
	if p.isStatisticalEvent(event) {
		return false
	}
	
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

// isCornerEvent determines if an event is a corner event
func (p *JSONParser) isCornerEvent(event FonbetAPIEvent) bool {
	// Corner events have kind: 400100 and rootKind: 400000
	return event.Kind == 400100 && event.RootKind == 400000
}

// isYellowCardEvent determines if an event is a yellow card event
func (p *JSONParser) isYellowCardEvent(event FonbetAPIEvent) bool {
	// Yellow card events have kind: 400200 and rootKind: 400000
	return event.Kind == 400200 && event.RootKind == 400000
}

// isFoulEvent determines if an event is a foul event
func (p *JSONParser) isFoulEvent(event FonbetAPIEvent) bool {
	// Foul events have kind: 400300 and rootKind: 400000
	return event.Kind == 400300 && event.RootKind == 400000
}

// isShotsOnTargetEvent determines if an event is a shots on target event
func (p *JSONParser) isShotsOnTargetEvent(event FonbetAPIEvent) bool {
	// Shots on target events have kind: 400400 and rootKind: 400000
	return event.Kind == 400400 && event.RootKind == 400000
}

// isOffsideEvent determines if an event is an offside event
func (p *JSONParser) isOffsideEvent(event FonbetAPIEvent) bool {
	// Offside events have kind: 400500 and rootKind: 400000
	return event.Kind == 400500 && event.RootKind == 400000
}

// isThrowInEvent determines if an event is a throw-in event
func (p *JSONParser) isThrowInEvent(event FonbetAPIEvent) bool {
	// Throw-in events have kind: 401000 and rootKind: 400000
	return event.Kind == 401000 && event.RootKind == 400000
}

// isStatisticalEvent determines if an event is any type of statistical event
func (p *JSONParser) isStatisticalEvent(event FonbetAPIEvent) bool {
	// Statistical events that should be filtered out
	statisticalEventKinds := map[int64]bool{
		400100: true, // Corner events
		400200: true, // Yellow cards
		400300: true, // Fouls
		400400: true, // Shots on target
		400500: true, // Offsides
		401000: true, // Throw-ins
	}
	
	return statisticalEventKinds[event.Kind]
}

