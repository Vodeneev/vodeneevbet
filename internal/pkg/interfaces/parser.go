package interfaces

import "context"

// Parser interface for bookmaker data parsers
type Parser interface {
	// Start starts the parser (may run in background or just wait for context)
	Start(ctx context.Context) error
	
	// Stop stops the parser
	Stop() error
	
	// GetName returns the parser name
	GetName() string
	
	// ParseOnce triggers a single parsing run (on-demand parsing)
	ParseOnce(ctx context.Context) error
}

// EventFetcher interface for fetching events from bookmaker APIs
type EventFetcher interface {
	// FetchEvents fetches events for a specific sport
	FetchEvents(sport string) ([]byte, error)
	
	// FetchEventFactors fetches factors for a specific event
	FetchEventFactors(eventID int64) ([]byte, error)
}

// OddsParser interface for parsing odds from bookmaker data
type OddsParser interface {
	// ParseEvents parses events from JSON data
	ParseEvents(jsonData []byte) ([]interface{}, error)
	
	// ParseFactors parses factors from JSON data
	ParseFactors(jsonData []byte) ([]interface{}, error)
}

// MatchBuilder interface for building match structures
type MatchBuilder interface {
	// BuildMatch builds a match from parsed data
	BuildMatch(mainEvent interface{}, statisticalEvents []interface{}, factors []interface{}) (*interface{}, error)
	
	// BuildEvent builds an event from parsed data
	BuildEvent(eventData interface{}, odds map[string]float64) (*interface{}, error)
}

// EventProcessor interface for processing events
type EventProcessor interface {
	// ProcessEvent processes a single event
	ProcessEvent(event interface{}) error
	
	// ProcessEvents processes multiple events
	ProcessEvents(events []interface{}) error
	
	// ProcessSportEvents processes events for a specific sport
	ProcessSportEvents(sport string) error
}
