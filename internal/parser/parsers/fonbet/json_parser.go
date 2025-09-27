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
		// Corner events have kind: 400100 and rootKind: 400000
		if event.Kind == 400100 && event.RootKind == 400000 {
			cornerEvents = append(cornerEvents, event)
		}
	}
	
	return cornerEvents, nil
}

