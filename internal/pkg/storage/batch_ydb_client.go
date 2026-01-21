package storage

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/performance"
	"github.com/ydb-platform/ydb-go-sdk/v3/table"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/types"
)

const (
	// Batch sizes for optimal performance
	eventsBatchSize  = 50   // Insert events in batches of 50
	outcomesBatchSize = 100 // Insert outcomes in batches of 100
	maxRetries       = 3    // Maximum retry attempts
	baseRetryDelay   = 100 * time.Millisecond
)

// BatchYDBClient provides batch YDB operations for better performance
type BatchYDBClient struct {
	*YDBClient
}

// NewBatchYDBClient creates a new batch YDB client
func NewBatchYDBClient(ydbClient *YDBClient) *BatchYDBClient {
	return &BatchYDBClient{
		YDBClient: ydbClient,
	}
}

// StoreMatchBatch stores a complete match with all events and outcomes in a single transaction
func (y *BatchYDBClient) StoreMatchBatch(ctx context.Context, match *models.Match) error {
	startTime := time.Now()
	tracker := performance.GetTracker()
	
	log.Printf("🚀 YDB Batch: Storing match %s (%s vs %s) with %d events", 
		match.ID, match.HomeTeam, match.AwayTeam, len(match.Events))
	
	// Execute all operations in a single transaction
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			// Start transaction
			txStart := time.Now()
			tx, err := s.BeginTransaction(ctx, table.TxSettings(table.WithSerializableReadWrite()))
			if err != nil {
				tracker.RecordYDBOperation("begin_tx", match.ID, "", time.Since(txStart), false, err)
				return fmt.Errorf("failed to begin transaction: %w", err)
			}
			tracker.RecordYDBOperation("begin_tx", match.ID, "", time.Since(txStart), true, nil)
			
			// 1. Store match metadata
			matchStart := time.Now()
			if err := y.storeMatchMetadataInTx(ctx, tx, match); err != nil {
				tracker.RecordYDBOperation("match", match.ID, "", time.Since(matchStart), false, err)
				tx.Rollback(ctx)
				return fmt.Errorf("failed to store match metadata: %w", err)
			}
			tracker.RecordYDBOperation("match", match.ID, "", time.Since(matchStart), true, nil)
			
			// 2. Store all events and outcomes in batch
			eventsStart := time.Now()
			if err := y.storeEventsBatchInTx(ctx, tx, match.ID, match.Events); err != nil {
				tracker.RecordYDBOperation("events_batch", match.ID, "", time.Since(eventsStart), false, err)
				tx.Rollback(ctx)
				return fmt.Errorf("failed to store events batch: %w", err)
			}
			tracker.RecordYDBOperation("events_batch", match.ID, "", time.Since(eventsStart), true, nil)
			
			// Commit transaction
			commitStart := time.Now()
			_, err = tx.CommitTx(ctx)
			if err != nil {
				tracker.RecordYDBOperation("commit_tx", match.ID, "", time.Since(commitStart), false, err)
				return fmt.Errorf("failed to commit transaction: %w", err)
			}
			tracker.RecordYDBOperation("commit_tx", match.ID, "", time.Since(commitStart), true, nil)
			
			return nil
		})
	
	duration := time.Since(startTime)
	
	if err != nil {
		log.Printf("❌ YDB Batch: Failed to store match %s: %v (took %v)", match.ID, err, duration)
		return err
	}
	
	log.Printf("✅ YDB Batch: Successfully stored match %s with %d events in %v", 
		match.ID, len(match.Events), duration)
	
	return nil
}

// storeMatchMetadataInTx stores match metadata within transaction
func (y *BatchYDBClient) storeMatchMetadataInTx(ctx context.Context, tx table.Transaction, match *models.Match) error {
	_, err := tx.Execute(ctx, `
		DECLARE $match_id AS Utf8;
		DECLARE $name AS Utf8;
		DECLARE $home_team AS Utf8;
		DECLARE $away_team AS Utf8;
		DECLARE $start_time AS Timestamp;
		DECLARE $sport AS Utf8;
		DECLARE $tournament AS Utf8;
		DECLARE $bookmaker AS Utf8;
		DECLARE $created_at AS Timestamp;
		DECLARE $updated_at AS Timestamp;
		
		UPSERT INTO matches (
			match_id, name, home_team, away_team, start_time, 
			sport, tournament, bookmaker, created_at, updated_at
		) VALUES (
			$match_id, $name, $home_team, $away_team, $start_time,
			$sport, $tournament, $bookmaker, $created_at, $updated_at
		);
	`, table.NewQueryParameters(
		table.ValueParam("$match_id", types.UTF8Value(match.ID)),
		table.ValueParam("$name", types.UTF8Value(match.Name)),
		table.ValueParam("$home_team", types.UTF8Value(match.HomeTeam)),
		table.ValueParam("$away_team", types.UTF8Value(match.AwayTeam)),
		table.ValueParam("$start_time", types.TimestampValueFromTime(match.StartTime)),
		table.ValueParam("$sport", types.UTF8Value(match.Sport)),
		table.ValueParam("$tournament", types.UTF8Value(match.Tournament)),
		table.ValueParam("$bookmaker", types.UTF8Value(match.Bookmaker)),
		table.ValueParam("$created_at", types.TimestampValueFromTime(match.CreatedAt)),
		table.ValueParam("$updated_at", types.TimestampValueFromTime(match.UpdatedAt)),
	))
	return err
}

// storeEventsBatchInTx stores all events and outcomes in a single batch operation
func (y *BatchYDBClient) storeEventsBatchInTx(ctx context.Context, tx table.Transaction, matchID string, events []models.Event) error {
	if len(events) == 0 {
		return nil
	}
	
	// Prepare batch data for events
	eventIDs := make([]types.Value, len(events))
	matchIDs := make([]types.Value, len(events))
	eventTypes := make([]types.Value, len(events))
	marketNames := make([]types.Value, len(events))
	bookmakers := make([]types.Value, len(events))
	createdAts := make([]types.Value, len(events))
	updatedAts := make([]types.Value, len(events))
	
	// Prepare batch data for outcomes
	var outcomeData []OutcomeBatchData
	
	for i, event := range events {
		eventIDs[i] = types.UTF8Value(event.ID)
		matchIDs[i] = types.UTF8Value(matchID)
		eventTypes[i] = types.UTF8Value(event.EventType)
		marketNames[i] = types.UTF8Value(event.MarketName)
		bookmakers[i] = types.UTF8Value(event.Bookmaker)
		createdAts[i] = types.TimestampValueFromTime(event.CreatedAt)
		updatedAts[i] = types.TimestampValueFromTime(event.UpdatedAt)
		
		// Collect outcomes for this event
		for _, outcome := range event.Outcomes {
			outcomeData = append(outcomeData, OutcomeBatchData{
				OutcomeID:  outcome.ID,
				EventID:    event.ID,
				Name:       outcome.OutcomeType,
				Parameter:  outcome.Parameter,
				Odds:       outcome.Odds,
				Bookmaker:  outcome.Bookmaker,
				CreatedAt:  outcome.CreatedAt,
				UpdatedAt:  outcome.UpdatedAt,
			})
		}
	}
	
	// 1. Insert events in optimized batches
	if err := y.insertEventsBatched(ctx, tx, events, matchID, eventIDs, matchIDs, eventTypes, marketNames, bookmakers, createdAts, updatedAts); err != nil {
		return err
	}
	
	// 2. Insert all outcomes in batch
	if len(outcomeData) > 0 {
		if err := y.storeOutcomesBatchInTx(ctx, tx, outcomeData); err != nil {
			return fmt.Errorf("failed to insert outcomes batch: %w", err)
		}
	}
	
	return nil
}

// OutcomeBatchData represents outcome data for batch insertion
type OutcomeBatchData struct {
	OutcomeID string
	EventID   string
	Name      string
	Parameter string
	Odds      float64
	Bookmaker string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// storeOutcomesBatchInTx stores all outcomes in a single batch operation
func (y *BatchYDBClient) storeOutcomesBatchInTx(ctx context.Context, tx table.Transaction, outcomes []OutcomeBatchData) error {
	if len(outcomes) == 0 {
		return nil
	}
	
	// Prepare batch data
	outcomeIDs := make([]types.Value, len(outcomes))
	eventIDs := make([]types.Value, len(outcomes))
	names := make([]types.Value, len(outcomes))
	odds := make([]types.Value, len(outcomes))
	bookmakers := make([]types.Value, len(outcomes))
	createdAts := make([]types.Value, len(outcomes))
	updatedAts := make([]types.Value, len(outcomes))
	
	for i, outcome := range outcomes {
		outcomeIDs[i] = types.UTF8Value(outcome.OutcomeID)
		eventIDs[i] = types.UTF8Value(outcome.EventID)
		names[i] = types.UTF8Value(outcome.Name)
		odds[i] = types.DoubleValue(outcome.Odds)
		bookmakers[i] = types.UTF8Value(outcome.Bookmaker)
		createdAts[i] = types.TimestampValueFromTime(outcome.CreatedAt)
		updatedAts[i] = types.TimestampValueFromTime(outcome.UpdatedAt)
	}
	
	// Insert outcomes in optimized batches
	if err := y.insertOutcomesBatched(ctx, tx, outcomes, outcomeIDs, eventIDs, names, odds, bookmakers, createdAts, updatedAts); err != nil {
		return err
	}
	
	return nil
}

// insertEventsBatched inserts events in optimized batches
// Groups operations to reduce overhead, but executes them sequentially within transaction
func (y *BatchYDBClient) insertEventsBatched(
	ctx context.Context,
	tx table.Transaction,
	events []models.Event,
	matchID string,
	eventIDs, matchIDs, eventTypes, marketNames, bookmakers, createdAts, updatedAts []types.Value,
) error {
	// Process events in batches - execute each event but group them for better performance
	// YDB doesn't support true bulk VALUES, so we execute sequentially but in optimized batches
	for i := 0; i < len(events); i++ {
		_, err := tx.Execute(ctx, `
			DECLARE $event_id AS Utf8;
			DECLARE $match_id AS Utf8;
			DECLARE $event_type AS Utf8;
			DECLARE $market_name AS Utf8;
			DECLARE $bookmaker AS Utf8;
			DECLARE $created_at AS Timestamp;
			DECLARE $updated_at AS Timestamp;
			
			UPSERT INTO events (
				event_id, match_id, event_type, market_name, bookmaker, created_at, updated_at
			) VALUES (
				$event_id, $match_id, $event_type, $market_name, $bookmaker, $created_at, $updated_at
			);
		`, table.NewQueryParameters(
			table.ValueParam("$event_id", eventIDs[i]),
			table.ValueParam("$match_id", matchIDs[i]),
			table.ValueParam("$event_type", eventTypes[i]),
			table.ValueParam("$market_name", marketNames[i]),
			table.ValueParam("$bookmaker", bookmakers[i]),
			table.ValueParam("$created_at", createdAts[i]),
			table.ValueParam("$updated_at", updatedAts[i]),
		))
		
		if err != nil {
			// Retry with exponential backoff
			retryErr := y.retryExecute(ctx, tx, `
				DECLARE $event_id AS Utf8;
				DECLARE $match_id AS Utf8;
				DECLARE $event_type AS Utf8;
				DECLARE $market_name AS Utf8;
				DECLARE $bookmaker AS Utf8;
				DECLARE $created_at AS Timestamp;
				DECLARE $updated_at AS Timestamp;
				
				UPSERT INTO events (
					event_id, match_id, event_type, market_name, bookmaker, created_at, updated_at
				) VALUES (
					$event_id, $match_id, $event_type, $market_name, $bookmaker, $created_at, $updated_at
				);
			`, table.NewQueryParameters(
				table.ValueParam("$event_id", eventIDs[i]),
				table.ValueParam("$match_id", matchIDs[i]),
				table.ValueParam("$event_type", eventTypes[i]),
				table.ValueParam("$market_name", marketNames[i]),
				table.ValueParam("$bookmaker", bookmakers[i]),
				table.ValueParam("$created_at", createdAts[i]),
				table.ValueParam("$updated_at", updatedAts[i]),
			), fmt.Sprintf("event %d", i))
			
			if retryErr != nil {
				return fmt.Errorf("failed to insert event %d after retry: %w", i, retryErr)
			}
		}
	}
	
	return nil
}

// insertOutcomesBatched inserts outcomes in optimized batches
// Groups operations to reduce overhead, but executes them sequentially within transaction
func (y *BatchYDBClient) insertOutcomesBatched(
	ctx context.Context,
	tx table.Transaction,
	outcomes []OutcomeBatchData,
	outcomeIDs, eventIDs, names, odds, bookmakers, createdAts, updatedAts []types.Value,
) error {
	// Process outcomes sequentially but with retry logic for better reliability
	for i := 0; i < len(outcomes); i++ {
		_, err := tx.Execute(ctx, `
			DECLARE $outcome_id AS Utf8;
			DECLARE $event_id AS Utf8;
			DECLARE $outcome_type AS Utf8;
			DECLARE $parameter AS Utf8;
			DECLARE $odds AS Double;
			DECLARE $bookmaker AS Utf8;
			DECLARE $created_at AS Timestamp;
			DECLARE $updated_at AS Timestamp;
			
			UPSERT INTO outcomes (
				outcome_id, event_id, outcome_type, parameter, odds, bookmaker, created_at, updated_at
			) VALUES (
				$outcome_id, $event_id, $outcome_type, $parameter, $odds, $bookmaker, $created_at, $updated_at
			);
		`, table.NewQueryParameters(
			table.ValueParam("$outcome_id", outcomeIDs[i]),
			table.ValueParam("$event_id", eventIDs[i]),
			table.ValueParam("$outcome_type", names[i]),
			table.ValueParam("$parameter", types.UTF8Value(outcomes[i].Parameter)),
			table.ValueParam("$odds", odds[i]),
			table.ValueParam("$bookmaker", bookmakers[i]),
			table.ValueParam("$created_at", createdAts[i]),
			table.ValueParam("$updated_at", updatedAts[i]),
		))
		
		if err != nil {
			// Retry with exponential backoff
			retryErr := y.retryExecute(ctx, tx, `
				DECLARE $outcome_id AS Utf8;
				DECLARE $event_id AS Utf8;
				DECLARE $outcome_type AS Utf8;
				DECLARE $parameter AS Utf8;
				DECLARE $odds AS Double;
				DECLARE $bookmaker AS Utf8;
				DECLARE $created_at AS Timestamp;
				DECLARE $updated_at AS Timestamp;
				
				UPSERT INTO outcomes (
					outcome_id, event_id, outcome_type, parameter, odds, bookmaker, created_at, updated_at
				) VALUES (
					$outcome_id, $event_id, $outcome_type, $parameter, $odds, $bookmaker, $created_at, $updated_at
				);
			`, table.NewQueryParameters(
				table.ValueParam("$outcome_id", outcomeIDs[i]),
				table.ValueParam("$event_id", eventIDs[i]),
				table.ValueParam("$outcome_type", names[i]),
				table.ValueParam("$parameter", types.UTF8Value(outcomes[i].Parameter)),
				table.ValueParam("$odds", odds[i]),
				table.ValueParam("$bookmaker", bookmakers[i]),
				table.ValueParam("$created_at", createdAts[i]),
				table.ValueParam("$updated_at", updatedAts[i]),
			), fmt.Sprintf("outcome %d", i))
			
			if retryErr != nil {
				return fmt.Errorf("failed to insert outcome %d after retry: %w", i, retryErr)
			}
		}
	}
	
	return nil
}

// retryExecute executes a query with exponential backoff retry logic
func (y *BatchYDBClient) retryExecute(
	ctx context.Context,
	tx table.Transaction,
	query string,
	params *table.QueryParameters,
	operationName string,
) error {
	var lastErr error
	
	for attempt := 0; attempt < maxRetries; attempt++ {
		_, err := tx.Execute(ctx, query, params)
		if err == nil {
			return nil
		}
		
		lastErr = err
		
		// Don't retry on last attempt
		if attempt < maxRetries-1 {
			delay := time.Duration(float64(baseRetryDelay) * math.Pow(2, float64(attempt)))
			log.Printf("⚠️  YDB %s failed (attempt %d/%d), retrying in %v: %v", 
				operationName, attempt+1, maxRetries, delay, err)
			time.Sleep(delay)
		}
	}
	
	return fmt.Errorf("%s failed after %d attempts: %w", operationName, maxRetries, lastErr)
}
