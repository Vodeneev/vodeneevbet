package storage

import (
	"context"
	"vodeneevbet/internal/pkg/models"
)

// Storage интерфейс для работы с хранилищем коэффициентов
type Storage interface {
	// StoreOdd сохраняет коэффициент
	StoreOdd(ctx context.Context, odd *models.Odd) error
	
	// GetOddsByMatch получает все коэффициенты для матча
	GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error)
	
	// GetAllMatches получает все доступные матчи
	GetAllMatches(ctx context.Context) ([]string, error)
	
	// Close закрывает соединение
	Close() error
}

// ArbitrageStorage интерфейс для работы с арбитражами
type ArbitrageStorage interface {
	// StoreArbitrage сохраняет найденный арбитраж
	StoreArbitrage(ctx context.Context, arb *models.Arbitrage) error
	
	// GetArbitrages получает арбитражи по фильтрам
	GetArbitrages(ctx context.Context, limit int) ([]*models.Arbitrage, error)
}
