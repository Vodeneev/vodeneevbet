package validation

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// Validator implements data validation
type Validator struct{}

// NewValidator creates a new validator
func NewValidator() interfaces.Validator {
	return &Validator{}
}

// ValidateMatch validates match data
func (v *Validator) ValidateMatch(match *models.Match) error {
	if match == nil {
		return fmt.Errorf("match cannot be nil")
	}

	// Validate required fields
	if match.ID == "" {
		return fmt.Errorf("match ID cannot be empty")
	}

	if match.Name == "" {
		return fmt.Errorf("match name cannot be empty")
	}

	if match.HomeTeam == "" {
		return fmt.Errorf("home team cannot be empty")
	}

	if match.AwayTeam == "" {
		return fmt.Errorf("away team cannot be empty")
	}

	if match.Sport == "" {
		return fmt.Errorf("sport cannot be empty")
	}

	// Validate ID format (alphanumeric and underscores only)
	if !v.isValidID(match.ID) {
		return fmt.Errorf("invalid match ID format: %s", match.ID)
	}

	// Validate team names (no special characters except spaces and hyphens)
	if !v.isValidTeamName(match.HomeTeam) {
		return fmt.Errorf("invalid home team name: %s", match.HomeTeam)
	}

	if !v.isValidTeamName(match.AwayTeam) {
		return fmt.Errorf("invalid away team name: %s", match.AwayTeam)
	}

	// Validate sport (must be lowercase)
	if match.Sport != strings.ToLower(match.Sport) {
		return fmt.Errorf("sport must be lowercase: %s", match.Sport)
	}

	// Validate start time (must be in the future for new matches)
	if match.StartTime.Before(time.Now()) {
		return fmt.Errorf("start time cannot be in the past: %s", match.StartTime.Format(time.RFC3339))
	}

	// Validate events
	for i, event := range match.Events {
		if err := v.ValidateEvent(&event); err != nil {
			return fmt.Errorf("event %d validation failed: %w", i, err)
		}
	}

	return nil
}

// ValidateEvent validates event data
func (v *Validator) ValidateEvent(event *models.Event) error {
	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}

	// Validate required fields
	if event.ID == "" {
		return fmt.Errorf("event ID cannot be empty")
	}

	if event.EventType == "" {
		return fmt.Errorf("event type cannot be empty")
	}

	if event.MarketName == "" {
		return fmt.Errorf("market name cannot be empty")
	}

	if event.Bookmaker == "" {
		return fmt.Errorf("bookmaker cannot be empty")
	}

	// Validate ID format
	if !v.isValidID(event.ID) {
		return fmt.Errorf("invalid event ID format: %s", event.ID)
	}

	// Validate event type (must be one of the standard types)
	if !v.isValidEventType(event.EventType) {
		return fmt.Errorf("invalid event type: %s", event.EventType)
	}

	// Validate bookmaker name
	if !v.isValidBookmaker(event.Bookmaker) {
		return fmt.Errorf("invalid bookmaker: %s", event.Bookmaker)
	}

	// Validate outcomes
	for i, outcome := range event.Outcomes {
		if err := v.ValidateOutcome(&outcome); err != nil {
			return fmt.Errorf("outcome %d validation failed: %w", i, err)
		}
	}

	return nil
}

// ValidateOutcome validates outcome data
func (v *Validator) ValidateOutcome(outcome *models.Outcome) error {
	if outcome == nil {
		return fmt.Errorf("outcome cannot be nil")
	}

	// Validate required fields
	if outcome.ID == "" {
		return fmt.Errorf("outcome ID cannot be empty")
	}

	if outcome.OutcomeType == "" {
		return fmt.Errorf("outcome type cannot be empty")
	}

	if outcome.Bookmaker == "" {
		return fmt.Errorf("bookmaker cannot be empty")
	}

	// Validate ID format
	if !v.isValidID(outcome.ID) {
		return fmt.Errorf("invalid outcome ID format: %s", outcome.ID)
	}

	// Validate outcome type
	if !v.isValidOutcomeType(outcome.OutcomeType) {
		return fmt.Errorf("invalid outcome type: %s", outcome.OutcomeType)
	}

	// Validate odds (must be positive and reasonable)
	if outcome.Odds <= 0 {
		return fmt.Errorf("odds must be positive: %f", outcome.Odds)
	}

	if outcome.Odds > 1000 {
		return fmt.Errorf("odds too high (suspicious): %f", outcome.Odds)
	}

	// Validate bookmaker
	if !v.isValidBookmaker(outcome.Bookmaker) {
		return fmt.Errorf("invalid bookmaker: %s", outcome.Bookmaker)
	}

	return nil
}

// ValidateValueBet validates value bet data
func (v *Validator) ValidateValueBet(valueBet *models.ValueBet) error {
	if valueBet == nil {
		return fmt.Errorf("value bet cannot be nil")
	}

	// Validate required fields
	if valueBet.ID == "" {
		return fmt.Errorf("value bet ID cannot be empty")
	}

	if valueBet.MatchID == "" {
		return fmt.Errorf("match ID cannot be empty")
	}

	if valueBet.Market == "" {
		return fmt.Errorf("market cannot be empty")
	}

	if valueBet.Outcome == "" {
		return fmt.Errorf("outcome cannot be empty")
	}

	// Validate ID format
	if !v.isValidID(valueBet.ID) {
		return fmt.Errorf("invalid value bet ID format: %s", valueBet.ID)
	}

	// Validate odds
	if valueBet.BookmakerOdd <= 0 {
		return fmt.Errorf("bookmaker odd must be positive: %f", valueBet.BookmakerOdd)
	}

	if valueBet.ReferenceOdd <= 0 {
		return fmt.Errorf("reference odd must be positive: %f", valueBet.ReferenceOdd)
	}

	// Validate value percentage (must be positive for value bets)
	if valueBet.ValuePercent <= 0 {
		return fmt.Errorf("value percentage must be positive: %f", valueBet.ValuePercent)
	}

	// Validate stake
	if valueBet.Stake <= 0 {
		return fmt.Errorf("stake must be positive: %f", valueBet.Stake)
	}

	// Validate potential win
	if valueBet.PotentialWin <= 0 {
		return fmt.Errorf("potential win must be positive: %f", valueBet.PotentialWin)
	}

	// Validate bookmaker
	if !v.isValidBookmaker(valueBet.Bookmaker) {
		return fmt.Errorf("invalid bookmaker: %s", valueBet.Bookmaker)
	}

	return nil
}

// Helper methods for validation

func (v *Validator) isValidID(id string) bool {
	// ID should be alphanumeric with underscores and hyphens
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, id)
	return matched && len(id) > 0 && len(id) <= 100
}

func (v *Validator) isValidTeamName(name string) bool {
	// Team name should contain only letters, spaces, hyphens, and apostrophes
	matched, _ := regexp.MatchString(`^[a-zA-Z\s\-']+$`, name)
	return matched && len(name) > 0 && len(name) <= 100
}

func (v *Validator) isValidEventType(eventType string) bool {
	validTypes := map[string]bool{
		"main_match":      true,
		"corners":         true,
		"yellow_cards":    true,
		"fouls":           true,
		"shots_on_target": true,
		"offsides":        true,
		"throw_ins":       true,
	}
	return validTypes[eventType]
}

func (v *Validator) isValidOutcomeType(outcomeType string) bool {
	validTypes := map[string]bool{
		"home_win":        true,
		"draw":            true,
		"away_win":        true,
		"total_over":      true,
		"total_under":     true,
		"exact_count":     true,
		"alt_total_over":  true,
		"alt_total_under": true,
	}
	return validTypes[outcomeType]
}

func (v *Validator) isValidBookmaker(bookmaker string) bool {
	// Normalize to lowercase for comparison
	normalized := strings.ToLower(strings.TrimSpace(bookmaker))
	validBookmakers := map[string]bool{
		"fonbet":      true,
		"bet365":      true,
		"pinnacle":    true,
		"pinnacle888": true,
		"1xbet":       true,
		"betfair":     true,
		"sbobet":      true,
		"williamhill": true,
		"zenit":       true,
		"olimp":       true,
		"marathonbet": true,
	}
	return validBookmakers[normalized]
}
