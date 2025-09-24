package calculator

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// ValueCalculator calculator for finding value bets
type ValueCalculator struct {
	ydbClient *storage.YDBWorkingClient
	config    *config.ValueCalculatorConfig
}

// NewValueCalculator creates new Value Bet calculator
func NewValueCalculator(ydbClient *storage.YDBWorkingClient, config *config.ValueCalculatorConfig) *ValueCalculator {
	return &ValueCalculator{
		ydbClient: ydbClient,
		config:    config,
	}
}

// Start starts calculator
func (vc *ValueCalculator) Start(ctx context.Context) error {
	log.Printf("Starting Value Bet Calculator with interval: %s", vc.config.CheckInterval)
	
	// Parse interval
	interval, err := time.ParseDuration(vc.config.CheckInterval)
	if err != nil {
		log.Printf("Invalid check interval '%s', using test interval", vc.config.CheckInterval)
		interval, err = time.ParseDuration(vc.config.TestInterval)
		if err != nil {
			log.Printf("Invalid test interval '%s', using default 5 minutes", vc.config.TestInterval)
			interval = 5 * time.Minute
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Stopping Value Bet Calculator")
			return nil
		case <-ticker.C:
			if err := vc.findValueBets(ctx); err != nil {
				log.Printf("Error finding value bets: %v", err)
			}
		}
	}
}

// findValueBets searches for value bets
func (vc *ValueCalculator) findValueBets(ctx context.Context) error {
	log.Println("Searching for value bets...")
	
	// Get all matches
	matches, err := vc.ydbClient.GetAllMatches(ctx)
	if err != nil {
		return fmt.Errorf("failed to get matches: %v", err)
	}

	if len(matches) == 0 {
		log.Println("No matches found")
		return nil
	}

	log.Printf("Found %d matches to analyze", len(matches))

	// Analyze each match
	for _, matchID := range matches {
		if err := vc.analyzeMatch(ctx, matchID); err != nil {
			log.Printf("Error analyzing match %s: %v", matchID, err)
		}
	}

	return nil
}

// analyzeMatch analyzes match for value bet
func (vc *ValueCalculator) analyzeMatch(ctx context.Context, matchID string) error {
	// Get all odds for match
	odds, err := vc.ydbClient.GetOddsByMatch(ctx, matchID)
	if err != nil {
		return fmt.Errorf("failed to get odds for match %s: %v", matchID, err)
	}

	if len(odds) == 0 {
		return nil
	}

	// Group odds by markets
	oddsByMarket := vc.groupOddsByMarket(odds)

	// Analyze each market
	for market, marketOdds := range oddsByMarket {
		if err := vc.analyzeMarket(ctx, matchID, market, marketOdds); err != nil {
			log.Printf("Error analyzing market %s for match %s: %v", market, matchID, err)
		}
	}

	return nil
}

// groupOddsByMarket groups odds by markets
func (vc *ValueCalculator) groupOddsByMarket(odds []*models.Odd) map[string][]*models.Odd {
	grouped := make(map[string][]*models.Odd)
	
	for _, odd := range odds {
		grouped[odd.Market] = append(grouped[odd.Market], odd)
	}
	
	return grouped
}

// analyzeMarket analyzes market for value bet
func (vc *ValueCalculator) analyzeMarket(ctx context.Context, matchID, market string, odds []*models.Odd) error {
	if len(odds) < 2 {
		return nil // Need at least 2 bookmakers for comparison
	}

	// Create reference data
	referenceData, err := vc.calculateReferenceData(odds)
	if err != nil {
		return fmt.Errorf("failed to calculate reference data: %v", err)
	}

	// Search value bet for each outcome
	for outcome, referenceOdd := range referenceData.ReferenceOdds {
		valueBets := vc.findValueBetsForOutcome(odds, outcome, referenceOdd)
		
		// Save found value bets
		for _, valueBet := range valueBets {
			if err := vc.saveValueBet(ctx, valueBet); err != nil {
				log.Printf("Failed to save value bet: %v", err)
			}
		}
	}

	return nil
}

// ReferenceData represents reference data
type ReferenceData struct {
	ReferenceOdds map[string]float64 `json:"reference_odds"` // outcome -> coefficient
	Source        string              `json:"source"`
}

// calculateReferenceData calculates reference coefficients
func (vc *ValueCalculator) calculateReferenceData(odds []*models.Odd) (*ReferenceData, error) {
	// Collect all outcomes and their coefficients
	allOutcomes := make(map[string][]float64) // outcome -> []coefficients
	
	for _, odd := range odds {
		for outcome, coefficient := range odd.Outcomes {
			allOutcomes[outcome] = append(allOutcomes[outcome], coefficient)
		}
	}

	// Calculate reference coefficients
	referenceOdds := make(map[string]float64)
	
	for outcome, coefficients := range allOutcomes {
		if len(coefficients) == 0 {
			continue
		}
		
		// Sort coefficients in descending order (highest coefficients)
		sort.Sort(sort.Reverse(sort.Float64Slice(coefficients)))
		
		// Calculate average of top-5 (or less if insufficient data)
		topCount := int(math.Min(5, float64(len(coefficients))))
		topCoefficients := coefficients[:topCount]
		
		// Arithmetic mean
		sum := 0.0
		for _, coef := range topCoefficients {
			sum += coef
		}
		referenceOdds[outcome] = sum / float64(len(topCoefficients))
	}

	return &ReferenceData{
		ReferenceOdds: referenceOdds,
		Source:        "average_top5",
	}, nil
}

// findValueBetsForOutcome searches value bet for specific outcome
func (vc *ValueCalculator) findValueBetsForOutcome(odds []*models.Odd, outcome string, referenceOdd float64) []*models.ValueBet {
	var valueBets []*models.ValueBet

	for _, odd := range odds {
		bookmakerOdd, exists := odd.Outcomes[outcome]
		if !exists {
			continue
		}

		// Calculate value
		valuePercent := vc.calculateValuePercent(bookmakerOdd, referenceOdd)
		
		// Check criteria
		if valuePercent >= vc.config.MinValuePercent {
			valueBet := &models.ValueBet{
				ID:              fmt.Sprintf("%s_%s_%s_%s", odd.MatchID, odd.Market, outcome, odd.Bookmaker),
				MatchID:         odd.MatchID,
				MatchName:       odd.MatchName,
				MatchTime:       odd.MatchTime,
				Sport:           odd.Sport,
				Market:          odd.Market,
				Outcome:         outcome,
				BookmakerOdd:    bookmakerOdd,
				ReferenceOdd:    referenceOdd,
				ValuePercent:    valuePercent,
				Bookmaker:       odd.Bookmaker,
				ReferenceSource: "average_top5",
				Stake:           float64(vc.config.MinStake),
				PotentialWin:    float64(vc.config.MinStake) * bookmakerOdd,
				FoundAt:         time.Now(),
				ExpiresAt:       time.Now().Add(30 * time.Minute), // 30 minutes for bet
			}
			
			valueBets = append(valueBets, valueBet)
		}
	}

	return valueBets
}

// calculateValuePercent calculates value percentage
func (vc *ValueCalculator) calculateValuePercent(bookmakerOdd, referenceOdd float64) float64 {
	if referenceOdd <= 0 {
		return 0
	}
	return ((bookmakerOdd / referenceOdd) - 1) * 100
}

// saveValueBet saves found value bet
func (vc *ValueCalculator) saveValueBet(ctx context.Context, valueBet *models.ValueBet) error {
	// In real implementation, this would be PostgreSQL storage
	// For now, just log
	log.Printf("🎯 VALUE BET FOUND!")
	log.Printf("   Match: %s", valueBet.MatchName)
	log.Printf("   Market: %s, Outcome: %s", valueBet.Market, valueBet.Outcome)
	log.Printf("   Bookmaker: %s (%.2f) vs Reference (%.2f)", 
		valueBet.Bookmaker, valueBet.BookmakerOdd, valueBet.ReferenceOdd)
	log.Printf("   Value: %.2f%%", valueBet.ValuePercent)
	log.Printf("   Stake: %.0f, Potential Win: %.0f", valueBet.Stake, valueBet.PotentialWin)
	log.Printf("   Expires: %s", valueBet.ExpiresAt.Format("15:04:05"))
	log.Printf("   ---")
	
	return nil
}
