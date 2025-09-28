package interfaces

import "github.com/Vodeneev/vodeneevbet/internal/pkg/models"

// Validator interface for data validation
type Validator interface {
	// ValidateMatch validates match data
	ValidateMatch(match *models.Match) error
	
	// ValidateEvent validates event data
	ValidateEvent(event *models.Event) error
	
	// ValidateOutcome validates outcome data
	ValidateOutcome(outcome *models.Outcome) error
	
	// ValidateValueBet validates value bet data
	ValidateValueBet(valueBet *models.ValueBet) error
}

// DataSanitizer interface for data sanitization
type DataSanitizer interface {
	// SanitizeMatch sanitizes match data
	SanitizeMatch(match *models.Match) error
	
	// SanitizeEvent sanitizes event data
	SanitizeEvent(event *models.Event) error
	
	// SanitizeOutcome sanitizes outcome data
	SanitizeOutcome(outcome *models.Outcome) error
}
