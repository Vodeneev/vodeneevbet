package storage

import (
	"context"
	"fmt"

	"vodeneevbet/internal/pkg/config"
	"vodeneevbet/internal/pkg/models"
)

// YDBSimpleClient упрощенный клиент для YDB (пока без реализации)
type YDBSimpleClient struct {
	config *config.YDBConfig
}

func NewYDBSimpleClient(cfg *config.YDBConfig) (*YDBSimpleClient, error) {
	return &YDBSimpleClient{
		config: cfg,
	}, nil
}

// StoreOdd сохраняет коэффициент (заглушка)
func (y *YDBSimpleClient) StoreOdd(ctx context.Context, odd *models.Odd) error {
	// Пока просто логируем, что данные получены
	fmt.Printf("YDB: Would store odd for match %s from %s: %+v\n", 
		odd.MatchID, odd.Bookmaker, odd.Outcomes)
	return nil
}

// GetOddsByMatch получает коэффициенты (заглушка)
func (y *YDBSimpleClient) GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error) {
	// Возвращаем пустой список
	return []*models.Odd{}, nil
}

// GetAllMatches получает матчи (заглушка)
func (y *YDBSimpleClient) GetAllMatches(ctx context.Context) ([]string, error) {
	// Возвращаем пустой список
	return []string{}, nil
}

// StoreArbitrage сохраняет арбитраж (заглушка)
func (y *YDBSimpleClient) StoreArbitrage(ctx context.Context, arb *models.Arbitrage) error {
	fmt.Printf("YDB: Would store arbitrage %s with profit %.2f%%\n", 
		arb.ID, arb.ProfitPercent)
	return nil
}

// Close закрывает соединение
func (y *YDBSimpleClient) Close() error {
	return nil
}
