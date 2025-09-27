package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// YDBClient provides YDB database operations
type YDBClient struct {
	config *config.YDBConfig
}

// NewYDBClient creates a new YDB client
func NewYDBClient(cfg *config.YDBConfig) (*YDBClient, error) {
	fmt.Printf("YDB: Connecting to %s\n", cfg.Endpoint)
	fmt.Printf("YDB: Database: %s\n", cfg.Database)
	
	return &YDBClient{
		config: cfg,
	}, nil
}

// StoreOdd stores betting odds in YDB
func (y *YDBClient) StoreOdd(ctx context.Context, odd *models.Odd) error {
	fmt.Printf("YDB: Storing odd for match %s from %s: %+v\n", 
		odd.MatchID, odd.Bookmaker, odd.Outcomes)
	return nil
}

// GetOddsByMatch retrieves odds for a specific match
func (y *YDBClient) GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error) {
	fmt.Printf("YDB: Getting odds for match %s\n", matchID)
	return []*models.Odd{}, nil
}

// GetAllMatches retrieves all available matches
func (y *YDBClient) GetAllMatches(ctx context.Context) ([]string, error) {
	fmt.Println("YDB: Getting all matches")
	return []string{}, nil
}

// GetAllOdds retrieves all odds from YDB
func (y *YDBClient) GetAllOdds(ctx context.Context) ([]*models.Odd, error) {
	fmt.Println("YDB: Getting all odds")
	// Mock data for testing - replace with real YDB query
	return []*models.Odd{
		{
			MatchID:   "match_1",
			Bookmaker: "Fonbet",
			Market:    "1x2",
			Outcomes: map[string]float64{
				"home": 1.85,
				"draw": 3.20,
				"away": 4.10,
			},
			UpdatedAt: time.Now(),
			MatchName: "Real Madrid vs Barcelona",
			MatchTime: time.Now().Add(2 * time.Hour),
			Sport:     "football",
		},
		{
			MatchID:   "match_2",
			Bookmaker: "Fonbet",
			Market:    "Corners",
			Outcomes: map[string]float64{
				"total_+5.5": 1.06,
				"total_-5.5": 10.0,
				"alt_total_+4.5": 1.5,
				"alt_total_-4.5": 2.6,
			},
			UpdatedAt: time.Now(),
			MatchName: "Manchester United vs Liverpool",
			MatchTime: time.Now().Add(4 * time.Hour),
			Sport:     "football",
		},
	}, nil
}

// StoreArbitrage stores arbitrage opportunity (stub for compatibility)
func (y *YDBClient) StoreArbitrage(ctx context.Context, arb *models.Arbitrage) error {
	fmt.Printf("YDB: Would store arbitrage %s with profit %.2f%%\n", 
		arb.ID, arb.ProfitPercent)
	return nil
}

// Close closes the YDB connection
func (y *YDBClient) Close() error {
	fmt.Println("YDB: Closing connection")
	return nil
}
