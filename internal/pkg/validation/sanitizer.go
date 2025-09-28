package validation

import (
	"regexp"
	"strings"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// Sanitizer implements data sanitization
type Sanitizer struct{}

// NewSanitizer creates a new sanitizer
func NewSanitizer() interfaces.DataSanitizer {
	return &Sanitizer{}
}

// SanitizeMatch sanitizes match data
func (s *Sanitizer) SanitizeMatch(match *models.Match) error {
	if match == nil {
		return nil
	}

	// Sanitize string fields
	match.ID = s.sanitizeID(match.ID)
	match.Name = s.sanitizeString(match.Name)
	match.HomeTeam = s.sanitizeTeamName(match.HomeTeam)
	match.AwayTeam = s.sanitizeTeamName(match.AwayTeam)
	match.Sport = strings.ToLower(strings.TrimSpace(match.Sport))
	match.Tournament = s.sanitizeString(match.Tournament)
	match.Bookmaker = s.sanitizeBookmaker(match.Bookmaker)

	// Sanitize events
	for i := range match.Events {
		if err := s.SanitizeEvent(&match.Events[i]); err != nil {
			return err
		}
	}

	return nil
}

// SanitizeEvent sanitizes event data
func (s *Sanitizer) SanitizeEvent(event *models.Event) error {
	if event == nil {
		return nil
	}

	// Sanitize string fields
	event.ID = s.sanitizeID(event.ID)
	event.EventType = strings.ToLower(strings.TrimSpace(event.EventType))
	event.MarketName = s.sanitizeString(event.MarketName)
	event.Bookmaker = s.sanitizeBookmaker(event.Bookmaker)

	// Sanitize outcomes
	for i := range event.Outcomes {
		if err := s.SanitizeOutcome(&event.Outcomes[i]); err != nil {
			return err
		}
	}

	return nil
}

// SanitizeOutcome sanitizes outcome data
func (s *Sanitizer) SanitizeOutcome(outcome *models.Outcome) error {
	if outcome == nil {
		return nil
	}

	// Sanitize string fields
	outcome.ID = s.sanitizeID(outcome.ID)
	outcome.OutcomeType = strings.ToLower(strings.TrimSpace(outcome.OutcomeType))
	outcome.Parameter = s.sanitizeString(outcome.Parameter)
	outcome.Bookmaker = s.sanitizeBookmaker(outcome.Bookmaker)

	// Ensure odds is reasonable
	if outcome.Odds <= 0 {
		outcome.Odds = 1.0 // Default to 1.0 if invalid
	}
	if outcome.Odds > 1000 {
		outcome.Odds = 1000 // Cap at 1000
	}

	return nil
}

// Helper methods for sanitization

func (s *Sanitizer) sanitizeID(id string) string {
	// Remove any non-alphanumeric characters except underscores and hyphens
	reg := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	sanitized := reg.ReplaceAllString(id, "")
	
	// Ensure it's not empty
	if sanitized == "" {
		sanitized = "unknown"
	}
	
	// Limit length
	if len(sanitized) > 100 {
		sanitized = sanitized[:100]
	}
	
	return sanitized
}

func (s *Sanitizer) sanitizeString(str string) string {
	// Trim whitespace
	sanitized := strings.TrimSpace(str)
	
	// Remove any control characters
	reg := regexp.MustCompile(`[\x00-\x1F\x7F]`)
	sanitized = reg.ReplaceAllString(sanitized, "")
	
	// Limit length
	if len(sanitized) > 200 {
		sanitized = sanitized[:200]
	}
	
	return sanitized
}

func (s *Sanitizer) sanitizeTeamName(name string) string {
	// Trim and clean team name
	sanitized := strings.TrimSpace(name)
	
	// Remove any control characters
	reg := regexp.MustCompile(`[\x00-\x1F\x7F]`)
	sanitized = reg.ReplaceAllString(sanitized, "")
	
	// Remove multiple spaces
	reg = regexp.MustCompile(`\s+`)
	sanitized = reg.ReplaceAllString(sanitized, " ")
	
	// Limit length
	if len(sanitized) > 100 {
		sanitized = sanitized[:100]
	}
	
	return sanitized
}

func (s *Sanitizer) sanitizeBookmaker(bookmaker string) string {
	// Standardize bookmaker names
	sanitized := strings.TrimSpace(bookmaker)
	
	// Map common variations to standard names
	bookmakerMap := map[string]string{
		"fonbet":     "Fonbet",
		"bet365":     "Bet365",
		"pinnacle":   "Pinnacle",
		"betfair":    "Betfair",
		"sbobet":     "SBOBET",
		"williamhill": "WilliamHill",
		"william hill": "WilliamHill",
	}
	
	if standard, exists := bookmakerMap[strings.ToLower(sanitized)]; exists {
		return standard
	}
	
	// If not in map, capitalize first letter of each word
	words := strings.Fields(sanitized)
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}
	
	return strings.Join(words, " ")
}
