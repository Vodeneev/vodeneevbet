package storage

import (
	"context"
	"fmt"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// YDBSimpleClient simplified client for YDB (пока без реализации)
type YDBSimpleClient struct {
	config *config.YDBConfig
}

func NewYDBSimpleClient(cfg *config.YDBConfig) (*YDBSimpleClient, error) {
	return &YDBSimpleClient{
		config: cfg,
	}, nil
}

// StoreOdd stores odd (заглушка)
func (y *YDBSimpleClient) StoreOdd(ctx context.Context, odd *models.Odd) error {
	// Just log that data was received
	fmt.Printf("YDB: Would store odd for match %s from %s: %+v\n", 
		odd.MatchID, odd.Bookmaker, odd.Outcomes)
	return nil
}

// GetOddsByMatch gets odds (заглушка)
func (y *YDBSimpleClient) GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error) {
	// Return empty list
	return []*models.Odd{}, nil
}

// GetAllMatches gets matches (заглушка)
func (y *YDBSimpleClient) GetAllMatches(ctx context.Context) ([]string, error) {
	// Return empty list
	return []string{}, nil
}

// StoreValueBet stores value bet (заглушка)
func (y *YDBSimpleClient) StoreArbitrage(ctx context.Context, arb *models.Arbitrage) error {
	fmt.Printf("YDB: Would store arbitrage %s with profit %.2f%%\n", 
		arb.ID, arb.ProfitPercent)
	return nil
}

// Close closes connection
func (y *YDBSimpleClient) Close() error {
	return nil
}
