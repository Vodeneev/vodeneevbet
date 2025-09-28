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

