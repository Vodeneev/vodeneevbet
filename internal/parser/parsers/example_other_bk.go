package parsers

// Example implementation for another bookmaker (e.g., 1xBet)
// This shows how the standardized event mapping works across different bookmakers

import "fmt"

// Example1xBetParser demonstrates how another bookmaker would implement EventMapper
type Example1xBetParser struct {
	// 1xBet might use different event IDs
	supportedEvents map[int64]StandardEventType
}

// NewExample1xBetParser creates a new 1xBet parser with its event mapping
func NewExample1xBetParser() *Example1xBetParser {
	return &Example1xBetParser{
		supportedEvents: map[int64]StandardEventType{
			1:      StandardEventMainMatch,      // Main match
			101:    StandardEventCorners,         // 1xBet uses 101 for corners
			102:    StandardEventYellowCards,     // 1xBet uses 102 for yellow cards
			103:    StandardEventFouls,           // 1xBet uses 103 for fouls
			104:    StandardEventShotsOnTarget,   // 1xBet uses 104 for shots on target
			105:    StandardEventOffsides,        // 1xBet uses 105 for offsides
			106:    StandardEventThrowIns,        // 1xBet uses 106 for throw-ins
		},
	}
}

// GetStandardEventType maps a 1xBet event ID to a standard event type
func (p *Example1xBetParser) GetStandardEventType(eventID int64) StandardEventType {
	if eventType, exists := p.supportedEvents[eventID]; exists {
		return eventType
	}
	return StandardEventMainMatch // Default fallback
}

// IsSupportedEvent checks if an event type is supported by 1xBet parser
func (p *Example1xBetParser) IsSupportedEvent(eventID int64) bool {
	_, exists := p.supportedEvents[eventID]
	return exists
}

// GetSupportedEvents returns all supported event types for 1xBet
func (p *Example1xBetParser) GetSupportedEvents() map[int64]StandardEventType {
	return p.supportedEvents
}

// Example usage function
func ExampleUsage() {
	// Fonbet parser
	fonbetParser := &FonbetJSONParser{} // Assuming this implements EventMapper
	
	// 1xBet parser
	onexbetParser := NewExample1xBetParser()
	
	// Both parsers can now map their specific event IDs to standard types
	fonbetEventType := fonbetParser.GetStandardEventType(400100) // Fonbet corner event
	onexbetEventType := onexbetParser.GetStandardEventType(101)  // 1xBet corner event
	
	fmt.Printf("Fonbet corner event maps to: %s\n", fonbetEventType)
	fmt.Printf("1xBet corner event maps to: %s\n", onexbetEventType)
	
	// Both should return the same standard type
	if fonbetEventType == onexbetEventType {
		fmt.Println("✅ Both bookmakers map to the same standard event type!")
	}
	
	// Get market name using standard type
	marketName := GetMarketName(fonbetEventType)
	fmt.Printf("Market name: %s\n", marketName)
}
