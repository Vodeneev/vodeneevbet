package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/table"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/options"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/result"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/result/named"
	"github.com/ydb-platform/ydb-go-sdk/v3/table/types"
	"github.com/ydb-platform/ydb-go-yc"
)

// mustJSON converts a Go value to JSON string
func mustJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal JSON: %v", err))
	}
	return string(data)
}

// YDBClient provides YDB database operations
type YDBClient struct {
	db     ydb.Connection
	config *config.YDBConfig
}

// NewYDBClient creates a new YDB client
func NewYDBClient(cfg *config.YDBConfig) (*YDBClient, error) {
	ctx := context.Background()
	
	// Создаем строку подключения
	dsn := fmt.Sprintf("%s?database=%s", cfg.Endpoint, cfg.Database)
	
	log.Printf("YDB: Connecting to %s", dsn)
	
	// Создаем объект подключения db
	db, err := ydb.Open(ctx, dsn,
		yc.WithServiceAccountKeyFileCredentials(cfg.ServiceAccountKeyFile),
		// environ.WithEnvironCredentials(ctx), // альтернативный способ аутентификации
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to YDB: %w", err)
	}
	
	log.Printf("YDB: Successfully connected to database: %s", cfg.Database)
	
	return &YDBClient{
		db:     db,
		config: cfg,
	}, nil
}

// StoreOdd stores betting odds in YDB
func (y *YDBClient) StoreOdd(ctx context.Context, odd *models.Odd) error {
	log.Printf("YDB: Storing odd for match %s from %s: %+v", 
		odd.MatchID, odd.Bookmaker, odd.Outcomes)
	
	// Создаем таблицу если не существует
	if err := y.createTablesIfNotExist(ctx); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}
	
	// Сохраняем коэффициент в YDB
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			_, _, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithSerializableReadWrite()),
				table.CommitTx(),
			), `
				DECLARE $match_id AS Utf8;
				DECLARE $bookmaker AS Utf8;
				DECLARE $market AS Utf8;
				DECLARE $outcomes AS Json;
				DECLARE $updated_at AS Timestamp;
				DECLARE $match_name AS Utf8;
				DECLARE $match_time AS Timestamp;
				DECLARE $sport AS Utf8;
				
				UPSERT INTO odds (match_id, bookmaker, market, outcomes, updated_at, match_name, match_time, sport)
				VALUES ($match_id, $bookmaker, $market, $outcomes, $updated_at, $match_name, $match_time, $sport);
			`, table.NewQueryParameters(
				table.ValueParam("$match_id", types.UTF8Value(odd.MatchID)),
				table.ValueParam("$bookmaker", types.UTF8Value(odd.Bookmaker)),
				table.ValueParam("$market", types.UTF8Value(odd.Market)),
				table.ValueParam("$outcomes", types.JSONValue(mustJSON(odd.Outcomes))),
				table.ValueParam("$updated_at", types.TimestampValueFromTime(odd.UpdatedAt)),
				table.ValueParam("$match_name", types.UTF8Value(odd.MatchName)),
				table.ValueParam("$match_time", types.TimestampValueFromTime(odd.MatchTime)),
				table.ValueParam("$sport", types.UTF8Value(odd.Sport)),
			))
			return err
		})
	
	if err != nil {
		return fmt.Errorf("failed to store odd: %w", err)
	}
	
	log.Printf("YDB: Successfully stored odd for match %s", odd.MatchID)
	return nil
}

// GetOddsByMatch retrieves odds for a specific match
func (y *YDBClient) GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error) {
	log.Printf("YDB: Getting odds for match %s", matchID)
	
	var odds []*models.Odd
	
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			var res result.Result
			_, res, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithOnlineReadOnly()),
				table.CommitTx(),
			), `
				DECLARE $match_id AS Utf8;
				SELECT match_id, bookmaker, market, outcomes, updated_at, match_name, match_time, sport
				FROM odds
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
					odd := &models.Odd{}
					err = res.ScanNamed(
						named.Required("match_id", &odd.MatchID),
						named.Required("bookmaker", &odd.Bookmaker),
						named.Required("market", &odd.Market),
						named.Required("outcomes", &odd.Outcomes),
						named.Required("updated_at", &odd.UpdatedAt),
						named.Required("match_name", &odd.MatchName),
						named.Required("match_time", &odd.MatchTime),
						named.Required("sport", &odd.Sport),
					)
					if err != nil {
						return err
					}
					odds = append(odds, odd)
				}
			}
			return res.Err()
		})
	
	if err != nil {
		return nil, fmt.Errorf("failed to get odds for match %s: %w", matchID, err)
	}
	
	return odds, nil
}

// GetAllMatches retrieves all available matches
func (y *YDBClient) GetAllMatches(ctx context.Context) ([]string, error) {
	log.Println("YDB: Getting all matches")
	
	var matches []string
	
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			var res result.Result
			_, res, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithOnlineReadOnly()),
				table.CommitTx(),
			), `
				SELECT DISTINCT match_id FROM odds;
			`, table.NewQueryParameters())
			if err != nil {
				return err
			}
			defer res.Close()
			
			for res.NextResultSet(ctx) {
				for res.NextRow() {
					var matchID string
					err = res.ScanNamed(
						named.Required("match_id", &matchID),
					)
					if err != nil {
						return err
					}
					matches = append(matches, matchID)
				}
			}
			return res.Err()
		})
	
	if err != nil {
		return nil, fmt.Errorf("failed to get all matches: %w", err)
	}
	
	return matches, nil
}

// GetAllOdds retrieves all odds from YDB
func (y *YDBClient) GetAllOdds(ctx context.Context) ([]*models.Odd, error) {
	log.Println("YDB: Getting all odds")
	
	var odds []*models.Odd
	
	err := y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			var res result.Result
			_, res, err := s.Execute(ctx, table.TxControl(
				table.BeginTx(table.WithOnlineReadOnly()),
				table.CommitTx(),
			), `
				SELECT match_id, bookmaker, market, outcomes, updated_at, match_name, match_time, sport
				FROM odds
				ORDER BY updated_at DESC;
			`, table.NewQueryParameters())
			if err != nil {
				return err
			}
			defer res.Close()
			
			for res.NextResultSet(ctx) {
				for res.NextRow() {
					odd := &models.Odd{}
					err = res.ScanNamed(
						named.Required("match_id", &odd.MatchID),
						named.Required("bookmaker", &odd.Bookmaker),
						named.Required("market", &odd.Market),
						named.Required("outcomes", &odd.Outcomes),
						named.Required("updated_at", &odd.UpdatedAt),
						named.Required("match_name", &odd.MatchName),
						named.Required("match_time", &odd.MatchTime),
						named.Required("sport", &odd.Sport),
					)
					if err != nil {
						return err
					}
					odds = append(odds, odd)
				}
			}
			return res.Err()
		})
	
	if err != nil {
		return nil, fmt.Errorf("failed to get all odds: %w", err)
	}
	
	return odds, nil
}

// StoreArbitrage stores arbitrage opportunity (stub for compatibility)
func (y *YDBClient) StoreArbitrage(ctx context.Context, arb *models.Arbitrage) error {
	log.Printf("YDB: Would store arbitrage %s with profit %.2f%%", 
		arb.ID, arb.ProfitPercent)
	return nil
}

// createTablesIfNotExist creates necessary tables in YDB
func (y *YDBClient) createTablesIfNotExist(ctx context.Context) error {
	return y.db.Table().Do(ctx,
		func(ctx context.Context, s table.Session) error {
			// Создаем таблицу для коэффициентов
			err := s.CreateTable(ctx, path.Join(y.db.Name(), "odds"),
				options.WithColumn("match_id", types.TypeUTF8),
				options.WithColumn("bookmaker", types.TypeUTF8),
				options.WithColumn("market", types.TypeUTF8),
				options.WithColumn("outcomes", types.TypeJSON),
				options.WithColumn("updated_at", types.TypeTimestamp),
				options.WithColumn("match_name", types.TypeUTF8),
				options.WithColumn("match_time", types.TypeTimestamp),
				options.WithColumn("sport", types.TypeUTF8),
				options.WithPrimaryKeyColumn("match_id", "bookmaker", "market"),
			)
			if err != nil {
				// Игнорируем ошибку если таблица уже существует
				log.Printf("YDB: Table creation result: %v", err)
			} else {
				log.Println("YDB: Table 'odds' created successfully")
			}
			return nil
		})
}

// Close closes the YDB connection
func (y *YDBClient) Close() error {
	log.Println("YDB: Closing connection")
	ctx := context.Background()
	return y.db.Close(ctx)
}
