package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"reflect"
	"time"

	_ "github.com/lib/pq"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
)

// Ensure PostgresDiffStorage implements DiffBetStorage
var _ DiffBetStorage = (*PostgresDiffStorage)(nil)

// PostgresDiffStorage stores DiffBet records in PostgreSQL
type PostgresDiffStorage struct {
	db *sql.DB
}

// NewPostgresDiffStorage creates a new PostgreSQL storage for diffs
func NewPostgresDiffStorage(cfg *config.PostgresConfig) (*PostgresDiffStorage, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("postgres DSN is required")
	}

	db, err := sql.Open("postgres", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	storage := &PostgresDiffStorage{db: db}
	
	// Initialize schema
	if err := storage.initSchema(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	log.Println("PostgreSQL diff storage initialized successfully")
	return storage, nil
}

func (s *PostgresDiffStorage) initSchema(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS diff_bets (
		id SERIAL PRIMARY KEY,
		match_group_key VARCHAR(500) NOT NULL,
		match_name VARCHAR(500) NOT NULL,
		start_time TIMESTAMP NOT NULL,
		sport VARCHAR(100) NOT NULL,
		event_type VARCHAR(100) NOT NULL,
		outcome_type VARCHAR(100) NOT NULL,
		parameter VARCHAR(100) NOT NULL DEFAULT '',
		bet_key VARCHAR(500) NOT NULL,
		bookmakers INTEGER NOT NULL,
		min_bookmaker VARCHAR(100) NOT NULL,
		min_odd DECIMAL(10, 4) NOT NULL,
		max_bookmaker VARCHAR(100) NOT NULL,
		max_odd DECIMAL(10, 4) NOT NULL,
		diff_abs DECIMAL(10, 4) NOT NULL,
		diff_percent DECIMAL(10, 4) NOT NULL,
		calculated_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT NOW(),
		UNIQUE(match_group_key, bet_key, calculated_at)
	);

	CREATE INDEX IF NOT EXISTS idx_diff_bets_match_group_key ON diff_bets(match_group_key);
	CREATE INDEX IF NOT EXISTS idx_diff_bets_bet_key ON diff_bets(bet_key);
	CREATE INDEX IF NOT EXISTS idx_diff_bets_calculated_at ON diff_bets(calculated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_diff_bets_diff_percent ON diff_bets(diff_percent DESC);
	CREATE INDEX IF NOT EXISTS idx_diff_bets_unique_check ON diff_bets(match_group_key, bet_key, calculated_at);
	`

	_, err := s.db.ExecContext(ctx, query)
	return err
}

// extractDiffBetFields extracts fields from a DiffBet-like struct using reflection
func extractDiffBetFields(diffInterface interface{}) (matchGroupKey, matchName, sport, eventType, outcomeType, parameter, betKey, minBookmaker, maxBookmaker string, startTime, calculatedAt time.Time, bookmakers int, minOdd, maxOdd, diffAbs, diffPercent float64, err error) {
	v := reflect.ValueOf(diffInterface)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", "", "", "", "", "", "", "", "", time.Time{}, time.Time{}, 0, 0, 0, 0, 0, fmt.Errorf("expected struct or pointer to struct, got %T", diffInterface)
	}

	getField := func(name string) reflect.Value {
		return v.FieldByName(name)
	}

	matchGroupKey = getField("MatchGroupKey").String()
	matchName = getField("MatchName").String()
	sport = getField("Sport").String()
	eventType = getField("EventType").String()
	outcomeType = getField("OutcomeType").String()
	parameter = getField("Parameter").String()
	betKey = getField("BetKey").String()
	minBookmaker = getField("MinBookmaker").String()
	maxBookmaker = getField("MaxBookmaker").String()
	bookmakers = int(getField("Bookmakers").Int())
	minOdd = getField("MinOdd").Float()
	maxOdd = getField("MaxOdd").Float()
	diffAbs = getField("DiffAbs").Float()
	diffPercent = getField("DiffPercent").Float()
	
	startTimeVal := getField("StartTime")
	if startTimeVal.IsValid() && startTimeVal.CanInterface() {
		if t, ok := startTimeVal.Interface().(time.Time); ok {
			startTime = t
		}
	}
	
	calculatedAtVal := getField("CalculatedAt")
	if calculatedAtVal.IsValid() && calculatedAtVal.CanInterface() {
		if t, ok := calculatedAtVal.Interface().(time.Time); ok {
			calculatedAt = t
		}
	}

	return
}

// StoreDiffBet stores a DiffBet record if it doesn't already exist
// Returns true if the record was newly inserted, false if it already existed
func (s *PostgresDiffStorage) StoreDiffBet(ctx context.Context, diffInterface interface{}) (bool, error) {
	matchGroupKey, matchName, sport, eventType, outcomeType, parameter, betKey, minBookmaker, maxBookmaker, startTime, calculatedAt, bookmakers, minOdd, maxOdd, diffAbs, diffPercent, err := extractDiffBetFields(diffInterface)
	if err != nil {
		return false, err
	}

	query := `
	INSERT INTO diff_bets (
		match_group_key, match_name, start_time, sport,
		event_type, outcome_type, parameter, bet_key,
		bookmakers, min_bookmaker, min_odd, max_bookmaker, max_odd,
		diff_abs, diff_percent, calculated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	ON CONFLICT (match_group_key, bet_key, calculated_at) DO NOTHING
	RETURNING id
	`

	var id int
	err = s.db.QueryRowContext(ctx, query,
		matchGroupKey,
		matchName,
		startTime,
		sport,
		eventType,
		outcomeType,
		parameter,
		betKey,
		bookmakers,
		minBookmaker,
		minOdd,
		maxBookmaker,
		maxOdd,
		diffAbs,
		diffPercent,
		calculatedAt,
	).Scan(&id)

	if err == sql.ErrNoRows {
		// Record already exists (conflict)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to store diff bet: %w", err)
	}

	return true, nil
}

// IsNewDiffBet checks if a diff bet is new (not seen in the last N minutes)
// Returns true if the diff is new, false if it was already seen recently
func (s *PostgresDiffStorage) IsNewDiffBet(ctx context.Context, diffInterface interface{}, withinMinutes int) (bool, error) {
	matchGroupKey, _, _, _, _, _, betKey, _, _, _, _, _, _, _, _, _, err := extractDiffBetFields(diffInterface)
	if err != nil {
		return false, err
	}

	query := `
	SELECT COUNT(*) FROM diff_bets
	WHERE match_group_key = $1 
	  AND bet_key = $2
	  AND calculated_at > NOW() - INTERVAL '%d minutes'
	`
	
	var count int
	err = s.db.QueryRowContext(ctx, fmt.Sprintf(query, withinMinutes),
		matchGroupKey,
		betKey,
	).Scan(&count)

	if err != nil {
		return false, fmt.Errorf("failed to check if diff is new: %w", err)
	}

	return count == 0, nil
}

// GetRecentDiffBets gets diff bets from the last N minutes
func (s *PostgresDiffStorage) GetRecentDiffBets(ctx context.Context, withinMinutes int, minDiffPercent float64) ([]interface{}, error) {
	query := `
	SELECT 
		match_group_key, match_name, start_time, sport,
		event_type, outcome_type, parameter, bet_key,
		bookmakers, min_bookmaker, min_odd, max_bookmaker, max_odd,
		diff_abs, diff_percent, calculated_at
	FROM diff_bets
	WHERE calculated_at > NOW() - INTERVAL '%d minutes'
	  AND diff_percent >= $1
	ORDER BY diff_percent DESC, calculated_at DESC
	`

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(query, withinMinutes), minDiffPercent)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent diff bets: %w", err)
	}
	defer rows.Close()

	// For GetRecentDiffBets, we return a map structure since we can't create the actual type
	// The caller will need to reconstruct the DiffBet from the map
	var diffs []interface{}
	for rows.Next() {
		var matchGroupKey, matchName, sport, eventType, outcomeType, parameter, betKey, minBookmaker, maxBookmaker string
		var startTime, calculatedAt time.Time
		var bookmakers int
		var minOdd, maxOdd, diffAbs, diffPercent float64
		
		err := rows.Scan(
			&matchGroupKey,
			&matchName,
			&startTime,
			&sport,
			&eventType,
			&outcomeType,
			&parameter,
			&betKey,
			&bookmakers,
			&minBookmaker,
			&minOdd,
			&maxBookmaker,
			&maxOdd,
			&diffAbs,
			&diffPercent,
			&calculatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan diff bet: %w", err)
		}
		
		// Return as map for now - caller can convert
		diffMap := map[string]interface{}{
			"match_group_key": matchGroupKey,
			"match_name":      matchName,
			"start_time":       startTime,
			"sport":            sport,
			"event_type":       eventType,
			"outcome_type":     outcomeType,
			"parameter":        parameter,
			"bet_key":          betKey,
			"bookmakers":       bookmakers,
			"min_bookmaker":    minBookmaker,
			"min_odd":          minOdd,
			"max_bookmaker":    maxBookmaker,
			"max_odd":          maxOdd,
			"diff_abs":         diffAbs,
			"diff_percent":     diffPercent,
			"calculated_at":    calculatedAt,
		}
		diffs = append(diffs, diffMap)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return diffs, nil
}

// Close closes the database connection
func (s *PostgresDiffStorage) Close() error {
	return s.db.Close()
}
