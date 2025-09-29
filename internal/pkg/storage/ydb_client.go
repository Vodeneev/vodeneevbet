package storage

import (
	"context"
	"fmt"
	"log"
	"path"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/table"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/options"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/result"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/result/named"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/types"
	"github.com/ydb-platform/ydb-go-yc"
	ycMetadata "github.com/ydb-platform/ydb-go-yc-metadata"
)

// YDBClient provides YDB operations for hierarchical match data
type YDBClient struct {
	db     ydb.Connection
	config *config.YDBConfig
}

// NewYDBClient creates a new YDB client for match events
func NewYDBClient(cfg *config.YDBConfig) (*YDBClient, error) {
	ctx := context.Background()
	
	// Create connection string
	dsn := fmt.Sprintf("%s?database=%s", cfg.Endpoint, cfg.Database)
	
	log.Printf("YDB: Connecting to %s", dsn)
	
	// Create database connection object
	var db ydb.Connection
	var err error
	
	// Determine connection type by endpoint
	if cfg.Endpoint == "grpcs://ydb.serverless.yandexcloud.net:2135" {
		// Connect to Yandex Cloud YDB
		if cfg.ServiceAccountKeyFile != "" {
			// External connection with Service Account key
			log.Println("YDB: Using external connection with Service Account key")
			db, err = ydb.Open(ctx, dsn,
				yc.WithInternalCA(),
				yc.WithServiceAccountKeyFileCredentials(cfg.ServiceAccountKeyFile),
			)
		} else {
			// Internal connection from Yandex Cloud (Cloud Functions, VM)
			log.Println("YDB: Using internal connection with metadata credentials")
			db, err = ydb.Open(ctx, dsn,
				ycMetadata.WithInternalCA(),
				ycMetadata.WithCredentials(),
			)
		}
	} else {
		// Local connection (for testing)
		log.Println("YDB: Using local connection with anonymous credentials")
		db, err = ydb.Open(ctx, dsn,
			ydb.WithAnonymousCredentials(),
		)
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to connect to YDB: %w", err)
	}
	
	log.Printf("YDB: Successfully connected to database: %s", cfg.Database)
	
	client := &YDBClient{
		db:     db,
		config: cfg,
	}
	
	// Create tables if they don't exist
	if err := client.createTablesIfNotExist(ctx); err != nil {
		log.Printf("YDB: Warning - failed to create tables: %v", err)
	}
	
	return client, nil
}

// StoreMatch stores a complete match with all its events and outcomes
func (y *YDBClient) StoreMatch(ctx context.Context, match *models.Match) error {
	startTime := time.Now()
	log.Printf("YDB: Storing match %s (%s vs %s) with %d events", 
		match.ID, match.HomeTeam, match.AwayTeam, len(match.Events))
	
	// Store match metadata
	metadataStart := time.Now()
	if err := y.storeMatchMetadata(ctx, match); err != nil {
		return fmt.Errorf("failed to store match metadata: %w", err)
	}
	metadataDuration := time.Since(metadataStart)
	
	// Store events
	eventsStart := time.Now()
	for _, event := range match.Events {
		eventStart := time.Now()
		if err := y.storeEvent(ctx, match.ID, &event); err != nil {
			return fmt.Errorf("failed to store event %s: %w", event.ID, err)
		}
		eventDuration := time.Since(eventStart)
		if eventDuration > 50*time.Millisecond {
			log.Printf("⏱️  YDB Event %s took: %v", event.ID, eventDuration)
		}
	}
	eventsDuration := time.Since(eventsStart)
	
	totalDuration := time.Since(startTime)
	log.Printf("⏱️  YDB StoreMatch %s: metadata=%v, events=%v, total=%v", 
		match.ID, metadataDuration, eventsDuration, totalDuration)
	log.Printf("YDB: Successfully stored match %s with %d events", match.ID, len(match.Events))
	return nil
}

// storeMatchMetadata stores match basic information
func (y *YDBClient) storeMatchMetadata(ctx context.Context, match *models.Match) error {
	return y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			_, _, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithSerializableReadWrite()),
				table.CommitTx(),
			), `
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
		})
}

// storeEvent stores an event with all its outcomes
func (y *YDBClient) storeEvent(ctx context.Context, matchID string, event *models.Event) error {
	// Store event metadata
	if err := y.storeEventMetadata(ctx, matchID, event); err != nil {
		return err
	}
	
	// Store outcomes
	for _, outcome := range event.Outcomes {
		if err := y.storeOutcome(ctx, event.ID, &outcome); err != nil {
			return fmt.Errorf("failed to store outcome %s: %w", outcome.ID, err)
		}
	}
	
	return nil
}

// storeEventMetadata stores event basic information
func (y *YDBClient) storeEventMetadata(ctx context.Context, matchID string, event *models.Event) error {
	return y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			_, _, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithSerializableReadWrite()),
				table.CommitTx(),
			), `
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
				table.ValueParam("$event_id", types.UTF8Value(event.ID)),
				table.ValueParam("$match_id", types.UTF8Value(matchID)),
				table.ValueParam("$event_type", types.UTF8Value(string(event.EventType))),
				table.ValueParam("$market_name", types.UTF8Value(event.MarketName)),
				table.ValueParam("$bookmaker", types.UTF8Value(event.Bookmaker)),
				table.ValueParam("$created_at", types.TimestampValueFromTime(event.CreatedAt)),
				table.ValueParam("$updated_at", types.TimestampValueFromTime(event.UpdatedAt)),
			))
			return err
		})
}

// storeOutcome stores a single outcome
func (y *YDBClient) storeOutcome(ctx context.Context, eventID string, outcome *models.Outcome) error {
	return y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			_, _, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithSerializableReadWrite()),
				table.CommitTx(),
			), `
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
				table.ValueParam("$outcome_id", types.UTF8Value(outcome.ID)),
				table.ValueParam("$event_id", types.UTF8Value(eventID)),
				table.ValueParam("$outcome_type", types.UTF8Value(string(outcome.OutcomeType))),
				table.ValueParam("$parameter", types.UTF8Value(outcome.Parameter)),
				table.ValueParam("$odds", types.DoubleValue(outcome.Odds)),
				table.ValueParam("$bookmaker", types.UTF8Value(outcome.Bookmaker)),
				table.ValueParam("$created_at", types.TimestampValueFromTime(outcome.CreatedAt)),
				table.ValueParam("$updated_at", types.TimestampValueFromTime(outcome.UpdatedAt)),
			))
			return err
		})
}

// GetMatch retrieves a complete match with all events and outcomes
func (y *YDBClient) GetMatch(ctx context.Context, matchID string) (*models.Match, error) {
	log.Printf("YDB: Getting match %s", matchID)
	
	// Get match metadata
	match, err := y.getMatchMetadata(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get match metadata: %w", err)
	}
	
	// Get events for this match
	events, err := y.getEventsForMatch(ctx, matchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	
	match.Events = events
	return match, nil
}

// getMatchMetadata retrieves match basic information
func (y *YDBClient) getMatchMetadata(ctx context.Context, matchID string) (*models.Match, error) {
	var match *models.Match
	
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			var res result.Result
			_, res, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithOnlineReadOnly()),
				table.CommitTx(),
			), `
				SELECT match_id, name, home_team, away_team, start_time, sport, tournament, bookmaker, created_at, updated_at
				FROM matches
				WHERE match_id = $match_id;
			`, table.NewQueryParameters(
				table.ValueParam("$match_id", types.UTF8Value(matchID)),
			))
			if err != nil {
				return err
			}
			defer res.Close()
			
			if res.NextResultSet(ctx) && res.NextRow() {
				match = &models.Match{}
				err = res.ScanNamed(
					named.Required("match_id", &match.ID),
					named.Required("name", &match.Name),
					named.Required("home_team", &match.HomeTeam),
					named.Required("away_team", &match.AwayTeam),
					named.Required("start_time", &match.StartTime),
					named.Required("sport", &match.Sport),
					named.Required("tournament", &match.Tournament),
					named.Required("bookmaker", &match.Bookmaker),
					named.Required("created_at", &match.CreatedAt),
					named.Required("updated_at", &match.UpdatedAt),
				)
				if err != nil {
					return err
				}
			}
			return res.Err()
		})
	
	if err != nil {
		return nil, err
	}
	
	if match == nil {
		return nil, fmt.Errorf("match %s not found", matchID)
	}
	
	return match, nil
}

// getEventsForMatch retrieves all events for a match
func (y *YDBClient) getEventsForMatch(ctx context.Context, matchID string) ([]models.Event, error) {
	var events []models.Event
	
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			var res result.Result
			_, res, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithOnlineReadOnly()),
				table.CommitTx(),
			), `
				SELECT event_id, event_type, market_name, bookmaker, created_at, updated_at
				FROM events
				WHERE match_id = $match_id;
			`, table.NewQueryParameters(
				table.ValueParam("$match_id", types.UTF8Value(matchID)),
			))
			if err != nil {
				return err
			}
			defer res.Close()
			
			for res.NextResultSet(ctx) {
				for res.NextRow() {
					event := models.Event{}
					var eventTypeStr string
					err = res.ScanNamed(
						named.Required("event_id", &event.ID),
						named.Required("event_type", &eventTypeStr),
						named.Required("market_name", &event.MarketName),
						named.Required("bookmaker", &event.Bookmaker),
						named.Required("created_at", &event.CreatedAt),
						named.Required("updated_at", &event.UpdatedAt),
					)
					if err != nil {
						return err
					}
					
					// Convert string to StandardEventType
					event.EventType = string(models.StandardEventType(eventTypeStr))
					
					// Get outcomes for this event
					outcomes, err := y.getOutcomesForEvent(ctx, event.ID)
					if err != nil {
						return fmt.Errorf("failed to get outcomes for event %s: %w", event.ID, err)
					}
					event.Outcomes = outcomes
					
					events = append(events, event)
				}
			}
			return res.Err()
		})
	
	return events, err
}

// getOutcomesForEvent retrieves all outcomes for an event
func (y *YDBClient) getOutcomesForEvent(ctx context.Context, eventID string) ([]models.Outcome, error) {
	var outcomes []models.Outcome
	
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			var res result.Result
			_, res, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithOnlineReadOnly()),
				table.CommitTx(),
			), `
				SELECT outcome_id, outcome_type, parameter, odds, bookmaker, created_at, updated_at
				FROM outcomes
				WHERE event_id = $event_id;
			`, table.NewQueryParameters(
				table.ValueParam("$event_id", types.UTF8Value(eventID)),
			))
			if err != nil {
				return err
			}
			defer res.Close()
			
			for res.NextResultSet(ctx) {
				for res.NextRow() {
					outcome := models.Outcome{}
					var outcomeTypeStr string
					err = res.ScanNamed(
						named.Required("outcome_id", &outcome.ID),
						named.Required("outcome_type", &outcomeTypeStr),
						named.Required("parameter", &outcome.Parameter),
						named.Required("odds", &outcome.Odds),
						named.Required("bookmaker", &outcome.Bookmaker),
						named.Required("created_at", &outcome.CreatedAt),
						named.Required("updated_at", &outcome.UpdatedAt),
					)
					if err != nil {
						return err
					}
					
					// Convert string to StandardOutcomeType
					outcome.OutcomeType = string(models.StandardOutcomeType(outcomeTypeStr))
					
					outcomes = append(outcomes, outcome)
				}
			}
			return res.Err()
		})
	
	return outcomes, err
}

// GetAllMatches retrieves all matches with their events and outcomes
func (y *YDBClient) GetAllMatches(ctx context.Context) ([]models.Match, error) {
	log.Println("YDB: Getting all matches")
	
	var matches []models.Match
	
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			var res result.Result
			_, res, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithOnlineReadOnly()),
				table.CommitTx(),
			), `
				SELECT match_id, name, home_team, away_team, start_time, sport, tournament, bookmaker, created_at, updated_at
				FROM matches
				ORDER BY start_time DESC;
			`, table.NewQueryParameters())
			if err != nil {
				return err
			}
			defer res.Close()
			
			for res.NextResultSet(ctx) {
				for res.NextRow() {
					match := models.Match{}
					err = res.ScanNamed(
						named.Required("match_id", &match.ID),
						named.Required("name", &match.Name),
						named.Required("home_team", &match.HomeTeam),
						named.Required("away_team", &match.AwayTeam),
						named.Required("start_time", &match.StartTime),
						named.Required("sport", &match.Sport),
						named.Required("tournament", &match.Tournament),
						named.Required("bookmaker", &match.Bookmaker),
						named.Required("created_at", &match.CreatedAt),
						named.Required("updated_at", &match.UpdatedAt),
					)
					if err != nil {
						return err
					}
					
					// Get events for this match
					events, err := y.getEventsForMatch(ctx, match.ID)
					if err != nil {
						return fmt.Errorf("failed to get events for match %s: %w", match.ID, err)
					}
					match.Events = events
					
					matches = append(matches, match)
				}
			}
			return res.Err()
		})
	
	if err != nil {
		return nil, fmt.Errorf("failed to get all matches: %w", err)
	}
	
	return matches, nil
}

// createTablesIfNotExist creates the hierarchical tables
func (y *YDBClient) createTablesIfNotExist(ctx context.Context) error {
	return y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			// Create matches table
			err := s.CreateTable(ctx, path.Join(y.db.Name(), "matches"),
				options.WithColumn("match_id", types.TypeUTF8),
				options.WithColumn("name", types.TypeUTF8),
				options.WithColumn("home_team", types.TypeUTF8),
				options.WithColumn("away_team", types.TypeUTF8),
				options.WithColumn("start_time", types.TypeTimestamp),
				options.WithColumn("sport", types.TypeUTF8),
				options.WithColumn("tournament", types.TypeUTF8),
				options.WithColumn("bookmaker", types.TypeUTF8),
				options.WithColumn("created_at", types.TypeTimestamp),
				options.WithColumn("updated_at", types.TypeTimestamp),
				options.WithPrimaryKeyColumn("match_id"),
			)
			if err != nil {
				log.Printf("YDB: Matches table creation result: %v", err)
			} else {
				log.Println("YDB: Table 'matches' created successfully")
			}
			
			// Create events table
			err = s.CreateTable(ctx, path.Join(y.db.Name(), "events"),
				options.WithColumn("event_id", types.TypeUTF8),
				options.WithColumn("match_id", types.TypeUTF8),
				options.WithColumn("event_type", types.TypeUTF8),
				options.WithColumn("market_name", types.TypeUTF8),
				options.WithColumn("bookmaker", types.TypeUTF8),
				options.WithColumn("created_at", types.TypeTimestamp),
				options.WithColumn("updated_at", types.TypeTimestamp),
				options.WithPrimaryKeyColumn("event_id"),
			)
			if err != nil {
				log.Printf("YDB: Events table creation result: %v", err)
			} else {
				log.Println("YDB: Table 'events' created successfully")
			}
			
			// Create outcomes table
			err = s.CreateTable(ctx, path.Join(y.db.Name(), "outcomes"),
				options.WithColumn("outcome_id", types.TypeUTF8),
				options.WithColumn("event_id", types.TypeUTF8),
				options.WithColumn("outcome_type", types.TypeUTF8),
				options.WithColumn("parameter", types.TypeUTF8),
				options.WithColumn("odds", types.TypeDouble),
				options.WithColumn("bookmaker", types.TypeUTF8),
				options.WithColumn("created_at", types.TypeTimestamp),
				options.WithColumn("updated_at", types.TypeTimestamp),
				options.WithPrimaryKeyColumn("outcome_id"),
			)
			if err != nil {
				log.Printf("YDB: Outcomes table creation result: %v", err)
			} else {
				log.Println("YDB: Table 'outcomes' created successfully")
			}
			
			return nil
		})
}

// GetMatchesWithLimit retrieves matches with a limit to avoid timeout
func (y *YDBClient) GetMatchesWithLimit(ctx context.Context, limit int) ([]models.Match, error) {
	log.Printf("YDB: Getting matches with limit %d", limit)
	
	var matches []models.Match
	
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			var res result.Result
			_, res, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithOnlineReadOnly()),
				table.CommitTx(),
			), fmt.Sprintf(`
				SELECT match_id, name, home_team, away_team, start_time, sport, tournament, bookmaker, created_at, updated_at
				FROM matches
				ORDER BY start_time DESC
				LIMIT %d;
			`, limit), table.NewQueryParameters())
			if err != nil {
				return err
			}
			defer res.Close()
			
			for res.NextResultSet(ctx) {
				for res.NextRow() {
					match := models.Match{}
					err = res.ScanNamed(
						named.Required("match_id", &match.ID),
						named.Required("name", &match.Name),
						named.Required("home_team", &match.HomeTeam),
						named.Required("away_team", &match.AwayTeam),
						named.Required("start_time", &match.StartTime),
						named.Required("sport", &match.Sport),
						named.Required("tournament", &match.Tournament),
						named.Required("bookmaker", &match.Bookmaker),
						named.Required("created_at", &match.CreatedAt),
						named.Required("updated_at", &match.UpdatedAt),
					)
					if err != nil {
						return err
					}
					
					// Get events for this match
					events, err := y.getEventsForMatch(ctx, match.ID)
					if err != nil {
						log.Printf("Warning: failed to get events for match %s: %v", match.ID, err)
						events = []models.Event{}
					}
					match.Events = events
					
					matches = append(matches, match)
				}
			}
			return res.Err()
		})
	
	return matches, err
}

// CleanTable removes all data from a table
func (y *YDBClient) CleanTable(ctx context.Context, tableName string) error {
	log.Printf("YDB: Cleaning table %s", tableName)
	
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			_, _, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithSerializableReadWrite()),
				table.CommitTx(),
			), fmt.Sprintf("DELETE FROM %s;", tableName), table.NewQueryParameters())
			return err
		})
	
	if err != nil {
		return fmt.Errorf("failed to clean table %s: %w", tableName, err)
	}
	
	log.Printf("YDB: Table %s cleaned successfully", tableName)
	return nil
}

// GetTTLSettings retrieves TTL settings for tables
func (y *YDBClient) GetTTLSettings(ctx context.Context) (map[string]interface{}, error) {
	// For now, return a simple status
	// In a real implementation, you would query YDB for TTL settings
	return map[string]interface{}{
		"status": "not_configured",
		"tables": []string{"matches", "events", "outcomes"},
	}, nil
}

// SetupTTL configures TTL for tables
func (y *YDBClient) SetupTTL(ctx context.Context, expireAfter time.Duration) error {
	log.Printf("YDB: Setting up TTL with expire after %v", expireAfter)
	
	// For now, just log the action
	// In a real implementation, you would configure TTL on YDB tables
	log.Println("YDB: TTL setup completed (simulated)")
	
	return nil
}

// DisableTTL disables TTL for tables
func (y *YDBClient) DisableTTL(ctx context.Context) error {
	log.Println("YDB: Disabling TTL")
	
	// For now, just log the action
	// In a real implementation, you would disable TTL on YDB tables
	log.Println("YDB: TTL disabled (simulated)")
	
	return nil
}

// Close closes the database connection
func (y *YDBClient) Close() error {
	return y.db.Close(context.Background())
}
