package fonbet

import (
	"encoding/json"
	"fmt"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// OddsParser handles parsing odds from Fonbet API responses
type OddsParser struct{}

// NewOddsParser creates a new odds parser
func NewOddsParser() interfaces.OddsParser {
	return &OddsParser{}
}

// ParseEvents parses events from JSON data
func (p *OddsParser) ParseEvents(jsonData []byte) ([]interface{}, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	// Convert to interface{} slice
	events := make([]interface{}, len(response.Events))
	for i, event := range response.Events {
		events[i] = event
	}
	
	return events, nil
}

// ParseFactors parses factors from JSON data
func (p *OddsParser) ParseFactors(jsonData []byte) ([]interface{}, error) {
	var response FonbetAPIResponse
	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	
	// Convert to interface{} slice
	factors := make([]interface{}, len(response.CustomFactors))
	for i, factor := range response.CustomFactors {
		factors[i] = factor
	}
	
	return factors, nil
}

// ParseEventOdds parses odds for any type of event
func (p *OddsParser) ParseEventOdds(event FonbetEvent, factors []FonbetFactor) map[string]float64 {
	// Determine event type based on Kind
	eventType := p.getEventTypeFromKind(event.Kind)
	
	switch eventType {
	case "corners":
		return p.parseCornerOdds(factors)
	case "yellow_cards":
		return p.parseYellowCardOdds(factors)
	case "fouls":
		return p.parseFoulOdds(factors)
	case "shots_on_target":
		return p.parseShotsOnTargetOdds(factors)
	case "offsides":
		return p.parseOffsideOdds(factors)
	case "throw_ins":
		return p.parseThrowInOdds(factors)
	default:
		// For main matches, parse basic match odds
		return p.parseMainMatchOdds(factors)
	}
}

// getEventTypeFromKind determines event type from Kind field
func (p *OddsParser) getEventTypeFromKind(kind int64) string {
	// Create a temporary event to use the standardized mapping
	tempEvent := FonbetAPIEvent{Kind: kind}
	eventType := p.getStandardEventType(tempEvent)
	return string(eventType)
}

// getStandardEventType maps a Fonbet event ID to a standard event type
func (p *OddsParser) getStandardEventType(event FonbetAPIEvent) models.StandardEventType {
	if eventType, exists := supportedEvents[event.Kind]; exists {
		return models.StandardEventType(eventType)
	}
	return models.StandardEventMainMatch // Default fallback
}

func addIfParam(odds map[string]float64, keyPrefix string, param string, value float64) {
	if param == "" {
		return
	}
	// In Fonbet, handicap-like lines are often encoded with a sign in pt (e.g. "+1.5", "-2").
	// For totals we only want unsigned numeric lines (e.g. "2.5").
	if param[0] == '+' || param[0] == '-' {
		return
	}
	odds[keyPrefix+param] = value
}

func addIfParamSigned(odds map[string]float64, keyPrefix string, param string, value float64) {
	if param == "" {
		return
	}
	odds[keyPrefix+param] = value
}

func addHandicap(odds map[string]float64, factor FonbetFactor) {
	switch factor.F {
	// Validated on match handicaps (PAOK vs Betis) and corners handicaps.
	case 910, 989, 1569, 927, 1672, 1677, 1680:
		addIfParamSigned(odds, "handicap_home_", factor.Pt, factor.V)
	case 912, 991, 1572, 928, 1675, 1678, 1681:
		addIfParamSigned(odds, "handicap_away_", factor.Pt, factor.V)
	}
}

// parseMainMatchOdds parses basic match odds (1X2, totals, etc.)
func (p *OddsParser) parseMainMatchOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		// 1X2 odds (Fonbet list response commonly uses 921/922/923).
		case 921:
			odds["outcome_1"] = factor.V
		case 922:
			odds["outcome_2"] = factor.V
		case 923:
			odds["outcome_3"] = factor.V

		// Match total goals over/under (Fonbet commonly uses 930/931).
		case 930: // Total over
			addIfParam(odds, "total_over_", factor.Pt, factor.V)
		case 931: // Total under
			addIfParam(odds, "total_under_", factor.Pt, factor.V)

		default:
			addHandicap(odds, factor)
		}
	}
	
	return odds
}

// parseCornerOdds parses corner betting odds
func (p *OddsParser) parseCornerOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		// 1X2 for corners (team1/team2/draw in corners market).
		case 921:
			odds["outcome_1"] = factor.V
		case 922:
			odds["outcome_2"] = factor.V
		case 923:
			odds["outcome_3"] = factor.V

		// Total corners over/under.
		case 930:
			addIfParam(odds, "total_over_", factor.Pt, factor.V)
		case 931:
			addIfParam(odds, "total_under_", factor.Pt, factor.V)
		default:
			// Corners handicap uses the same signed pt mechanism.
			addHandicap(odds, factor)
		}
	}
	
	return odds
}

// parseYellowCardOdds parses yellow card betting odds
func (p *OddsParser) parseYellowCardOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 921:
			odds["outcome_1"] = factor.V
		case 922:
			odds["outcome_2"] = factor.V
		case 923:
			odds["outcome_3"] = factor.V
		case 930:
			addIfParam(odds, "total_over_", factor.Pt, factor.V)
		case 931:
			addIfParam(odds, "total_under_", factor.Pt, factor.V)
		default:
			addHandicap(odds, factor)
		}
	}
	
	return odds
}

// parseFoulOdds parses foul betting odds
func (p *OddsParser) parseFoulOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 921:
			odds["outcome_1"] = factor.V
		case 922:
			odds["outcome_2"] = factor.V
		case 923:
			odds["outcome_3"] = factor.V
		case 930:
			addIfParam(odds, "total_over_", factor.Pt, factor.V)
		case 931:
			addIfParam(odds, "total_under_", factor.Pt, factor.V)
		default:
			addHandicap(odds, factor)
		}
	}
	
	return odds
}

// parseShotsOnTargetOdds parses shots on target betting odds
func (p *OddsParser) parseShotsOnTargetOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 921:
			odds["outcome_1"] = factor.V
		case 922:
			odds["outcome_2"] = factor.V
		case 923:
			odds["outcome_3"] = factor.V
		case 930:
			addIfParam(odds, "total_over_", factor.Pt, factor.V)
		case 931:
			addIfParam(odds, "total_under_", factor.Pt, factor.V)
		default:
			addHandicap(odds, factor)
		}
	}
	
	return odds
}

// parseOffsideOdds parses offside betting odds
func (p *OddsParser) parseOffsideOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 921:
			odds["outcome_1"] = factor.V
		case 922:
			odds["outcome_2"] = factor.V
		case 923:
			odds["outcome_3"] = factor.V
		case 930:
			addIfParam(odds, "total_over_", factor.Pt, factor.V)
		case 931:
			addIfParam(odds, "total_under_", factor.Pt, factor.V)
		default:
			addHandicap(odds, factor)
		}
	}
	
	return odds
}

// parseThrowInOdds parses throw-in betting odds
func (p *OddsParser) parseThrowInOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 921:
			odds["outcome_1"] = factor.V
		case 922:
			odds["outcome_2"] = factor.V
		case 923:
			odds["outcome_3"] = factor.V
		case 930:
			addIfParam(odds, "total_over_", factor.Pt, factor.V)
		case 931:
			addIfParam(odds, "total_under_", factor.Pt, factor.V)
		default:
			addHandicap(odds, factor)
		}
	}
	
	return odds
}
