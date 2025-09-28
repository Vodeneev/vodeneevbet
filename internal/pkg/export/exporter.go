package export

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// Export represents the export format
type Export struct {
	Timestamp string           `json:"timestamp"`
	TotalMatches int          `json:"total_matches"`
	Matches   []MatchExport   `json:"matches"`
}

// MatchExport represents a match with all its events
type MatchExport struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	HomeTeam    string         `json:"home_team"`
	AwayTeam    string         `json:"away_team"`
	StartTime   time.Time      `json:"start_time"`
	Sport       string         `json:"sport"`
	Tournament  string         `json:"tournament"`
	Bookmaker   string         `json:"bookmaker"`
	Events      []EventExport  `json:"events"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// EventExport represents an event within a match
type EventExport struct {
	ID          string           `json:"id"`
	EventType   string           `json:"event_type"`
	MarketName  string           `json:"market_name"`
	Bookmaker   string           `json:"bookmaker"`
	Outcomes    []OutcomeExport  `json:"outcomes"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// OutcomeExport represents a betting outcome
type OutcomeExport struct {
	ID          string  `json:"id"`
	OutcomeType string  `json:"outcome_type"`
	Parameter   string  `json:"parameter"`
	Odds        float64 `json:"odds"`
	Bookmaker   string  `json:"bookmaker"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Exporter handles the export format
type Exporter struct{}

// NewExporter creates a new exporter
func NewExporter() *Exporter {
	return &Exporter{}
}

// ExportMatches converts matches to export format
func (e *Exporter) ExportMatches(matches []models.Match) *Export {
	export := &Export{
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		TotalMatches: len(matches),
		Matches:      []MatchExport{},
	}

	for _, match := range matches {
		matchExport := e.convertMatch(match)
		export.Matches = append(export.Matches, matchExport)
	}

	return export
}

// convertMatch converts a models.Match to MatchExport
func (e *Exporter) convertMatch(match models.Match) MatchExport {
	events := make([]EventExport, 0, len(match.Events))
	
	for _, event := range match.Events {
		eventExport := e.convertEvent(event)
		events = append(events, eventExport)
	}

	return MatchExport{
		ID:         match.ID,
		Name:       match.Name,
		HomeTeam:   match.HomeTeam,
		AwayTeam:   match.AwayTeam,
		StartTime:  match.StartTime,
		Sport:      match.Sport,
		Tournament: match.Tournament,
		Bookmaker:  match.Bookmaker,
		Events:     events,
		CreatedAt:  match.CreatedAt,
		UpdatedAt:  match.UpdatedAt,
	}
}

// convertEvent converts a models.Event to EventExport
func (e *Exporter) convertEvent(event models.Event) EventExport {
	outcomes := make([]OutcomeExport, 0, len(event.Outcomes))
	
	for _, outcome := range event.Outcomes {
		outcomeExport := OutcomeExport{
			ID:          outcome.ID,
			OutcomeType: outcome.OutcomeType,
			Parameter:   outcome.Parameter,
			Odds:        outcome.Odds,
			Bookmaker:   outcome.Bookmaker,
			CreatedAt:   outcome.CreatedAt,
			UpdatedAt:   outcome.UpdatedAt,
		}
		outcomes = append(outcomes, outcomeExport)
	}

	return EventExport{
		ID:         event.ID,
		EventType:  event.EventType,
		MarketName: event.MarketName,
		Bookmaker:  event.Bookmaker,
		Outcomes:   outcomes,
		CreatedAt:  event.CreatedAt,
		UpdatedAt:  event.UpdatedAt,
	}
}

// ExportToJSON exports matches to JSON format
func (e *Exporter) ExportToJSON(matches []models.Match) ([]byte, error) {
	export := e.ExportMatches(matches)
	return json.MarshalIndent(export, "", "  ")
}

// ExportToCSV exports matches to CSV format (simplified)
func (e *Exporter) ExportToCSV(matches []models.Match) ([]byte, error) {
	// TODO: Implement CSV export for hierarchical structure
	// This would be more complex as we need to flatten the hierarchy
	return nil, fmt.Errorf("CSV export for hierarchical structure not implemented yet")
}

// PrintSummary prints a summary of the export
func (e *Exporter) PrintSummary(export *Export) {
	fmt.Printf("=== Export Summary ===\n")
	fmt.Printf("Timestamp: %s\n", export.Timestamp)
	fmt.Printf("Total Matches: %d\n", export.TotalMatches)
	
	totalEvents := 0
	totalOutcomes := 0
	
	for _, match := range export.Matches {
		totalEvents += len(match.Events)
		for _, event := range match.Events {
			totalOutcomes += len(event.Outcomes)
		}
	}
	
	fmt.Printf("Total Events: %d\n", totalEvents)
	fmt.Printf("Total Outcomes: %d\n", totalOutcomes)
	
	// Group by event type
	eventTypeCount := make(map[string]int)
	for _, match := range export.Matches {
		for _, event := range match.Events {
			eventTypeCount[event.EventType]++
		}
	}
	
	fmt.Printf("\nEvent Types:\n")
	for eventType, count := range eventTypeCount {
		fmt.Printf("  %s: %d\n", eventType, count)
	}
}
