package parsers

import (
	"context"
	"log"
	"math/rand"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// TestParser test parser with stub data
type TestParser struct {
	*BaseParser
}

// NewTestParser creates new test parser
func NewTestParser(ydbClient *storage.YDBWorkingClient, config *config.Config) *TestParser {
	return &TestParser{
		BaseParser: NewBaseParser(ydbClient, config, "test_bookmaker"),
	}
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

// parseAndStore parses data and saves to Redis
func (p *TestParser) parseAndStore() error {
	// Generate test data
	matches := p.generateTestMatches()
	
	for _, match := range matches {
		// Save odds for each market
		markets := []string{"1x2", "total", "handicap"}
		
		for _, market := range markets {
			odd := &models.Odd{
				MatchID:   match.ID,
				Bookmaker: p.GetName(),
				Market:    market,
				Outcomes:  p.generateOutcomes(market),
				UpdatedAt: time.Now(),
				MatchName: match.Name,
				MatchTime: match.Time,
				Sport:     "football",
			}

			if err := p.ydbClient.StoreOdd(context.Background(), odd); err != nil {
				log.Printf("Failed to store odd for match %s: %v", match.ID, err)
			}
		}
	}

	log.Printf("Parsed and stored %d matches from %s", len(matches), p.GetName())
	return nil
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
