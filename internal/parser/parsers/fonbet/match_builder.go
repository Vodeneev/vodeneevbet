package fonbet

import (
	"fmt"
	"strconv"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// MatchBuilder handles building match structures from parsed data
type MatchBuilder struct {
	bookmaker string
}

// NewMatchBuilder creates a new match builder
func NewMatchBuilder(bookmaker string) interfaces.MatchBuilder {
	return &MatchBuilder{
		bookmaker: bookmaker,
	}
}

// BuildMatch builds a match from parsed data
func (b *MatchBuilder) BuildMatch(mainEvent interface{}, statisticalEvents []interface{}, factors []interface{}) (*interface{}, error) {
	// Type assertion to get the actual event
	fonbetEvent, ok := mainEvent.(FonbetEvent)
	if !ok {
		return nil, fmt.Errorf("invalid main event type")
	}

	// Convert factors
	fonbetFactors := make([]FonbetFactor, len(factors))
	for i, factor := range factors {
		if f, ok := factor.(FonbetFactor); ok {
			fonbetFactors[i] = f
		}
	}

	// Convert statistical events
	statEvents := make([]FonbetEvent, len(statisticalEvents))
	for i, event := range statisticalEvents {
		if e, ok := event.(FonbetEvent); ok {
			statEvents[i] = e
		}
	}

	now := time.Now()
	
	// Create match name
	matchName := fmt.Sprintf("%s vs %s", fonbetEvent.HomeTeam, fonbetEvent.AwayTeam)
	
	// Create match
	match := &models.Match{
		ID:         fonbetEvent.ID,
		Name:       matchName,
		HomeTeam:   fonbetEvent.HomeTeam,
		AwayTeam:   fonbetEvent.AwayTeam,
		StartTime:  fonbetEvent.StartTime,
		Sport:      "football",
		Tournament: fonbetEvent.Tournament,
		Bookmaker:  b.bookmaker,
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Add main match event
	mainEventModel, err := b.buildMainEvent(fonbetEvent, fonbetFactors)
	if err != nil {
		return nil, fmt.Errorf("failed to build main event: %w", err)
	}
	if mainEventModel != nil {
		match.Events = append(match.Events, *mainEventModel)
	}
	
	// Add statistical events
	for _, statEvent := range statEvents {
		statEventID, err := strconv.ParseInt(statEvent.ID, 10, 64)
		if err != nil {
			continue
		}
		
		// Get factors for this statistical event
		statFactors := b.getFactorsForEvent(statEventID, fonbetFactors)
		if len(statFactors) > 0 {
			statEventModel, err := b.buildStatisticalEvent(statEvent, statFactors)
			if err != nil {
				continue
			}
			if statEventModel != nil {
				match.Events = append(match.Events, *statEventModel)
			}
		}
	}
	
	// Return as interface{}
	var result interface{} = match
	return &result, nil
}

// BuildEvent builds an event from parsed data
func (b *MatchBuilder) BuildEvent(eventData interface{}, odds map[string]float64) (*interface{}, error) {
	event, ok := eventData.(FonbetEvent)
	if !ok {
		return nil, fmt.Errorf("invalid event type")
	}

	eventModel, err := b.buildEventModel(event, odds)
	if err != nil {
		return nil, err
	}

	var result interface{} = eventModel
	return &result, nil
}

// buildMainEvent builds the main match event
func (b *MatchBuilder) buildMainEvent(fonbetEvent FonbetEvent, factors []FonbetFactor) (*models.Event, error) {
	// Parse odds for main event
	oddsParser := &OddsParser{}
	mainOdds := oddsParser.ParseEventOdds(fonbetEvent, factors)
	
	if len(mainOdds) == 0 {
		return nil, nil
	}

	return b.buildEventModel(fonbetEvent, mainOdds)
}

// buildStatisticalEvent builds a statistical event
func (b *MatchBuilder) buildStatisticalEvent(fonbetEvent FonbetEvent, factors []FonbetFactor) (*models.Event, error) {
	// Parse odds for statistical event
	oddsParser := &OddsParser{}
	statOdds := oddsParser.ParseEventOdds(fonbetEvent, factors)
	
	if len(statOdds) == 0 {
		return nil, nil
	}

	return b.buildEventModel(fonbetEvent, statOdds)
}

// buildEventModel creates a models.Event from FonbetEvent and odds
func (b *MatchBuilder) buildEventModel(fonbetEvent FonbetEvent, odds map[string]float64) (*models.Event, error) {
	now := time.Now()
	
	// Determine event type
	eventType := b.getStandardEventType(fonbetEvent.Kind)
	marketName := models.GetMarketName(eventType)
	
	event := &models.Event{
		ID:         fmt.Sprintf("%s_%s", fonbetEvent.ID, eventType),
		EventType:  string(eventType),
		MarketName: marketName,
		Bookmaker:  b.bookmaker,
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Create outcomes
	for outcomeType, oddsValue := range odds {
		outcome := &models.Outcome{
			ID:          fmt.Sprintf("%s_%s_%s", event.ID, outcomeType, b.getParameterFromOutcome(outcomeType)),
			OutcomeType: string(b.getStandardOutcomeType(outcomeType)),
			Parameter:   b.getParameterFromOutcome(outcomeType),
			Odds:        oddsValue,
			Bookmaker:   b.bookmaker,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		event.Outcomes = append(event.Outcomes, *outcome)
	}
	
	return event, nil
}

// getStandardEventType maps Fonbet Kind to standard event type
func (b *MatchBuilder) getStandardEventType(kind int64) models.StandardEventType {
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

// getStandardOutcomeType maps outcome string to standard type
func (b *MatchBuilder) getStandardOutcomeType(outcome string) models.StandardOutcomeType {
	switch {
	case outcome == "outcome_1":
		return models.OutcomeTypeHomeWin
	case outcome == "outcome_2":
		return models.OutcomeTypeAwayWin
	case outcome == "outcome_3":
		return models.OutcomeTypeDraw
	case len(outcome) > 6 && outcome[:6] == "total_":
		return models.OutcomeTypeTotalOver
	case len(outcome) > 7 && outcome[:7] == "exact_":
		return models.OutcomeTypeExactCount
	default:
		return models.StandardOutcomeType(outcome)
	}
}

// getParameterFromOutcome extracts parameter from outcome string
func (b *MatchBuilder) getParameterFromOutcome(outcome string) string {
	if len(outcome) > 6 && outcome[:6] == "total_" {
		return outcome[6:]
	}
	if len(outcome) > 7 && outcome[:7] == "exact_" {
		return outcome[7:]
	}
	return ""
}

// getFactorsForEvent gets factors for a specific event
func (b *MatchBuilder) getFactorsForEvent(eventID int64, allFactors []FonbetFactor) []FonbetFactor {
	var factors []FonbetFactor
	for _, factor := range allFactors {
		// This is a simplified implementation
		// In reality, you'd need to match factors to events properly
		factors = append(factors, factor)
	}
	return factors
}
