package calculator

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// ValueCalculator calculator for finding value bets
type ValueCalculator struct {
	ydbClient *storage.YDBClient
	config    *config.ValueCalculatorConfig
}

// NewValueCalculator creates new Value Bet calculator
func NewValueCalculator(ydbClient *storage.YDBClient, config *config.ValueCalculatorConfig) *ValueCalculator {
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
	for _, match := range matches {
		if err := vc.analyzeMatch(ctx, &match); err != nil {
			log.Printf("Error analyzing match %s: %v", match.ID, err)
		}
	}

	return nil
}

// analyzeMatch analyzes match for value bet
func (vc *ValueCalculator) analyzeMatch(ctx context.Context, match *models.Match) error {
	// Analyze each event in the match
	for _, event := range match.Events {
		if err := vc.analyzeEvent(ctx, match, &event); err != nil {
			log.Printf("Error analyzing event %s for match %s: %v", event.ID, match.ID, err)
		}
	}

	return nil
}

// analyzeEvent analyzes event for value bet
func (vc *ValueCalculator) analyzeEvent(ctx context.Context, match *models.Match, event *models.Event) error {
	if len(event.Outcomes) == 0 {
		return nil
	}

	// Convert outcomes to odds format for analysis
	odds := make(map[string]float64)
	for _, outcome := range event.Outcomes {
		odds[outcome.OutcomeType] = outcome.Odds
	}

	// Analyze outcomes for value bets
	for _, outcome := range event.Outcomes {
		if err := vc.analyzeOutcome(ctx, match, event, &outcome); err != nil {
			log.Printf("Error analyzing outcome %s: %v", outcome.ID, err)
		}
	}

	return nil
}

// analyzeOutcome analyzes specific outcome for value bet
func (vc *ValueCalculator) analyzeOutcome(ctx context.Context, match *models.Match, event *models.Event, outcome *models.Outcome) error {
	// For now, just log the outcome
	// In a real implementation, you would compare with reference odds
	log.Printf("Analyzing outcome: %s (%.2f) for event %s in match %s", 
		outcome.OutcomeType, outcome.Odds, event.ID, match.ID)
	
	return nil
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
