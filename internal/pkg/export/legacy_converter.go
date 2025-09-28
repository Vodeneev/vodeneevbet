package export

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// LegacyOdd represents the old flat format
type LegacyOdd struct {
	MatchID   string             `json:"match_id"`
	Bookmaker string             `json:"bookmaker"`
	Market    string             `json:"market"`
	Outcomes  map[string]float64 `json:"outcomes"`
	UpdatedAt time.Time          `json:"updated_at"`
	MatchName string             `json:"match_name"`
	MatchTime time.Time          `json:"match_time"`
	Sport     string             `json:"sport"`
}

// LegacyExport represents the old export format
type LegacyExport struct {
	Timestamp  string      `json:"timestamp"`
	TotalOdds  int         `json:"total_odds"`
	Matches    []string    `json:"matches"`
	Odds       []LegacyOdd `json:"odds"`
}

// LegacyConverter converts old flat format to new hierarchical format
type LegacyConverter struct {
	exporter *HierarchicalExporter
}

// NewLegacyConverter creates a new legacy converter
func NewLegacyConverter() *LegacyConverter {
	return &LegacyConverter{
		exporter: NewHierarchicalExporter(),
	}
}

// ConvertLegacyToHierarchical converts old format to new hierarchical format
func (c *LegacyConverter) ConvertLegacyToHierarchical(legacyData []byte) (*HierarchicalExport, error) {
	var legacyExport LegacyExport
	if err := json.Unmarshal(legacyData, &legacyExport); err != nil {
		return nil, fmt.Errorf("failed to unmarshal legacy data: %w", err)
	}

	// Group odds by match
	matchesMap := make(map[string][]LegacyOdd)
	for _, odd := range legacyExport.Odds {
		matchesMap[odd.MatchID] = append(matchesMap[odd.MatchID], odd)
	}

	// Convert to hierarchical format
	var matches []models.Match
	for matchID, odds := range matchesMap {
		match, err := c.convertMatchFromLegacy(matchID, odds)
		if err != nil {
			// Log error but continue with other matches
			continue
		}
		matches = append(matches, *match)
	}

	return c.exporter.ExportMatches(matches), nil
}

// convertMatchFromLegacy converts legacy odds to a hierarchical match
func (c *LegacyConverter) convertMatchFromLegacy(matchID string, odds []LegacyOdd) (*models.Match, error) {
	if len(odds) == 0 {
		return nil, fmt.Errorf("no odds for match %s", matchID)
	}

	// Use first odd for match metadata
	firstOdd := odds[0]
	now := time.Now()

	// Parse match name to extract teams
	homeTeam, awayTeam := c.parseMatchName(firstOdd.MatchName)

	match := &models.Match{
		ID:         matchID,
		Name:       firstOdd.MatchName,
		HomeTeam:   homeTeam,
		AwayTeam:   awayTeam,
		StartTime:  firstOdd.MatchTime,
		Sport:      firstOdd.Sport,
		Tournament: "Unknown Tournament", // Not available in legacy format
		Bookmaker:  firstOdd.Bookmaker,
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Group odds by market/event type
	eventsMap := make(map[string][]LegacyOdd)
	for _, odd := range odds {
		eventType := c.determineEventType(odd.Market)
		eventsMap[eventType] = append(eventsMap[eventType], odd)
	}

	// Convert each event type to models.Event
	for eventType, eventOdds := range eventsMap {
		event := c.convertEventFromLegacy(matchID, eventType, eventOdds)
		match.Events = append(match.Events, *event)
	}

	return match, nil
}

// convertEventFromLegacy converts legacy odds to a models.Event
func (c *LegacyConverter) convertEventFromLegacy(matchID, eventType string, odds []LegacyOdd) *models.Event {
	if len(odds) == 0 {
		return nil
	}

	firstOdd := odds[0]
	now := time.Now()

	event := &models.Event{
		ID:         fmt.Sprintf("%s_%s", matchID, eventType),
		MatchID:    matchID,
		EventType:  eventType,
		MarketName: firstOdd.Market,
		Bookmaker:  firstOdd.Bookmaker,
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// Convert all outcomes from all odds for this event
	for _, odd := range odds {
		for outcomeType, oddsValue := range odd.Outcomes {
			outcome := models.Outcome{
				ID:          fmt.Sprintf("%s_%s_%s", matchID, eventType, outcomeType),
				EventID:     event.ID,
				OutcomeType: c.standardizeOutcomeType(outcomeType),
				Parameter:   c.extractParameter(outcomeType),
				Odds:        oddsValue,
				Bookmaker:   odd.Bookmaker,
				CreatedAt:   odd.UpdatedAt,
				UpdatedAt:   odd.UpdatedAt,
			}
			event.Outcomes = append(event.Outcomes, outcome)
		}
	}

	return event
}

// determineEventType determines the event type from market name
func (c *LegacyConverter) determineEventType(market string) string {
	switch strings.ToLower(market) {
	case "match result":
		return string(models.StandardEventMainMatch)
	case "corners":
		return string(models.StandardEventCorners)
	case "yellow cards":
		return string(models.StandardEventYellowCards)
	case "fouls":
		return string(models.StandardEventFouls)
	case "shots on target":
		return string(models.StandardEventShotsOnTarget)
	case "offsides":
		return string(models.StandardEventOffsides)
	case "throw-ins":
		return string(models.StandardEventThrowIns)
	default:
		return string(models.StandardEventMainMatch)
	}
}

// standardizeOutcomeType converts legacy outcome types to standard types
func (c *LegacyConverter) standardizeOutcomeType(outcomeType string) string {
	switch strings.ToLower(outcomeType) {
	case "home":
		return string(models.OutcomeTypeHomeWin)
	case "draw":
		return string(models.OutcomeTypeDraw)
	case "away":
		return string(models.OutcomeTypeAwayWin)
	case "total_+", "total_over":
		return string(models.OutcomeTypeTotalOver)
	case "total_-", "total_under":
		return string(models.OutcomeTypeTotalUnder)
	case "exact_":
		return string(models.OutcomeTypeExactCount)
	case "alt_total_+", "alt_total_over":
		return string(models.OutcomeTypeAltTotalOver)
	case "alt_total_-", "alt_total_under":
		return string(models.OutcomeTypeAltTotalUnder)
	default:
		return outcomeType
	}
}

// extractParameter extracts parameter from outcome type
func (c *LegacyConverter) extractParameter(outcomeType string) string {
	// Handle different parameter formats
	if strings.HasPrefix(outcomeType, "total_") {
		parts := strings.Split(outcomeType, "_")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	if strings.HasPrefix(outcomeType, "exact_") {
		parts := strings.Split(outcomeType, "_")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	if strings.HasPrefix(outcomeType, "alt_total_") {
		parts := strings.Split(outcomeType, "_")
		if len(parts) > 2 {
			return parts[2]
		}
	}
	return ""
}

// parseMatchName extracts team names from match name
func (c *LegacyConverter) parseMatchName(matchName string) (string, string) {
	// Simple parsing - assumes format "Team1 vs Team2"
	parts := strings.Split(matchName, " vs ")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	
	// Fallback - try to extract from "Corners for event X" format
	if strings.Contains(matchName, "Corners for event") {
		return "Unknown Home", "Unknown Away"
	}
	
	// Default fallback
	return "Unknown Home", "Unknown Away"
}

// ConvertAndExport converts legacy data and exports to new format
func (c *LegacyConverter) ConvertAndExport(legacyData []byte) ([]byte, error) {
	hierarchicalExport, err := c.ConvertLegacyToHierarchical(legacyData)
	if err != nil {
		return nil, err
	}

	return json.MarshalIndent(hierarchicalExport, "", "  ")
}
