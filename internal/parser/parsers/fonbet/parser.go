package fonbet

import (
	"context"
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums/fonbet"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers"
)

type Parser struct {
	httpClient *HTTPClient
	jsonParser  *JSONParser
	ydbClient   parsers.YDBClient
	config      *config.Config
}

func NewParser(ydbClient parsers.YDBClient, config *config.Config) *Parser {
	return &Parser{
		httpClient: NewHTTPClient(config),
		jsonParser: NewJSONParser(),
		ydbClient:  ydbClient,
		config:     config,
	}
}

func (p *Parser) Start(ctx context.Context) error {
	fmt.Println("Starting Fonbet parser...")
	
	for _, sportStr := range p.config.ValueCalculator.Sports {
		sport, valid := enums.ParseSport(sportStr)
		if !valid {
			fmt.Printf("Unsupported sport: %s\n", sportStr)
			continue
		}
		
		if err := p.parseSportEvents(sport); err != nil {
			fmt.Printf("Failed to parse %s events: %v\n", sport, err)
			continue
		}
	}
	
	return nil
}

func (p *Parser) Stop() error {
	fmt.Println("Stopping Fonbet parser...")
	return nil
}

func (p *Parser) parseSportEvents(sport enums.Sport) error {
	sportInfo := sport.GetSportInfo()
	scopeMarket := fonbet.GetScopeMarket(sport)
	fmt.Printf("Parsing %s events from Fonbet (scope: %s)...\n", sportInfo.Name, scopeMarket.String())
	
	events, err := p.getSportEvents(sport)
	if err != nil {
		return fmt.Errorf("failed to get %s events: %w", sport, err)
	}
	
	for i, event := range events {
		if p.config.Parser.Fonbet.TestLimit > 0 && i >= p.config.Parser.Fonbet.TestLimit {
			fmt.Printf("Limiting to first %d events for testing\n", p.config.Parser.Fonbet.TestLimit)
			break
		}
		
		if err := p.parseEvent(event); err != nil {
			fmt.Printf("Failed to parse event %s: %v\n", event.ID, err)
			continue
		}
	}
	
	return nil
}

func (p *Parser) getSportEvents(sport enums.Sport) ([]FonbetEvent, error) {
	fmt.Printf("Making API request for %s...\n", sport)
	
	jsonData, err := p.httpClient.GetEvents(sport)
	if err != nil {
		return nil, fmt.Errorf("failed to get events from API: %w", err)
	}
	
	fmt.Printf("Received %d bytes of data\n", len(jsonData))
	
	events, err := p.jsonParser.ParseEvents(jsonData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}
	
	fmt.Printf("Parsed %d events\n", len(events))
	return events, nil
}

func (p *Parser) parseEvent(event FonbetEvent) error {
	fmt.Printf("Parsing event %s\n", event.ID)
	
	// TODO: Extract actual odds from event data
	odd := &models.Odd{
		MatchID:   event.ID,
		Bookmaker: "Fonbet",
		Market:    "Match Result",
		Outcomes: map[string]float64{
			"home": 1.5,  // Mock data
			"draw": 3.2,
			"away": 2.1,
		},
		UpdatedAt: time.Now(),
		MatchName: fmt.Sprintf("Event %s", event.ID),
		MatchTime: time.Now().Add(2 * time.Hour),
		Sport:     "football",
	}
	
	if err := p.ydbClient.StoreOdd(context.Background(), odd); err != nil {
		fmt.Printf("Failed to store odd: %v\n", err)
		return err
	}
	
	fmt.Printf("Stored odd for event %s\n", event.ID)
	return nil
}


