package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	_ "github.com/lib/pq"
)

// Ensure PostgresOddsSnapshotStorage implements OddsSnapshotStorage
var _ OddsSnapshotStorage = (*PostgresOddsSnapshotStorage)(nil)

// PostgresOddsSnapshotStorage stores odds snapshots for line movement (прогрузы) detection.
type PostgresOddsSnapshotStorage struct {
	db *sql.DB
}

// NewPostgresOddsSnapshotStorage creates a new PostgreSQL storage for odds snapshots.
func NewPostgresOddsSnapshotStorage(cfg *config.PostgresConfig) (*PostgresOddsSnapshotStorage, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("postgres DSN is required")
	}

	dsn, err := parseDSNForMultipleHosts(cfg.DSN)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	s := &PostgresOddsSnapshotStorage{db: db}
	if err := s.initSchema(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	slog.Info("PostgreSQL odds snapshot storage initialized successfully")
	return s, nil
}

func (s *PostgresOddsSnapshotStorage) initSchema(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS odds_snapshots (
		id SERIAL PRIMARY KEY,
		match_group_key VARCHAR(500) NOT NULL,
		match_name VARCHAR(500) NOT NULL,
		start_time TIMESTAMP NOT NULL,
		sport VARCHAR(100) NOT NULL,
		event_type VARCHAR(100) NOT NULL,
		outcome_type VARCHAR(100) NOT NULL,
		parameter VARCHAR(100) NOT NULL DEFAULT '',
		bet_key VARCHAR(500) NOT NULL,
		bookmaker VARCHAR(100) NOT NULL,
		odd DECIMAL(10, 4) NOT NULL,
		recorded_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		UNIQUE(match_group_key, bet_key, bookmaker)
	);

	CREATE INDEX IF NOT EXISTS idx_odds_snapshots_match_bet_bk ON odds_snapshots(match_group_key, bet_key, bookmaker);
	CREATE INDEX IF NOT EXISTS idx_odds_snapshots_start_time ON odds_snapshots(start_time);
	`
	_, err := s.db.ExecContext(ctx, query)
	return err
}

// StoreOddsSnapshot saves current odd for (match_group_key, bet_key, bookmaker).
// Uses UPSERT: one row per (match_group_key, bet_key, bookmaker), updated on each call.
func (s *PostgresOddsSnapshotStorage) StoreOddsSnapshot(ctx context.Context, matchGroupKey, matchName, sport, eventType, outcomeType, parameter, betKey, bookmaker string, startTime time.Time, odd float64, recordedAt time.Time) error {
	query := `
	INSERT INTO odds_snapshots (
		match_group_key, match_name, start_time, sport,
		event_type, outcome_type, parameter, bet_key,
		bookmaker, odd, recorded_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	ON CONFLICT (match_group_key, bet_key, bookmaker) DO UPDATE SET
		match_name = EXCLUDED.match_name,
		start_time = EXCLUDED.start_time,
		sport = EXCLUDED.sport,
		event_type = EXCLUDED.event_type,
		outcome_type = EXCLUDED.outcome_type,
		parameter = EXCLUDED.parameter,
		odd = EXCLUDED.odd,
		recorded_at = EXCLUDED.recorded_at
	`
	_, err := s.db.ExecContext(ctx, query,
		matchGroupKey, matchName, startTime, sport,
		eventType, outcomeType, parameter, betKey,
		bookmaker, odd, recordedAt,
	)
	return err
}

// GetLastOddsSnapshot returns the most recent odd and recordedAt for (match_group_key, bet_key, bookmaker).
func (s *PostgresOddsSnapshotStorage) GetLastOddsSnapshot(ctx context.Context, matchGroupKey, betKey, bookmaker string) (float64, time.Time, error) {
	query := `
	SELECT odd, recorded_at FROM odds_snapshots
	WHERE match_group_key = $1 AND bet_key = $2 AND bookmaker = $3
	`
	var odd float64
	var recordedAt time.Time
	err := s.db.QueryRowContext(ctx, query, matchGroupKey, betKey, bookmaker).Scan(&odd, &recordedAt)
	if err == sql.ErrNoRows {
		return 0, time.Time{}, nil
	}
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("failed to get last odds snapshot: %w", err)
	}
	return odd, recordedAt, nil
}

// CleanSnapshotsForStartedMatches deletes snapshots for matches that have already started.
func (s *PostgresOddsSnapshotStorage) CleanSnapshotsForStartedMatches(ctx context.Context) error {
	query := `DELETE FROM odds_snapshots WHERE start_time < NOW()`
	res, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to clean odds_snapshots: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows > 0 {
		slog.Info("Cleaned odds_snapshots for started matches", "rows_deleted", rows)
	}
	return nil
}

// Close closes the database connection.
func (s *PostgresOddsSnapshotStorage) Close() error {
	return s.db.Close()
}
