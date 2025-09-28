package parsers

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// TestParser test parser with stub data
type TestParser struct {
	ydbClient *storage.YDBClient
	config    *config.Config
	name      string
}

// NewTestParser creates new test parser
func NewTestParser(config *config.Config) *TestParser {
	// Create YDB client
	ydbClient, err := storage.NewYDBClient(&config.YDB)
	if err != nil {
		log.Printf("Warning: Failed to create YDB client: %v", err)
	}
	
	return &TestParser{
		ydbClient: ydbClient,
		config:    config,
		name:      "test_bookmaker",
	}
}

// GetName returns parser name
func (p *TestParser) GetName() string {
	return p.name
}

// Start starts parser
func (p *TestParser) Start(ctx context.Context) error {
	log.Printf("Starting %s parser", p.GetName())

	ticker := time.NewTicker(p.config.Parser.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Stopping %s parser", p.GetName())
			return nil
		case <-ticker.C:
			if err := p.parseAndStore(); err != nil {
				log.Printf("Error parsing %s: %v", p.GetName(), err)
			}
		}
	}
}

// Stop stops parser
func (p *TestParser) Stop() error {
	return nil
}

// parseAndStore parses data and saves to YDB using hierarchical structure
func (p *TestParser) parseAndStore() error {
	// Generate test data
	matches := p.generateTestMatches()
	
	for _, match := range matches {
		// Create hierarchical match structure
		hierarchicalMatch := p.createHierarchicalMatch(match)
		
		// Store using YDB client
		if p.ydbClient != nil {
			if err := p.ydbClient.StoreMatch(context.Background(), hierarchicalMatch); err != nil {
				log.Printf("Failed to store match %s: %v", match.ID, err)
			} else {
				log.Printf("Successfully stored match %s", match.ID)
			}
		}
	}

	log.Printf("Parsed and stored %d matches from %s", len(matches), p.GetName())
	return nil
}

// createHierarchicalMatch creates a hierarchical match structure from test data
func (p *TestParser) createHierarchicalMatch(testMatch TestMatch) *models.Match {
	now := time.Now()
	
	// Extract team names
	homeTeam, awayTeam := p.extractTeamNames(testMatch.Name)
	
	// Create match
	match := &models.Match{
		ID:         testMatch.ID,
		Name:       testMatch.Name,
		HomeTeam:   homeTeam,
		AwayTeam:   awayTeam,
		StartTime:  testMatch.Time,
		Sport:      "football",
		Tournament: "Test Tournament",
		Bookmaker:  p.GetName(),
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Create events for different markets
	markets := []string{"1x2", "total", "handicap"}
	for _, market := range markets {
		event := models.Event{
			ID:         fmt.Sprintf("%s_%s", testMatch.ID, market),
			EventType:  p.getEventTypeFromMarket(market),
			MarketName: market,
			Bookmaker:  p.GetName(),
			Outcomes:   []models.Outcome{},
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		
		// Create outcomes
		outcomes := p.generateOutcomes(market)
		for outcomeType, odds := range outcomes {
			outcome := models.Outcome{
				ID:          fmt.Sprintf("%s_%s_%s", event.ID, outcomeType, p.getParameterFromOutcome(outcomeType)),
				OutcomeType: string(p.getStandardOutcomeType(outcomeType)),
				Parameter:   p.getParameterFromOutcome(outcomeType),
				Odds:        odds,
				Bookmaker:   p.GetName(),
				CreatedAt:   now,
				UpdatedAt:  now,
			}
			event.Outcomes = append(event.Outcomes, outcome)
		}
		
		match.Events = append(match.Events, event)
	}
	
	return match
}

// extractTeamNames extracts home and away team names from match name
func (p *TestParser) extractTeamNames(matchName string) (string, string) {
	// Try different separators
	separators := []string{" vs ", " - ", " v "}
	for _, sep := range separators {
		if parts := strings.Split(matchName, sep); len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
	}
	
	// Fallback
	return matchName, "Unknown Away"
}

// getEventTypeFromMarket maps market to event type
func (p *TestParser) getEventTypeFromMarket(market string) string {
	switch market {
	case "1x2":
		return "main_match"
	case "total":
		return "corners"
	case "handicap":
		return "yellow_cards"
	default:
		return "unknown"
	}
}

// getStandardOutcomeType maps outcome string to standard type
func (p *TestParser) getStandardOutcomeType(outcome string) models.StandardOutcomeType {
	switch {
	case strings.Contains(outcome, "home"):
		return models.OutcomeTypeHomeWin
	case strings.Contains(outcome, "away"):
		return models.OutcomeTypeAwayWin
	case strings.Contains(outcome, "draw"):
		return models.OutcomeTypeDraw
	case strings.Contains(outcome, "total_+"):
		return models.OutcomeTypeTotalOver
	case strings.Contains(outcome, "total_-"):
		return models.OutcomeTypeTotalUnder
	default:
		return models.StandardOutcomeType(outcome)
	}
}

// getParameterFromOutcome extracts parameter from outcome string
func (p *TestParser) getParameterFromOutcome(outcome string) string {
	if strings.Contains(outcome, "total_") {
		parts := strings.Split(outcome, "_")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	return ""
}

// TestMatch represents test match
type TestMatch struct {
	ID   string
	Name string
	Time time.Time
}

// generateTestMatches generates test matches
func (p *TestParser) generateTestMatches() []TestMatch {
	matches := []TestMatch{
		{
			ID:   "match_1",
			Name: "Real Madrid vs Barcelona",
			Time: time.Now().Add(2 * time.Hour),
		},
		{
			ID:   "match_2", 
			Name: "Manchester United vs Liverpool",
			Time: time.Now().Add(4 * time.Hour),
		},
		{
			ID:   "match_3",
			Name: "Bayern Munich vs Borussia Dortmund", 
			Time: time.Now().Add(6 * time.Hour),
		},
	}

	return matches
}

// generateOutcomes generates test outcomes for market
func (p *TestParser) generateOutcomes(market string) map[string]float64 {
	rand.Seed(time.Now().UnixNano())
	
	switch market {
	case "1x2":
		return map[string]float64{
			"win_home": 1.5 + rand.Float64()*0.5,  // 1.5-2.0
			"draw":     2.8 + rand.Float64()*0.4,   // 2.8-3.2
			"win_away": 1.8 + rand.Float64()*0.7,   // 1.8-2.5
		}
	case "total":
		return map[string]float64{
			"over_2.5":  1.7 + rand.Float64()*0.3,  // 1.7-2.0
			"under_2.5": 1.9 + rand.Float64()*0.3,  // 1.9-2.2
		}
	case "handicap":
		return map[string]float64{
			"handicap_home_-1": 2.1 + rand.Float64()*0.4,  // 2.1-2.5
			"handicap_away_+1": 1.6 + rand.Float64()*0.3,  // 1.6-1.9
		}
	default:
		return map[string]float64{}
	}
}
