package fonbet

import (
	"fmt"
	"strconv"
	"strings"
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

	// Convert factor groups (customFactors) - each group belongs to a specific event ID.
	factorGroups := make([]FonbetFactorGroup, 0, len(factors))
	for _, factor := range factors {
		if g, ok := factor.(FonbetFactorGroup); ok {
			factorGroups = append(factorGroups, g)
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
	
	// Canonical match ID for consistent match identification across bookmakers.
	matchID := models.CanonicalMatchID(fonbetEvent.HomeTeam, fonbetEvent.AwayTeam, fonbetEvent.StartTime)

	// Create match
	match := &models.Match{
		ID:         matchID,
		Name:       matchName,
		HomeTeam:   fonbetEvent.HomeTeam,
		AwayTeam:   fonbetEvent.AwayTeam,
		StartTime:  fonbetEvent.StartTime,
		Sport:      "football",
		Tournament: fonbetEvent.Tournament,
		// Match row is shared between bookmakers; store bookmaker on events/outcomes instead.
		Bookmaker:  "",
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Add main match event
	mainEventModel, err := b.buildMainEvent(fonbetEvent, factorGroups)
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
		statFactors := b.getFactorsForEvent(statEventID, factorGroups)
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
func (b *MatchBuilder) buildMainEvent(fonbetEvent FonbetEvent, factorGroups []FonbetFactorGroup) (*models.Event, error) {
	// Parse odds for main event
	eventID, err := strconv.ParseInt(fonbetEvent.ID, 10, 64)
	if err != nil {
		return nil, nil
	}
	factors := b.getFactorsForEvent(eventID, factorGroups)
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
	eventType, ok := b.getStandardEventType(fonbetEvent)
	if !ok {
		// Do not downgrade unknown statistical events into main_match.
		return nil, nil
	}
	marketName := models.GetMarketName(eventType)

	matchID := models.CanonicalMatchID(fonbetEvent.HomeTeam, fonbetEvent.AwayTeam, fonbetEvent.StartTime)
	bookmakerKey := strings.ToLower(strings.TrimSpace(b.bookmaker))
	if bookmakerKey == "" {
		bookmakerKey = "unknown"
	}
	// Normalize bookmaker name to lowercase for consistency
	normalizedBookmaker := bookmakerKey
	
	event := &models.Event{
		ID:         fmt.Sprintf("%s_%s_%s", matchID, bookmakerKey, eventType),
		MatchID:    matchID,
		EventType:  string(eventType),
		MarketName: marketName,
		Bookmaker:  normalizedBookmaker,
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Create outcomes
	for outcomeType, oddsValue := range odds {
		param := b.getParameterFromOutcome(outcomeType)
		stdOutcome := string(b.getStandardOutcomeType(outcomeType))
		outcome := &models.Outcome{
			ID:          fmt.Sprintf("%s_%s_%s_%s_%s", matchID, bookmakerKey, eventType, stdOutcome, param),
			EventID:     event.ID,
			OutcomeType: stdOutcome,
			Parameter:   param,
			Odds:        oddsValue,
			Bookmaker:   normalizedBookmaker,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		event.Outcomes = append(event.Outcomes, *outcome)
	}
	
	return event, nil
}

// getStandardEventType maps Fonbet event Kind/Level to standard event type.
func (b *MatchBuilder) getStandardEventType(event FonbetEvent) (models.StandardEventType, bool) {
	switch event.Kind {
	case 1:
		return models.StandardEventMainMatch, true
	case 400100:
		return models.StandardEventCorners, true
	case 400200:
		return models.StandardEventYellowCards, true
	case 400300:
		return models.StandardEventFouls, true
	case 400400:
		return models.StandardEventShotsOnTarget, true
	case 400500:
		return models.StandardEventOffsides, true
	case 401000:
		return models.StandardEventThrowIns, true
	default:
		// Unknown kind: keep main match only for actual main match-like events,
		// but skip unknown statistical events.
		if event.Level > 1 || event.RootKind == 400000 {
			return "", false
		}
		return models.StandardEventMainMatch, true
	}
}

// getStandardOutcomeType maps outcome string to standard type
func (b *MatchBuilder) getStandardOutcomeType(outcome string) models.StandardOutcomeType {
	switch {
	case outcome == "outcome_1":
		return models.OutcomeTypeHomeWin
	case outcome == "outcome_2":
		return models.OutcomeTypeDraw
	case outcome == "outcome_3":
		return models.OutcomeTypeAwayWin
	case strings.HasPrefix(outcome, "handicap_home_"):
		return models.StandardOutcomeType("handicap_home")
	case strings.HasPrefix(outcome, "handicap_away_"):
		return models.StandardOutcomeType("handicap_away")
	case strings.HasPrefix(outcome, "total_over_"):
		return models.OutcomeTypeTotalOver
	case strings.HasPrefix(outcome, "total_under_"):
		return models.OutcomeTypeTotalUnder
	case strings.HasPrefix(outcome, "alt_total_over_"):
		return models.OutcomeTypeAltTotalOver
	case strings.HasPrefix(outcome, "alt_total_under_"):
		return models.OutcomeTypeAltTotalUnder
	case strings.HasPrefix(outcome, "exact_"):
		return models.OutcomeTypeExactCount
	default:
		return models.StandardOutcomeType(outcome)
	}
}

// getParameterFromOutcome extracts parameter from outcome string
func (b *MatchBuilder) getParameterFromOutcome(outcome string) string {
	if strings.HasPrefix(outcome, "handicap_home_") {
		return strings.TrimPrefix(outcome, "handicap_home_")
	}
	if strings.HasPrefix(outcome, "handicap_away_") {
		return strings.TrimPrefix(outcome, "handicap_away_")
	}
	if strings.HasPrefix(outcome, "total_over_") {
		return strings.TrimPrefix(outcome, "total_over_")
	}
	if strings.HasPrefix(outcome, "total_under_") {
		return strings.TrimPrefix(outcome, "total_under_")
	}
	if strings.HasPrefix(outcome, "alt_total_over_") {
		return strings.TrimPrefix(outcome, "alt_total_over_")
	}
	if strings.HasPrefix(outcome, "alt_total_under_") {
		return strings.TrimPrefix(outcome, "alt_total_under_")
	}
	if strings.HasPrefix(outcome, "exact_") {
		return strings.TrimPrefix(outcome, "exact_")
	}
	return ""
}

// getFactorsForEvent gets factors for a specific event
func (b *MatchBuilder) getFactorsForEvent(eventID int64, groups []FonbetFactorGroup) []FonbetFactor {
	for _, g := range groups {
		if g.EventID == eventID {
			return g.Factors
		}
	}
	return nil
}
