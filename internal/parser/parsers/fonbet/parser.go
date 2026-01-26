package fonbet

import (
	"context"
	"fmt"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/performance"
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
	// Create components
	eventFetcher := NewEventFetcher(config)
	oddsParser := NewOddsParser()
	matchBuilder := NewMatchBuilder("Fonbet")
	eventProcessor := NewBatchProcessor(nil, eventFetcher, oddsParser, matchBuilder)
	
	return &Parser{
		eventFetcher:   eventFetcher,
		oddsParser:     oddsParser,
		matchBuilder:   matchBuilder,
		eventProcessor: eventProcessor,
		storage:        nil, // No external storage - data served from memory
		config:         config,
	}
}

// runOnce performs a single parsing run for all configured sports
func (p *Parser) runOnce(ctx context.Context) error {
	for _, sportStr := range p.config.ValueCalculator.Sports {
		select {
		case <-ctx.Done():
			return ctx.Err()
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
		
		// Print performance summary after each run
		performance.GetTracker().PrintSummary()
	}
	return nil
}

func (p *Parser) Start(ctx context.Context) error {
	fmt.Println("Starting Fonbet parser (on-demand mode - parsing triggered by /matches requests)...")
	
	// Run once at startup to have initial data
	if err := p.runOnce(ctx); err != nil {
		return err
	}
	
	// Just wait for context cancellation (no background parsing)
	<-ctx.Done()
	
	// Print final summary before shutdown
	fmt.Println("\n📊 Final Performance Summary:")
	performance.GetTracker().PrintSummary()
	return nil
}

// ParseOnce triggers a single parsing run (on-demand parsing)
func (p *Parser) ParseOnce(ctx context.Context) error {
	return p.runOnce(ctx)
}

func (p *Parser) Stop() error {
	fmt.Println("Stopping Fonbet parser...")
	return nil
}

func (p *Parser) GetName() string {
	return "Fonbet"
}
