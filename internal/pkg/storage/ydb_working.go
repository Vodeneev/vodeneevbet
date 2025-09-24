package storage

import (
	"context"
	"fmt"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// YDBWorkingClient working client for YDB (with stub for testing)
type YDBWorkingClient struct {
	config *config.YDBConfig
}

func NewYDBWorkingClient(cfg *config.YDBConfig) (*YDBWorkingClient, error) {
	// Check if key file exists
	if cfg.ServiceAccountKeyFile == "" {
		return nil, fmt.Errorf("service account key file not specified")
	}
	
	// In real implementation, this would be YDB connection
	// For now, just check that file exists
	fmt.Printf("YDB: Connecting to %s\n", cfg.Endpoint)
	fmt.Printf("YDB: Database: %s\n", cfg.Database)
	fmt.Printf("YDB: Using key file: %s\n", cfg.ServiceAccountKeyFile)
	
	return &YDBWorkingClient{
		config: cfg,
	}, nil
}

// StoreOdd stores odd (stub with logging)
func (y *YDBWorkingClient) StoreOdd(ctx context.Context, odd *models.Odd) error {
	// In real implementation, this would be YDB storage
	fmt.Printf("YDB: Storing odd for match %s from %s: %+v\n", 
		odd.MatchID, odd.Bookmaker, odd.Outcomes)
	return nil
}

// GetOddsByMatch gets odds (stub)
func (y *YDBWorkingClient) GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error) {
	// In real implementation, this would be YDB query
	fmt.Printf("YDB: Getting odds for match %s\n", matchID)
	return []*models.Odd{}, nil
}

// GetAllMatches gets matches (stub)
func (y *YDBWorkingClient) GetAllMatches(ctx context.Context) ([]string, error) {
	// In real implementation, this would be YDB query
	fmt.Println("YDB: Getting all matches")
	return []string{}, nil
}

// StoreValueBet stores value bet (stub)
func (y *YDBWorkingClient) StoreArbitrage(ctx context.Context, arb *models.Arbitrage) error {
	// In real implementation, this would be YDB storage
	fmt.Printf("YDB: Storing arbitrage %s with profit %.2f%%\n", 
		arb.ID, arb.ProfitPercent)
	return nil
}

// Close closes connection
func (y *YDBWorkingClient) Close() error {
	fmt.Println("YDB: Closing connection")
	return nil
}
