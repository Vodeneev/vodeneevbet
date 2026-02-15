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
		max_odd DECIMAL(10, 4),
		min_odd DECIMAL(10, 4),
		recorded_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		UNIQUE(match_group_key, bet_key, bookmaker)
	);

	CREATE INDEX IF NOT EXISTS idx_odds_snapshots_match_bet_bk ON odds_snapshots(match_group_key, bet_key, bookmaker);
	CREATE INDEX IF NOT EXISTS idx_odds_snapshots_start_time ON odds_snapshots(start_time);
	`
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return err
	}
	// Migration: add max_odd/min_odd if table existed without them
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE odds_snapshots ADD COLUMN IF NOT EXISTS max_odd DECIMAL(10, 4)`)
	_, _ = s.db.ExecContext(ctx, `ALTER TABLE odds_snapshots ADD COLUMN IF NOT EXISTS min_odd DECIMAL(10, 4)`)
	_, _ = s.db.ExecContext(ctx, `UPDATE odds_snapshots SET max_odd = odd WHERE max_odd IS NULL`)
	_, _ = s.db.ExecContext(ctx, `UPDATE odds_snapshots SET min_odd = odd WHERE min_odd IS NULL`)

	// History of (odd, time) per key for timeline in alerts
	historyQuery := `
	CREATE TABLE IF NOT EXISTS odds_snapshot_history (
		id SERIAL PRIMARY KEY,
		match_group_key VARCHAR(500) NOT NULL,
		bet_key VARCHAR(500) NOT NULL,
		bookmaker VARCHAR(100) NOT NULL,
		odd DECIMAL(10, 4) NOT NULL,
		recorded_at TIMESTAMP NOT NULL,
		start_time TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_odds_snapshot_history_key ON odds_snapshot_history(match_group_key, bet_key, bookmaker);
	CREATE INDEX IF NOT EXISTS idx_odds_snapshot_history_start ON odds_snapshot_history(start_time);
	`
	_, _ = s.db.ExecContext(ctx, historyQuery)
	return nil
}

// StoreOddsSnapshot saves current odd and updates max_odd/min_odd for (match_group_key, bet_key, bookmaker).
func (s *PostgresOddsSnapshotStorage) StoreOddsSnapshot(ctx context.Context, matchGroupKey, matchName, sport, eventType, outcomeType, parameter, betKey, bookmaker string, startTime time.Time, odd float64, recordedAt time.Time) error {
	query := `
	INSERT INTO odds_snapshots (
		match_group_key, match_name, start_time, sport,
		event_type, outcome_type, parameter, bet_key,
		bookmaker, odd, max_odd, min_odd, recorded_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $10, $11)
	ON CONFLICT (match_group_key, bet_key, bookmaker) DO UPDATE SET
		match_name = EXCLUDED.match_name,
		start_time = EXCLUDED.start_time,
		sport = EXCLUDED.sport,
		event_type = EXCLUDED.event_type,
		outcome_type = EXCLUDED.outcome_type,
		parameter = EXCLUDED.parameter,
		odd = EXCLUDED.odd,
		max_odd = GREATEST(COALESCE(odds_snapshots.max_odd, odds_snapshots.odd), EXCLUDED.odd),
		min_odd = LEAST(COALESCE(odds_snapshots.min_odd, odds_snapshots.odd), EXCLUDED.odd),
		recorded_at = EXCLUDED.recorded_at
	`
	_, err := s.db.ExecContext(ctx, query,
		matchGroupKey, matchName, startTime, sport,
		eventType, outcomeType, parameter, betKey,
		bookmaker, odd, recordedAt,
	)
	return err
}

// GetLastOddsSnapshot returns last odd, max and min seen, and recordedAt for (match_group_key, bet_key, bookmaker).
func (s *PostgresOddsSnapshotStorage) GetLastOddsSnapshot(ctx context.Context, matchGroupKey, betKey, bookmaker string) (odd, maxOdd, minOdd float64, recordedAt time.Time, err error) {
	query := `
	SELECT odd, COALESCE(max_odd, odd), COALESCE(min_odd, odd), recorded_at FROM odds_snapshots
	WHERE match_group_key = $1 AND bet_key = $2 AND bookmaker = $3
	`
	err = s.db.QueryRowContext(ctx, query, matchGroupKey, betKey, bookmaker).Scan(&odd, &maxOdd, &minOdd, &recordedAt)
	if err == sql.ErrNoRows {
		return 0, 0, 0, time.Time{}, nil
	}
	if err != nil {
		return 0, 0, 0, time.Time{}, fmt.Errorf("failed to get last odds snapshot: %w", err)
	}
	return odd, maxOdd, minOdd, recordedAt, nil
}

// AppendOddsHistory appends one (odd, recordedAt) point for timeline.
func (s *PostgresOddsSnapshotStorage) AppendOddsHistory(ctx context.Context, matchGroupKey, betKey, bookmaker string, startTime time.Time, odd float64, recordedAt time.Time) error {
	query := `INSERT INTO odds_snapshot_history (match_group_key, bet_key, bookmaker, odd, recorded_at, start_time) VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := s.db.ExecContext(ctx, query, matchGroupKey, betKey, bookmaker, odd, recordedAt, startTime)
	return err
}

// GetOddsHistory returns recent points in chronological order (oldest first), at most limit.
func (s *PostgresOddsSnapshotStorage) GetOddsHistory(ctx context.Context, matchGroupKey, betKey, bookmaker string, limit int) ([]OddsHistoryPoint, error) {
	if limit <= 0 {
		limit = 30
	}
	query := `
	SELECT odd, recorded_at FROM (
		SELECT odd, recorded_at FROM odds_snapshot_history
		WHERE match_group_key = $1 AND bet_key = $2 AND bookmaker = $3
		ORDER BY recorded_at DESC
		LIMIT $4
	) sub ORDER BY recorded_at ASC
	`
	rows, err := s.db.QueryContext(ctx, query, matchGroupKey, betKey, bookmaker, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OddsHistoryPoint
	for rows.Next() {
		var p OddsHistoryPoint
		if err := rows.Scan(&p.Odd, &p.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ResetExtremesAfterAlert sets max_odd=odd and min_odd=odd so next comparison is from current baseline (no re-alert spam).
func (s *PostgresOddsSnapshotStorage) ResetExtremesAfterAlert(ctx context.Context, matchGroupKey, betKey, bookmaker string) error {
	query := `UPDATE odds_snapshots SET max_odd = odd, min_odd = odd WHERE match_group_key = $1 AND bet_key = $2 AND bookmaker = $3`
	_, err := s.db.ExecContext(ctx, query, matchGroupKey, betKey, bookmaker)
	return err
}

// CleanSnapshotsForStartedMatches deletes snapshots and history for matches that have already started.
func (s *PostgresOddsSnapshotStorage) CleanSnapshotsForStartedMatches(ctx context.Context) error {
	res1, err := s.db.ExecContext(ctx, `DELETE FROM odds_snapshots WHERE start_time < NOW()`)
	if err != nil {
		return fmt.Errorf("failed to clean odds_snapshots: %w", err)
	}
	res2, err := s.db.ExecContext(ctx, `DELETE FROM odds_snapshot_history WHERE start_time < NOW()`)
	if err != nil {
		return fmt.Errorf("failed to clean odds_snapshot_history: %w", err)
	}
	n1, _ := res1.RowsAffected()
	n2, _ := res2.RowsAffected()
	if n1 > 0 || n2 > 0 {
		slog.Info("Cleaned odds snapshots for started matches", "snapshots_deleted", n1, "history_deleted", n2)
	}
	return nil
}

// Close closes the database connection.
func (s *PostgresOddsSnapshotStorage) Close() error {
	return s.db.Close()
}
