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

// OddsHistoryPoint is one recorded (odd, time) point for timeline in alerts.
type OddsHistoryPoint struct {
	Odd       float64
	RecordedAt time.Time
}

// OddsSnapshotStorage stores odds snapshots for line movement detection.
// Keeps max_odd and min_odd per (match, bet, bookmaker) so gradual moves (e.g. 4.15→4.0→3.45) are detected.
type OddsSnapshotStorage interface {
	// StoreOddsSnapshot saves current odd and updates max_odd/min_odd for (match_group_key, bet_key, bookmaker)
	StoreOddsSnapshot(ctx context.Context, matchGroupKey, matchName, sport, eventType, outcomeType, parameter, betKey, bookmaker string, startTime time.Time, odd float64, recordedAt time.Time) error
	// AppendOddsHistory appends one (odd, recordedAt) point for timeline; startTime is used for cleanup.
	AppendOddsHistory(ctx context.Context, matchGroupKey, betKey, bookmaker string, startTime time.Time, odd float64, recordedAt time.Time) error
	// GetOddsHistory returns recent points (oldest first), at most limit. Used to show "6.70 (12 min ago) → 7.10 (now)".
	GetOddsHistory(ctx context.Context, matchGroupKey, betKey, bookmaker string, limit int) ([]OddsHistoryPoint, error)
	// GetLastOddsSnapshot returns last odd, max and min seen, and recordedAt (0,0,0,zero time,nil if no row)
	GetLastOddsSnapshot(ctx context.Context, matchGroupKey, betKey, bookmaker string) (odd, maxOdd, minOdd float64, recordedAt time.Time, err error)
	// ResetExtremesAfterAlert sets max_odd=odd and min_odd=odd for the row so we don't re-alert on same range
	ResetExtremesAfterAlert(ctx context.Context, matchGroupKey, betKey, bookmaker string) error
	// CleanSnapshotsForStartedMatches deletes snapshots and history for matches that have already started (start_time < now)
	CleanSnapshotsForStartedMatches(ctx context.Context) error
	Close() error
}
