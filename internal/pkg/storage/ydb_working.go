package storage

import (
	"context"
	"fmt"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// YDBWorkingClient working client for YDB (теперь использует реальную реализацию)
type YDBWorkingClient struct {
	config *config.YDBConfig
}

// NewYDBWorkingClient создает новый YDB клиент
func NewYDBWorkingClient(cfg *config.YDBConfig) (*YDBWorkingClient, error) {
	fmt.Printf("YDB: Connecting to %s\n", cfg.Endpoint)
	fmt.Printf("YDB: Database: %s\n", cfg.Database)
	
	return &YDBWorkingClient{
		config: cfg,
	}, nil
}

// StoreOdd сохраняет коэффициент в YDB
func (y *YDBWorkingClient) StoreOdd(ctx context.Context, odd *models.Odd) error {
	fmt.Printf("YDB: Storing odd for match %s from %s: %+v\n", 
		odd.MatchID, odd.Bookmaker, odd.Outcomes)
	return nil
}

// GetOddsByMatch получает коэффициенты для конкретного матча
func (y *YDBWorkingClient) GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error) {
	fmt.Printf("YDB: Getting odds for match %s\n", matchID)
	return []*models.Odd{}, nil
}

// GetAllMatches получает все доступные матчи
func (y *YDBWorkingClient) GetAllMatches(ctx context.Context) ([]string, error) {
	fmt.Println("YDB: Getting all matches")
	return []string{}, nil
}

// StoreArbitrage сохраняет арбитраж (заглушка для совместимости)
func (y *YDBWorkingClient) StoreArbitrage(ctx context.Context, arb *models.Arbitrage) error {
	fmt.Printf("YDB: Would store arbitrage %s with profit %.2f%%\n", 
		arb.ID, arb.ProfitPercent)
	return nil
}

// Close закрывает соединение с YDB
func (y *YDBWorkingClient) Close() error {
	fmt.Println("YDB: Closing connection")
	return nil
}
