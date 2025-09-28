package fonbet

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums/fonbet"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)


type Parser struct {
	httpClient *HTTPClient
	jsonParser *JSONParser
	ydbClient  *storage.YDBClient
	config     *config.Config
}

func NewParser(config *config.Config) *Parser {
	// Create YDB client
	fmt.Println("Creating YDB client...")
	fmt.Printf("YDB config: endpoint=%s, database=%s, key_file=%s\n", 
		config.YDB.Endpoint, config.YDB.Database, config.YDB.ServiceAccountKeyFile)
	
	ydbClient, err := storage.NewYDBClient(&config.YDB)
	if err != nil {
		fmt.Printf("❌ Failed to create YDB client: %v\n", err)
		fmt.Println("⚠️  Parser will run without YDB storage")
		ydbClient = nil
	} else {
		fmt.Println("✅ YDB client created successfully")
	}
	
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
	
	// Group events by parent match
	eventsByMatch := p.groupEventsByMatch(events)
	
	// Process each match with all its events
	matchCount := 0
	for matchID, matchEvents := range eventsByMatch {
		if p.config.Parser.Fonbet.TestLimit > 0 && matchCount >= p.config.Parser.Fonbet.TestLimit {
			fmt.Printf("Limiting to first %d matches for testing\n", p.config.Parser.Fonbet.TestLimit)
			break
		}
		
		if len(matchEvents) == 0 {
			continue
		}
		
		// The first event is always the main match (Level 1)
		mainEvent := &matchEvents[0]
		statisticalEvents := matchEvents[1:] // All other events are statistical
		
		// Process the match with all its events
		if err := p.parseMatchWithEvents(*mainEvent, statisticalEvents); err != nil {
			fmt.Printf("Failed to parse match %s: %v\n", matchID, err)
			continue
		}
		
		matchCount++
	}
	
	return nil
}

// groupEventsByMatch groups events by their parent match ID
func (p *Parser) groupEventsByMatch(events []FonbetEvent) map[string][]FonbetEvent {
	groups := make(map[string][]FonbetEvent)
	
	// First, find all main matches (Level 1)
	mainMatches := make(map[string]FonbetEvent)
	for _, event := range events {
		if event.Level == 1 {
			mainMatches[event.ID] = event
		}
	}
	
	// Then, for each main match, find all related events
	for matchID, mainMatch := range mainMatches {
		// Add the main match itself
		groups[matchID] = append(groups[matchID], mainMatch)
		
		// Find all statistical events for this match
		for _, event := range events {
			if event.Level > 1 && event.ParentID > 0 {
				parentID := fmt.Sprintf("%d", event.ParentID)
				if parentID == matchID {
					groups[matchID] = append(groups[matchID], event)
				}
			}
		}
	}
	
	return groups
}

// parseMatchWithEvents processes a main match with all its statistical events
func (p *Parser) parseMatchWithEvents(mainEvent FonbetEvent, statisticalEvents []FonbetEvent) error {
	fmt.Printf("Processing match %s (%s vs %s) with %d statistical events\n", 
		mainEvent.ID, mainEvent.HomeTeam, mainEvent.AwayTeam, len(statisticalEvents))
	
	// Get factors for main event
	mainEventID, err := strconv.ParseInt(mainEvent.ID, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse main event ID: %w", err)
	}
	
	// Get factors for main event
	mainFactors, err := p.getEventFactors(mainEventID)
	if err != nil {
		fmt.Printf("Failed to get factors for main event %s: %v\n", mainEvent.ID, err)
		mainFactors = []FonbetFactor{}
	}
	
	// Create hierarchical match structure
	match := p.createHierarchicalMatchWithEvents(mainEvent, statisticalEvents, mainFactors)
	
	// Store the match
	if err := p.storeHierarchicalMatch(context.Background(), match); err != nil {
		return fmt.Errorf("failed to store match: %w", err)
	}
	
	fmt.Printf("Successfully stored match %s with %d events\n", match.ID, len(match.Events))
	return nil
}

// GetEventFactors gets factors for a specific event (public method for debugging)
func (p *Parser) GetEventFactors(eventID int64) ([]FonbetFactor, error) {
	return p.getEventFactors(eventID)
}

// getEventFactors gets factors for a specific event
func (p *Parser) getEventFactors(eventID int64) ([]FonbetFactor, error) {
	factorData, err := p.httpClient.GetEventFactors(eventID)
	if err != nil {
		return nil, fmt.Errorf("failed to get factors: %w", err)
	}
	
	factorGroups, err := p.jsonParser.ParseFactors(factorData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse factors: %w", err)
	}
	
	// Find factors for this specific event
	for _, group := range factorGroups {
		if group.EventID == eventID {
			return group.Factors, nil
		}
	}
	
	return []FonbetFactor{}, nil
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
	
	// Convert string ID to int64 for API call
	eventID, err := strconv.ParseInt(event.ID, 10, 64)
	if err != nil {
		fmt.Printf("Failed to parse event ID %s: %v\n", event.ID, err)
		return err
	}
	
	// Get factors for this specific event
	factorData, err := p.httpClient.GetEventFactors(eventID)
	if err != nil {
		fmt.Printf("Failed to get factors for event %s: %v\n", event.ID, err)
		return err
	}
	
	// Parse factors from the event-specific response
	factorGroups, err := p.jsonParser.ParseFactors(factorData)
	if err != nil {
		fmt.Printf("Failed to parse factors for event %s: %v\n", event.ID, err)
		return err
	}
	
	// Find factors for this specific event
	var factors []FonbetFactor
	for _, group := range factorGroups {
		if group.EventID == eventID {
			factors = group.Factors
			break
		}
	}
	
	if len(factors) == 0 {
		fmt.Printf("No factors found for event %s\n", event.ID)
		return nil
	}
	
	// Parse odds based on event type
	odds := p.parseEventOdds(event, factors)
	if len(odds) > 0 {
		fmt.Printf("Extracted odds: %+v\n", odds)
		
		// Create hierarchical match structure
		match := p.createHierarchicalMatch(event, odds)
		
		// Store using new hierarchical structure
		if err := p.storeHierarchicalMatch(context.Background(), match); err != nil {
			fmt.Printf("Failed to store hierarchical match: %v\n", err)
			return err
		}
		
		fmt.Printf("Stored odd for event %s with %d outcomes\n", event.ID, len(odds))
	}
	
	return nil
}

// parseEventOdds parses odds for any type of event
func (p *Parser) parseEventOdds(event FonbetEvent, factors []FonbetFactor) map[string]float64 {
	// Determine event type based on Kind
	eventType := p.getEventTypeFromKind(event.Kind)
	
	switch eventType {
	case "corners":
		return p.parseCornerOdds(factors)
	case "yellow_cards":
		return p.parseYellowCardOdds(factors)
	case "fouls":
		return p.parseFoulOdds(factors)
	case "shots_on_target":
		return p.parseShotsOnTargetOdds(factors)
	case "offsides":
		return p.parseOffsideOdds(factors)
	case "throw_ins":
		return p.parseThrowInOdds(factors)
	default:
		// For main matches, parse basic match odds
		return p.parseMainMatchOdds(factors)
	}
}

// getEventTypeFromKind determines event type from Kind field using standardized mapping
func (p *Parser) getEventTypeFromKind(kind int64) string {
	// Create a temporary event to use the standardized mapping
	tempEvent := FonbetAPIEvent{Kind: kind}
	eventType := p.jsonParser.getEventType(tempEvent)
	return string(eventType)
}

// parseMainMatchOdds parses basic match odds (1X2, totals, etc.)
func (p *Parser) parseMainMatchOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 1, 2, 3: // 1X2 odds
			odds[fmt.Sprintf("outcome_%d", factor.F)] = factor.V
		case 910, 912: // Total goals over/under (main totals)
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 930, 931: // Total 3.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 989, 991: // Total 2
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1569, 1572: // Total 1.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1672, 1675, 1677, 1678: // Total 1
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1680, 1681: // Total 1.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1696, 1697: // Total 0.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1727, 1728: // Total 1
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1730, 1731: // Total 1.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1733, 1734: // Total 2
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1736, 1737: // Total 2.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1739, 1791: // Total 3
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1793, 1794: // Total 4
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1796, 1797: // Total 4.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1799, 1800: // Total 5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1802, 1803: // Total 5.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1809, 1810: // Total 0.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1812, 1813: // Total 1
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1815, 1816: // Total 1.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1818, 1819: // Total 2
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1821, 1822: // Total 2.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1824, 1825: // Total 3
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1827, 1828: // Total 3.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1854, 1871: // Total 0.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1873, 1874: // Total 1
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1880, 1881: // Total 1.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1883, 1884: // Total 2
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 1886, 1887: // Total 2.5
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 4241, 4242: // Additional totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		default:
			// Handle all other factors with parameters
			if factor.Pt != "" {
				// String parameters (like "0.5", "1", "1.5", etc.)
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			} else if factor.P != 0 {
				// Numeric parameters - convert to string
				paramStr := fmt.Sprintf("%.1f", float64(factor.P)/100.0)
				odds[fmt.Sprintf("total_%s", paramStr)] = factor.V
			}
		}
	}
	
	return odds
}

// getEventMarketName returns the market name for an event using standardized mapping
func (p *Parser) getEventMarketName(event FonbetEvent) string {
	eventType := p.getEventTypeFromKind(event.Kind)
	return models.GetMarketName(models.StandardEventType(eventType))
}


func (p *Parser) parseCornerEvents(jsonData []byte) error {
	// Parse all events first
	events, err := p.jsonParser.ParseEvents(jsonData)
	if err != nil {
		return fmt.Errorf("failed to parse events: %w", err)
	}
	
	// Filter corner events (Kind = 400100 for corners)
	var cornerEvents []FonbetEvent
	for _, event := range events {
		if event.Kind == 400100 {
			cornerEvents = append(cornerEvents, event)
		}
	}
	
	fmt.Printf("Found %d corner events\n", len(cornerEvents))
	
	// Process each corner event individually
	for i, cornerEvent := range cornerEvents {
		if p.config.Parser.Fonbet.TestLimit > 0 && i >= p.config.Parser.Fonbet.TestLimit {
			fmt.Printf("Limiting to first %d corner events for testing\n", p.config.Parser.Fonbet.TestLimit)
			break
		}
		
		fmt.Printf("Processing corner event %s (%s)...\n", cornerEvent.ID, cornerEvent.Name)
		
		// Convert string ID to int64 for API call
		eventID, err := strconv.ParseInt(cornerEvent.ID, 10, 64)
		if err != nil {
			fmt.Printf("Failed to parse event ID %s: %v\n", cornerEvent.ID, err)
			continue
		}
		
		// Get factors for this specific event
		factorData, err := p.httpClient.GetEventFactors(eventID)
		if err != nil {
			fmt.Printf("Failed to get factors for event %s: %v\n", cornerEvent.ID, err)
			continue
		}
		
		fmt.Printf("Received %d bytes for event %s\n", len(factorData), cornerEvent.ID)
		if len(factorData) < 500 {
			fmt.Printf("Response data: %s\n", string(factorData))
		}
		
		// Parse factors from the event-specific response
		factorGroups, err := p.jsonParser.ParseFactors(factorData)
		if err != nil {
			fmt.Printf("Failed to parse factors for event %s: %v\n", cornerEvent.ID, err)
			continue
		}
		
		fmt.Printf("Received %d factor groups for event %s\n", len(factorGroups), cornerEvent.ID)
		for _, group := range factorGroups {
			fmt.Printf("  Factor group: event %d, factors: %d\n", group.EventID, len(group.Factors))
		}
		
		// Find factors for this specific event or its parent
		var factors []FonbetFactor
		for _, group := range factorGroups {
			if group.EventID == eventID || group.EventID == cornerEvent.ParentID {
				factors = group.Factors
				fmt.Printf("Found factors for event %d (parent: %d)\n", group.EventID, cornerEvent.ParentID)
				break
			}
		}
		
		if len(factors) == 0 {
			fmt.Printf("No factors found for corner event %s\n", cornerEvent.ID)
			continue
		}
		
		fmt.Printf("Found %d factors for corner event %s\n", len(factors), cornerEvent.ID)
		
		// Parse corner odds
		cornerOdds := p.parseCornerOdds(factors)
		if len(cornerOdds) > 0 {
			fmt.Printf("Extracted corner odds: %+v\n", cornerOdds)
			
			// Create hierarchical match structure for corner event
			match := p.createHierarchicalMatchFromCornerEvent(cornerEvent, cornerOdds)
			
			// Store using hierarchical structure
			if err := p.storeHierarchicalMatch(context.Background(), match); err != nil {
				fmt.Printf("Failed to store hierarchical corner match: %v\n", err)
				continue
			}
			
			fmt.Printf("Stored hierarchical corner match for event %s with %d outcomes\n", 
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

// parseStatisticalEventOdds parses odds for different types of statistical events
func (p *Parser) parseStatisticalEventOdds(eventType string, factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	switch eventType {
	case "corners":
		odds = p.parseCornerOdds(factors)
	case "yellow_cards":
		odds = p.parseYellowCardOdds(factors)
	case "fouls":
		odds = p.parseFoulOdds(factors)
	case "shots_on_target":
		odds = p.parseShotsOnTargetOdds(factors)
	case "offsides":
		odds = p.parseOffsideOdds(factors)
	case "throw_ins":
		odds = p.parseThrowInOdds(factors)
	}
	
	return odds
}

// parseYellowCardOdds parses yellow card betting odds
func (p *Parser) parseYellowCardOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total yellow cards over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact yellow card counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// parseFoulOdds parses foul betting odds
func (p *Parser) parseFoulOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total fouls over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact foul counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// parseShotsOnTargetOdds parses shots on target betting odds
func (p *Parser) parseShotsOnTargetOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total shots on target over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact shots on target counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// parseOffsideOdds parses offside betting odds
func (p *Parser) parseOffsideOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total offsides over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact offside counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// parseThrowInOdds parses throw-in betting odds
func (p *Parser) parseThrowInOdds(factors []FonbetFactor) map[string]float64 {
	odds := make(map[string]float64)
	
	for _, factor := range factors {
		switch factor.F {
		case 910, 912: // Total throw-ins over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
			}
		case 921, 922, 923, 924, 925: // Exact throw-in counts
			odds[fmt.Sprintf("exact_%d", factor.F)] = factor.V
		case 927, 928: // Alternative totals
			if factor.Pt != "" {
				odds[fmt.Sprintf("alt_total_%s", factor.Pt)] = factor.V
			}
		}
	}
	
	return odds
}

// getMarketName returns the market name for a statistical event type
func (p *Parser) getMarketName(eventType string) string {
	switch eventType {
	case "corners":
		return "Corners"
	case "yellow_cards":
		return "Yellow Cards"
	case "fouls":
		return "Fouls"
	case "shots_on_target":
		return "Shots on Target"
	case "offsides":
		return "Offsides"
	case "throw_ins":
		return "Throw-ins"
	default:
		return "Statistical Event"
	}
}

// getStatisticalEventName returns a proper name for a statistical event
func (p *Parser) getStatisticalEventName(eventType string, event FonbetAPIEvent) string {
	eventTypeName := p.getMarketName(eventType)
	
	if event.ParentID > 0 {
		return fmt.Sprintf("%s for event %d", eventTypeName, event.ParentID)
	}
	
	if event.Name != "" {
		return fmt.Sprintf("%s: %s", eventTypeName, event.Name)
	}
	
	return fmt.Sprintf("%s event %d", eventTypeName, event.ID)
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

// createHierarchicalMatch creates a hierarchical match structure from event data
func (p *Parser) createHierarchicalMatch(event FonbetEvent, odds map[string]float64) *models.Match {
	now := time.Now()
	
	// For basic events, determine match name based on type
	var matchName string
	if event.HomeTeam != "" && event.AwayTeam != "" {
		matchName = fmt.Sprintf("%s vs %s", event.HomeTeam, event.AwayTeam)
	} else {
		matchName = event.Name
	}
	
	// Create match
	match := &models.Match{
		ID:         event.ID,
		Name:       matchName,
		HomeTeam:   event.HomeTeam,
		AwayTeam:   event.AwayTeam,
		StartTime:  event.StartTime,
		Sport:      "football",
		Tournament: event.Tournament,
		Bookmaker:  "Fonbet",
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Create event
	eventType := p.jsonParser.GetStandardEventType(event.Kind)
	marketName := models.GetMarketName(eventType)
	
	eventModel := models.Event{
		ID:         fmt.Sprintf("%s_%s", event.ID, eventType),
		EventType:  string(eventType),
		MarketName: marketName,
		Bookmaker:  "Fonbet",
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Create outcomes
	for outcomeType, oddsValue := range odds {
		outcome := models.Outcome{
			ID:          fmt.Sprintf("%s_%s_%s", eventModel.ID, outcomeType, p.getParameterFromOutcome(outcomeType)),
			OutcomeType: string(p.getStandardOutcomeType(outcomeType)),
			Parameter:   p.getParameterFromOutcome(outcomeType),
			Odds:        oddsValue,
			Bookmaker:   "Fonbet",
			CreatedAt:   now,
			UpdatedAt:  now,
		}
		eventModel.Outcomes = append(eventModel.Outcomes, outcome)
	}
	
	match.Events = append(match.Events, eventModel)
	return match
}

// createHierarchicalMatchWithEvents creates a hierarchical match with main and statistical events
func (p *Parser) createHierarchicalMatchWithEvents(mainEvent FonbetEvent, statisticalEvents []FonbetEvent, mainFactors []FonbetFactor) *models.Match {
	now := time.Now()
	
	// Create match name
	matchName := fmt.Sprintf("%s vs %s", mainEvent.HomeTeam, mainEvent.AwayTeam)
	
	// Create match
	match := &models.Match{
		ID:         mainEvent.ID,
		Name:       matchName,
		HomeTeam:   mainEvent.HomeTeam,
		AwayTeam:   mainEvent.AwayTeam,
		StartTime:  mainEvent.StartTime,
		Sport:      "football",
		Tournament: mainEvent.Tournament,
		Bookmaker:  "Fonbet",
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Add main match event
	if len(mainFactors) > 0 {
		mainOdds := p.parseEventOdds(mainEvent, mainFactors)
		if len(mainOdds) > 0 {
			mainEventModel := p.createEventModel(mainEvent, mainOdds, "main_match", "Match Result")
			match.Events = append(match.Events, mainEventModel)
		}
	}
	
	// Add statistical events
	for _, statEvent := range statisticalEvents {
		statEventID, err := strconv.ParseInt(statEvent.ID, 10, 64)
		if err != nil {
			continue
		}
		
		statFactors, err := p.getEventFactors(statEventID)
		if err != nil {
			continue
		}
		
		if len(statFactors) > 0 {
			statOdds := p.parseEventOdds(statEvent, statFactors)
			if len(statOdds) > 0 {
				eventType := p.jsonParser.GetStandardEventType(statEvent.Kind)
				marketName := models.GetMarketName(eventType)
				statEventModel := p.createEventModel(statEvent, statOdds, string(eventType), marketName)
				match.Events = append(match.Events, statEventModel)
			}
		}
	}
	
	return match
}

// createEventModel creates a models.Event from FonbetEvent and odds
func (p *Parser) createEventModel(fonbetEvent FonbetEvent, odds map[string]float64, eventType, marketName string) models.Event {
	now := time.Now()
	
	event := models.Event{
		ID:         fmt.Sprintf("%s_%s", fonbetEvent.ID, eventType),
		EventType:  eventType,
		MarketName: marketName,
		Bookmaker:  "Fonbet",
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Create outcomes
	for outcomeType, oddsValue := range odds {
		outcome := models.Outcome{
			ID:          fmt.Sprintf("%s_%s_%s", event.ID, outcomeType, p.getParameterFromOutcome(outcomeType)),
			OutcomeType: string(p.getStandardOutcomeType(outcomeType)),
			Parameter:   p.getParameterFromOutcome(outcomeType),
			Odds:        oddsValue,
			Bookmaker:   "Fonbet",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		event.Outcomes = append(event.Outcomes, outcome)
	}
	
	return event
}

// extractTeamNames extracts home and away team names from match name
func (p *Parser) extractTeamNames(matchName string) (string, string) {
	// Try different separators
	separators := []string{" vs ", " - ", " v "}
	for _, sep := range separators {
		if parts := strings.Split(matchName, sep); len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
	}
	
	// Fallback
	return matchName, "Unknown Away"
}

// getStandardOutcomeType maps outcome string to standard type
func (p *Parser) getStandardOutcomeType(outcome string) models.StandardOutcomeType {
	switch {
	case strings.Contains(outcome, "home"):
		return models.OutcomeTypeHomeWin
	case strings.Contains(outcome, "away"):
		return models.OutcomeTypeAwayWin
	case strings.Contains(outcome, "draw"):
		return models.OutcomeTypeDraw
	case strings.Contains(outcome, "total_+"):
		return models.OutcomeTypeTotalOver
	case strings.Contains(outcome, "total_-"):
		return models.OutcomeTypeTotalUnder
	case strings.Contains(outcome, "exact_"):
		return models.OutcomeTypeExactCount
	case strings.Contains(outcome, "outcome_1"):
		return models.OutcomeTypeHomeWin
	case strings.Contains(outcome, "outcome_2"):
		return models.OutcomeTypeAwayWin
	case strings.Contains(outcome, "outcome_3"):
		return models.OutcomeTypeDraw
	default:
		return models.StandardOutcomeType(outcome)
	}
}

// getParameterFromOutcome extracts parameter from outcome string
func (p *Parser) getParameterFromOutcome(outcome string) string {
	if strings.Contains(outcome, "total_") {
		parts := strings.Split(outcome, "_")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	if strings.Contains(outcome, "exact_") {
		parts := strings.Split(outcome, "_")
		if len(parts) > 1 {
			return parts[1]
		}
	}
	return ""
}

// createHierarchicalMatchFromCornerEvent creates a hierarchical match structure from corner event data
func (p *Parser) createHierarchicalMatchFromCornerEvent(cornerEvent FonbetEvent, odds map[string]float64) *models.Match {
	now := time.Now()
	
	// For corner events, determine match name based on teams
	var matchName string
	if cornerEvent.HomeTeam != "" && cornerEvent.AwayTeam != "" {
		matchName = fmt.Sprintf("%s vs %s", cornerEvent.HomeTeam, cornerEvent.AwayTeam)
	} else {
		matchName = cornerEvent.Name
	}
	
	// Create match
	match := &models.Match{
		ID:         fmt.Sprintf("%d", cornerEvent.ID),
		Name:       matchName,
		HomeTeam:   cornerEvent.HomeTeam,
		AwayTeam:   cornerEvent.AwayTeam,
		StartTime:  cornerEvent.StartTime,
		Sport:      "football",
		Tournament: "Unknown Tournament",
		Bookmaker:  "Fonbet",
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Create corner event
	eventModel := models.Event{
		ID:         fmt.Sprintf("%d_corners", cornerEvent.ID),
		EventType:  "corners",
		MarketName: "Corners",
		Bookmaker:  "Fonbet",
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	
	// Create outcomes
	for outcomeType, oddsValue := range odds {
		outcome := models.Outcome{
			ID:          fmt.Sprintf("%d_corners_%s_%s", cornerEvent.ID, outcomeType, p.getParameterFromOutcome(outcomeType)),
			OutcomeType: string(p.getStandardOutcomeType(outcomeType)),
			Parameter:   p.getParameterFromOutcome(outcomeType),
			Odds:        oddsValue,
			Bookmaker:   "Fonbet",
			CreatedAt:   now,
			UpdatedAt:  now,
		}
		eventModel.Outcomes = append(eventModel.Outcomes, outcome)
	}
	
	match.Events = append(match.Events, eventModel)
	return match
}

// storeHierarchicalMatch stores match using hierarchical structure
func (p *Parser) storeHierarchicalMatch(ctx context.Context, match *models.Match) error {
	if p.ydbClient == nil {
		return fmt.Errorf("YDB client is not available")
	}
	
	if err := p.ydbClient.StoreMatch(ctx, match); err != nil {
		return fmt.Errorf("failed to store match: %w", err)
	}
	
	fmt.Printf("Successfully stored hierarchical match %s\n", match.ID)
	return nil
}


