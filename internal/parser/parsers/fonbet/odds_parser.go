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

// parseMainMatchOdds parses basic match odds (1X2, totals, etc.)
func (p *OddsParser) parseMainMatchOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 1, 2, 3: // 1X2 odds
			odds[fmt.Sprintf("outcome_%d", factor.F)] = factor.V
		case 910, 912: // Total goals over/under (main totals)
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		default:
			// Handle all other factors with parameters
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			} else if factor.P != 0 {
				paramStr := fmt.Sprintf("%.1f", float64(factor.P)/100.0)
				odds[fmt.Sprintf("total_%s", paramStr)] = factor.V
			}
		}
	}
	
	return odds
}

// parseCornerOdds parses corner betting odds
func (p *OddsParser) parseCornerOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total corners over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact corner counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// parseYellowCardOdds parses yellow card betting odds
func (p *OddsParser) parseYellowCardOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total yellow cards over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact yellow card counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// parseFoulOdds parses foul betting odds
func (p *OddsParser) parseFoulOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total fouls over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact foul counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// parseShotsOnTargetOdds parses shots on target betting odds
func (p *OddsParser) parseShotsOnTargetOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total shots on target over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact shots on target counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// parseOffsideOdds parses offside betting odds
func (p *OddsParser) parseOffsideOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total offsides over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact offside counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// parseThrowInOdds parses throw-in betting odds
func (p *OddsParser) parseThrowInOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total throw-ins over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact throw-in counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}
