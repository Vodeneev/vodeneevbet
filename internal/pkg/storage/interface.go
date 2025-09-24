package storage

import (
	"context"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// Storage interface for working with odds storage
type Storage interface {
	// StoreOdd stores odd
	StoreOdd(ctx context.Context, odd *models.Odd) error
	
	// GetOddsByMatch gets all odds for match
	GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error)
	
	// GetAllMatches получает все доступные матчи
	GetAllMatches(ctx context.Context) ([]string, error)
	
	// Close closes connection
	Close() error
}

// ArbitrageStorage интерфейс для работы с арбитражами
type ArbitrageStorage interface {
	// StoreArbitrage сохраняет найденный арбитраж
	StoreArbitrage(ctx context.Context, arb *models.Arbitrage) error
	
	// GetArbitrages получает арбитражи по фильтрам
	GetArbitrages(ctx context.Context, limit int) ([]*models.Arbitrage, error)
}
