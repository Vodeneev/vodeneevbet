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
	
	// Parse corner events specifically
	if err := p.parseCornerEvents(jsonData); err != nil {
		fmt.Printf("Failed to parse corner events: %v\n", err)
	}
	
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
		MatchName: p.getMatchName(event),
		MatchTime: event.StartTime,
		Sport:     "football",
	}
	
	if err := p.ydbClient.StoreOdd(context.Background(), odd); err != nil {
		fmt.Printf("Failed to store odd: %v\n", err)
		return err
	}
	
	fmt.Printf("Stored odd for event %s\n", event.ID)
	return nil
}

func (p *Parser) parseCornerEvents(jsonData []byte) error {
	cornerEvents, err := p.jsonParser.ParseCornerEvents(jsonData)
	if err != nil {
		return fmt.Errorf("failed to parse corner events: %w", err)
	}
	
	fmt.Printf("Found %d corner events\n", len(cornerEvents))
	
	// Process each corner event individually
	for i, cornerEvent := range cornerEvents {
		if p.config.Parser.Fonbet.TestLimit > 0 && i >= p.config.Parser.Fonbet.TestLimit {
			fmt.Printf("Limiting to first %d corner events for testing\n", p.config.Parser.Fonbet.TestLimit)
			break
		}
		
		fmt.Printf("Processing corner event %d (%s)...\n", cornerEvent.ID, cornerEvent.Name)
		
		// Get factors for this specific event
		factorData, err := p.httpClient.GetEventFactors(cornerEvent.ID)
		if err != nil {
			fmt.Printf("Failed to get factors for event %d: %v\n", cornerEvent.ID, err)
			continue
		}
		
		fmt.Printf("Received %d bytes for event %d\n", len(factorData), cornerEvent.ID)
		if len(factorData) < 500 {
			fmt.Printf("Response data: %s\n", string(factorData))
		}
		
		// Parse factors from the event-specific response
		factorGroups, err := p.jsonParser.ParseFactors(factorData)
		if err != nil {
			fmt.Printf("Failed to parse factors for event %d: %v\n", cornerEvent.ID, err)
			continue
		}
		
		fmt.Printf("Received %d factor groups for event %d\n", len(factorGroups), cornerEvent.ID)
		for _, group := range factorGroups {
			fmt.Printf("  Factor group: event %d, factors: %d\n", group.EventID, len(group.Factors))
		}
		
		// Find factors for this specific event or its parent
		var factors []FonbetFactor
		for _, group := range factorGroups {
			if group.EventID == cornerEvent.ID || group.EventID == cornerEvent.ParentID {
				factors = group.Factors
				fmt.Printf("Found factors for event %d (parent: %d)\n", group.EventID, cornerEvent.ParentID)
				break
			}
		}
		
		if len(factors) == 0 {
			fmt.Printf("No factors found for corner event %d\n", cornerEvent.ID)
			continue
		}
		
		fmt.Printf("Found %d factors for corner event %d\n", len(factors), cornerEvent.ID)
		
		// Parse corner odds
		cornerOdds := p.parseCornerOdds(factors)
		if len(cornerOdds) > 0 {
			fmt.Printf("Extracted corner odds: %+v\n", cornerOdds)
			
			// Try to find parent event for team names
			parentEventName := cornerEvent.Name
			if cornerEvent.ParentID > 0 {
				// TODO: Look up parent event to get team names
				parentEventName = fmt.Sprintf("Corners for event %d", cornerEvent.ParentID)
			}
			
			odd := &models.Odd{
				MatchID:   fmt.Sprintf("%d", cornerEvent.ID),
				Bookmaker: "Fonbet",
				Market:    "Corners",
				Outcomes:  cornerOdds,
				UpdatedAt: time.Now(),
				MatchName: parentEventName,
				MatchTime: time.Unix(cornerEvent.StartTime, 0),
				Sport:     "football",
			}
			
			if err := p.ydbClient.StoreOdd(context.Background(), odd); err != nil {
				fmt.Printf("Failed to store corner odd: %v\n", err)
				continue
			}
			
			fmt.Printf("Stored corner odd for event %d with %d outcomes\n", 
				cornerEvent.ID, len(cornerOdds))
		}
	}
	
	return nil
}

func (p *Parser) parseCornerOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		// Parse different types of corner bets
		switch factor.F {
		case 910, 912: // Total corners over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact corner counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// getMatchName returns a proper match name with team names or fallback
func (p *Parser) getMatchName(event FonbetEvent) string {
	// If we have both team names, use them
	if event.HomeTeam != "" && event.AwayTeam != "" {
		return fmt.Sprintf("%s vs %s", event.HomeTeam, event.AwayTeam)
	}
	
	// If we have the event name, use it
	if event.Name != "" {
		return event.Name
	}
	
	// Fallback to event ID
	return fmt.Sprintf("Event %s", event.ID)
}


