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
	ydbClient, err := storage.NewYDBClient(&config.YDB)
	if err != nil {
		fmt.Printf("Warning: Failed to create YDB client: %v\n", err)
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
		case 910, 912: // Total goals over/under
			if factor.Pt != "" {
				odds[fmt.Sprintf("total_%s", factor.Pt)] = factor.V
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
	
	// Extract team names from event name
	homeTeam, awayTeam := p.extractTeamNames(event.Name)
	
	// Create match
	match := &models.Match{
		ID:         event.ID,
		Name:       event.Name,
		HomeTeam:   homeTeam,
		AwayTeam:   awayTeam,
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
	
	// Create match
	match := &models.Match{
		ID:         fmt.Sprintf("%d", cornerEvent.ID),
		Name:       cornerEvent.Name,
		HomeTeam:   "Unknown Home",
		AwayTeam:   "Unknown Away",
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


