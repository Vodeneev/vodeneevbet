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
			ID:         fmt.Sprintf("%d", event.E),
			Name:       fmt.Sprintf("Event %d", event.E),
			HomeTeam:   "Home Team",
			AwayTeam:   "Away Team",
			StartTime:  time.Now().Add(2 * time.Hour),
			Category:   "football",
			Tournament: "Unknown Tournament",
		})
	}
	
	return events, nil
}

func (p *JSONParser) ParseFactors(jsonData []byte) ([]FonbetFactor, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	var factors []FonbetFactor
	for _, event := range response.Events {
		factors = append(factors, event.Factors...)
	}
	
	return factors, nil
}

