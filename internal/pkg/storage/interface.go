package storage

import (
	"context"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// Storage interface for working with match data storage
type Storage interface {
	// StoreMatch stores a complete match with all its events and outcomes
	StoreMatch(ctx context.Context, match *models.Match) error
	
	// GetMatch retrieves a complete match with all events and outcomes
	GetMatch(ctx context.Context, matchID string) (*models.Match, error)
	
	// GetAllMatches retrieves all matches with their events and outcomes
	GetAllMatches(ctx context.Context) ([]models.Match, error)
	
	// GetMatchesWithLimit retrieves matches with a limit to avoid timeout
	GetMatchesWithLimit(ctx context.Context, limit int) ([]models.Match, error)
	
	// CleanTable removes all data from a table
	CleanTable(ctx context.Context, tableName string) error
	
	// Close closes the database connection
	Close() error
}

// ArbitrageStorage interface for working with arbitrage data
type ArbitrageStorage interface {
	// StoreArbitrage saves found arbitrage
	StoreArbitrage(ctx context.Context, arb interface{}) error
	
	// GetArbitrages gets arbitrages by filters
	GetArbitrages(ctx context.Context, limit int) ([]interface{}, error)
}

// ValueBetStorage interface for working with value bet data
type ValueBetStorage interface {
	// StoreValueBet saves found value bet
	StoreValueBet(ctx context.Context, valueBet *models.ValueBet) error
	
	// GetValueBets gets value bets by filters
	GetValueBets(ctx context.Context, limit int) ([]*models.ValueBet, error)
	
	// GetValueBetStats gets value bet statistics
	GetValueBetStats(ctx context.Context) (interface{}, error)
}
