package storage

import (
	"context"
	"time"
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

// DiffBetStorage interface for working with diff bet data
type DiffBetStorage interface {
	// StoreDiffBet stores a DiffBet record
	// Returns true if the record was newly inserted, false if it already existed
	StoreDiffBet(ctx context.Context, diff interface{}) (bool, error)
	
	// IsNewDiffBet checks if a diff bet is new (not seen recently)
	IsNewDiffBet(ctx context.Context, diff interface{}, withinMinutes int) (bool, error)
	
	// GetRecentDiffBets gets diff bets from the last N minutes
	GetRecentDiffBets(ctx context.Context, withinMinutes int, minDiffPercent float64) ([]interface{}, error)
	
	// GetLastDiffBet gets the most recent diff bet for a specific match+bet combination
	// Excludes diffs with calculated_at equal to excludeCalculatedAt (to avoid getting the current diff)
	// Returns the diff_percent and calculated_at, or (0, zero time, nil) if not found
	GetLastDiffBet(ctx context.Context, matchGroupKey, betKey string, excludeCalculatedAt time.Time) (diffPercent float64, calculatedAt time.Time, err error)
	
	// CleanDiffBets removes all records from diff_bets table
	// Useful for clearing old data on service restart
	CleanDiffBets(ctx context.Context) error
	
	// Close closes the database connection
	Close() error
}

// OddsSnapshotStorage stores odds snapshots for line movement (прогрузы) detection.
// Same bookmaker, same bet: compare current odd with previous to detect significant drops.
type OddsSnapshotStorage interface {
	// StoreOddsSnapshot saves current odd for (match_group_key, bet_key, bookmaker)
	StoreOddsSnapshot(ctx context.Context, matchGroupKey, matchName, sport, eventType, outcomeType, parameter, betKey, bookmaker string, startTime time.Time, odd float64, recordedAt time.Time) error
	// GetLastOddsSnapshot returns the most recent odd and recordedAt for (match_group_key, bet_key, bookmaker)
	GetLastOddsSnapshot(ctx context.Context, matchGroupKey, betKey, bookmaker string) (odd float64, recordedAt time.Time, err error)
	// CleanSnapshotsForStartedMatches deletes snapshots for matches that have already started (start_time < now)
	CleanSnapshotsForStartedMatches(ctx context.Context) error
	Close() error
}
