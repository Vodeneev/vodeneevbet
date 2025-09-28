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
		// Include all events, not just main matches
		events = append(events, FonbetEvent{
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
		})
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

// isStatisticalEvent checks if an event is any statistical event (RootKind: 400000)
func (p *JSONParser) isStatisticalEvent(event FonbetAPIEvent) bool {
	return event.RootKind == 400000
}

// isCornerEvent determines if an event is a corner event
func (p *JSONParser) isCornerEvent(event FonbetAPIEvent) bool {
	return event.Kind == 400100
}

// isYellowCardEvent determines if an event is a yellow card event
func (p *JSONParser) isYellowCardEvent(event FonbetAPIEvent) bool {
	return event.Kind == 400200
}

// isFoulEvent determines if an event is a foul event
func (p *JSONParser) isFoulEvent(event FonbetAPIEvent) bool {
	return event.Kind == 400300
}

// isShotsOnTargetEvent determines if an event is a shots on target event
func (p *JSONParser) isShotsOnTargetEvent(event FonbetAPIEvent) bool {
	return event.Kind == 400400
}

// isOffsideEvent determines if an event is an offside event
func (p *JSONParser) isOffsideEvent(event FonbetAPIEvent) bool {
	return event.Kind == 400500
}

// isThrowInEvent determines if an event is a throw-in event
func (p *JSONParser) isThrowInEvent(event FonbetAPIEvent) bool {
	return event.Kind == 401000
}


