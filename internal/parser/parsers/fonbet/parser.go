package fonbet

import (
	"context"
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// Parser is the main parser that coordinates all components
type Parser struct {
	eventFetcher   interfaces.EventFetcher
	oddsParser     interfaces.OddsParser
	matchBuilder   interfaces.MatchBuilder
	eventProcessor interfaces.EventProcessor
	storage        interfaces.Storage
	config         *config.Config
}

func NewParser(config *config.Config) *Parser {
	// Create YDB client
	fmt.Println("Creating YDB client...")
	fmt.Printf("YDB config: endpoint=%s, database=%s, key_file=%s\n", 
		config.YDB.Endpoint, config.YDB.Database, config.YDB.ServiceAccountKeyFile)
	
	baseYDBClient, err := storage.NewYDBClient(&config.YDB)
	var ydbClient interfaces.Storage
	if err != nil {
		fmt.Printf("❌ Failed to create YDB client: %v\n", err)
		fmt.Println("⚠️  Parser will run without YDB storage")
		ydbClient = nil
	} else {
		fmt.Println("✅ YDB client created successfully")
		// Wrap with batch client for better performance
		ydbClient = storage.NewBatchYDBClient(baseYDBClient)
		fmt.Println("✅ YDB batch client created successfully")
	}
	
	// Create components
	eventFetcher := NewEventFetcher(config)
	oddsParser := NewOddsParser()
	matchBuilder := NewMatchBuilder("Fonbet")
	eventProcessor := NewBatchProcessor(ydbClient, eventFetcher, oddsParser, matchBuilder, config.Parser.Fonbet.TestLimit)
	
	return &Parser{
		eventFetcher:   eventFetcher,
		oddsParser:     oddsParser,
		matchBuilder:   matchBuilder,
		eventProcessor: eventProcessor,
		storage:        ydbClient,
		config:         config,
	}
}

func (p *Parser) Start(ctx context.Context) error {
	fmt.Println("Starting Fonbet parser...")

	interval := p.config.Parser.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	runOnce := func() {
		for _, sportStr := range p.config.ValueCalculator.Sports {
			select {
			case <-ctx.Done():
				return
			default:
			}

			sport, valid := enums.ParseSport(sportStr)
			if !valid {
				fmt.Printf("Unsupported sport: %s\n", sportStr)
				continue
			}

			if err := p.eventProcessor.ProcessSportEvents(sportStr); err != nil {
				fmt.Printf("Failed to parse %s events: %v\n", sport, err)
				continue
			}
		}
	}

	// Run immediately, then on interval.
	runOnce()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runOnce()
		}
	}
}

func (p *Parser) Stop() error {
	fmt.Println("Stopping Fonbet parser...")
	return nil
}

func (p *Parser) GetName() string {
	return "Fonbet"
}
