package fonbet

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// MatchConverter converts parser data to YDB match structures
type MatchConverter struct {
	bookmaker string
}

// NewMatchConverter creates a new match converter for a specific bookmaker
func NewMatchConverter(bookmaker string) *MatchConverter {
	return &MatchConverter{
		bookmaker: bookmaker,
	}
}

// ConvertToMatch converts parser events to a unified match structure
func (c *MatchConverter) ConvertToMatch(
	mainEvent FonbetEvent,
	statisticalEvents []FonbetEvent,
	allOutcomes map[string]map[string]float64, // eventID -> outcomes
) (*models.Match, error) {
	
	now := time.Now()
	
	// Create the main match
	match := &models.Match{
		ID:         mainEvent.ID,
		Name:       mainEvent.Name,
		HomeTeam:   mainEvent.HomeTeam,
		AwayTeam:   mainEvent.AwayTeam,
		StartTime:  mainEvent.StartTime,
		Sport:      mainEvent.Category,
		Tournament: mainEvent.Tournament,
		Bookmaker:  c.bookmaker,
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Add main match event
	mainMatchEvent, err := c.convertToEvent(mainEvent, allOutcomes[mainEvent.ID])
	if err != nil {
		return nil, fmt.Errorf("failed to convert main match event: %w", err)
	}
	match.Events = append(match.Events, *mainMatchEvent)
	
	// Add statistical events
	for _, statEvent := range statisticalEvents {
		event, err := c.convertToEvent(statEvent, allOutcomes[statEvent.ID])
		if err != nil {
			// Log error but continue with other events
			continue
		}
		match.Events = append(match.Events, *event)
	}
	
	return match, nil
}

// convertToEvent converts a FonbetEvent to a models.Event
func (c *MatchConverter) convertToEvent(fonbetEvent FonbetEvent, outcomes map[string]float64) (*models.Event, error) {
	// Determine event type based on Kind
	eventType := c.getStandardEventType(fonbetEvent.Kind)
	
	now := time.Now()
	
	event := &models.Event{
		ID:         fonbetEvent.ID,
		MatchID:    strconv.FormatInt(fonbetEvent.ParentID, 10), // Parent match ID for statistical events
		EventType:  string(eventType),
		MarketName: models.GetMarketName(eventType),
		Bookmaker:  c.bookmaker,
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Convert outcomes
	for outcomeType, odds := range outcomes {
		outcome := &models.Outcome{
			ID:          fmt.Sprintf("%s_%s", fonbetEvent.ID, outcomeType),
			EventID:     fonbetEvent.ID,
			OutcomeType: outcomeType,
			Parameter:   c.extractParameter(outcomeType),
			Odds:        odds,
			Bookmaker:   c.bookmaker,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		event.Outcomes = append(event.Outcomes, *outcome)
	}
	
	return event, nil
}

// getStandardEventType maps Fonbet Kind to standard event type
func (c *MatchConverter) getStandardEventType(kind int64) models.StandardEventType {
	switch kind {
	case 1:
		return models.StandardEventMainMatch
	case 400100:
		return models.StandardEventCorners
	case 400200:
		return models.StandardEventYellowCards
	case 400300:
		return models.StandardEventFouls
	case 400400:
		return models.StandardEventShotsOnTarget
	case 400500:
		return models.StandardEventOffsides
	case 401000:
		return models.StandardEventThrowIns
	default:
		return models.StandardEventMainMatch
	}
}

// extractParameter extracts parameter from outcome type (e.g., "total_2.5" -> "2.5")
func (c *MatchConverter) extractParameter(outcomeType string) string {
	// This is a simplified implementation
	// In reality, you'd need more sophisticated parsing
	if len(outcomeType) > 6 && outcomeType[:6] == "total_" {
		return outcomeType[6:]
	}
	if len(outcomeType) > 7 && outcomeType[:7] == "exact_" {
		return outcomeType[7:]
	}
	return ""
}

// ConvertOddsToOutcomes converts parser odds to standardized outcomes
func (c *MatchConverter) ConvertOddsToOutcomes(odds map[string]float64) map[string]float64 {
	standardizedOutcomes := make(map[string]float64)
	
	for outcomeType, oddsValue := range odds {
		standardType := c.standardizeOutcomeType(outcomeType)
		standardizedOutcomes[standardType] = oddsValue
	}
	
	return standardizedOutcomes
}

// standardizeOutcomeType converts parser-specific outcome types to standard types
func (c *MatchConverter) standardizeOutcomeType(outcomeType string) string {
	// Convert parser-specific outcome types to standard types
	switch {
	case outcomeType == "outcome_1":
		return string(models.OutcomeTypeHomeWin)
	case outcomeType == "outcome_2":
		return string(models.OutcomeTypeAwayWin)
	case outcomeType == "outcome_3":
		return string(models.OutcomeTypeDraw)
	case len(outcomeType) > 6 && outcomeType[:6] == "total_":
		return string(models.OutcomeTypeTotalOver) + "_" + outcomeType[6:]
	case len(outcomeType) > 7 && outcomeType[:7] == "exact_":
		return string(models.OutcomeTypeExactCount) + "_" + outcomeType[7:]
	default:
		return outcomeType // Keep as-is if no mapping found
	}
}

// GroupEventsByMatch groups events by their parent match
func (c *MatchConverter) GroupEventsByMatch(events []FonbetEvent) map[string][]FonbetEvent {
	groups := make(map[string][]FonbetEvent)
	
	for _, event := range events {
		var matchID string
		if event.ParentID > 0 {
			matchID = strconv.FormatInt(event.ParentID, 10)
		} else {
			matchID = event.ID
		}
		
		groups[matchID] = append(groups[matchID], event)
	}
	
	return groups
}
