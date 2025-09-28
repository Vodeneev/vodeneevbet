package parsers

// StandardEventType represents a standardized event type across all bookmakers
type StandardEventType string

const (
	StandardEventMainMatch      StandardEventType = "main_match"
	StandardEventCorners        StandardEventType = "corners"
	StandardEventYellowCards    StandardEventType = "yellow_cards"
	StandardEventFouls          StandardEventType = "fouls"
	StandardEventShotsOnTarget  StandardEventType = "shots_on_target"
	StandardEventOffsides       StandardEventType = "offsides"
	StandardEventThrowIns       StandardEventType = "throw_ins"
)

// EventMapper defines the interface for mapping bookmaker-specific events to standard types
type EventMapper interface {
	// GetStandardEventType maps a bookmaker-specific event to a standard event type
	GetStandardEventType(eventID int64) StandardEventType
	
	// IsSupportedEvent checks if an event type is supported by this bookmaker
	IsSupportedEvent(eventID int64) bool
	
	// GetSupportedEvents returns all supported event types for this bookmaker
	GetSupportedEvents() map[int64]StandardEventType
}

// GetMarketName returns the market name for a standard event type
func GetMarketName(eventType StandardEventType) string {
	switch eventType {
	case StandardEventMainMatch:
		return "Match Result"
	case StandardEventCorners:
		return "Corners"
	case StandardEventYellowCards:
		return "Yellow Cards"
	case StandardEventFouls:
		return "Fouls"
	case StandardEventShotsOnTarget:
		return "Shots on Target"
	case StandardEventOffsides:
		return "Offsides"
	case StandardEventThrowIns:
		return "Throw-ins"
	default:
		return "Unknown Market"
	}
}
