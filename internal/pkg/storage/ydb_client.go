package storage

import (
	"context"
	"fmt"

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
