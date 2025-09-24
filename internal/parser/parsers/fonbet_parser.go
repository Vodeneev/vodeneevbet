package parsers

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// FonbetParser implements parser for Fonbet bookmaker
type FonbetParser struct {
	*BaseParser
	httpClient *http.Client
	baseURL    string
}

// NewFonbetParser creates a new Fonbet parser instance
func NewFonbetParser(ydbClient YDBClient, config *config.Config) *FonbetParser {
	httpClient := &http.Client{
		Timeout: config.Parser.Timeout,
	}

	return &FonbetParser{
		BaseParser: NewBaseParser(ydbClient, config, "Fonbet"),
		httpClient: httpClient,
		baseURL:    "https://fon.bet",
	}
}

// Start begins parsing process for Fonbet
func (p *FonbetParser) Start(ctx context.Context) error {
	fmt.Println("Starting Fonbet parser...")
	
	// Parse events for configured sports
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

// Stop stops the parser
func (p *FonbetParser) Stop() error {
	fmt.Println("Stopping Fonbet parser...")
	return nil
}

// parseSportEvents parses events for specific sport from Fonbet
func (p *FonbetParser) parseSportEvents(sport enums.Sport) error {
	fmt.Printf("Parsing %s events from Fonbet...\n", sport)
	
	// Get events list for sport
	events, err := p.getSportEvents(sport)
	if err != nil {
		return fmt.Errorf("failed to get %s events: %w", sport, err)
	}
	
	// Parse each event
	for _, event := range events {
		if err := p.parseEvent(event); err != nil {
			fmt.Printf("Failed to parse event %s: %v\n", event.ID, err)
			continue
		}
	}
	
	return nil
}

// getSportEvents retrieves events for specific sport from Fonbet
func (p *FonbetParser) getSportEvents(sport enums.Sport) ([]FonbetEvent, error) {
	url := fmt.Sprintf("%s/sports/%s?dateInterval=3", p.baseURL, sport.String())
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set headers from configuration
	req.Header.Set("User-Agent", p.config.Parser.UserAgent)
	for key, value := range p.config.Parser.Headers {
		req.Header.Set(key, value)
	}
	
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	
	// Read response body with gzip decompression
	var body []byte
	
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		
		body, err = io.ReadAll(gzReader)
		if err != nil {
			return nil, fmt.Errorf("failed to read gzipped response body: %w", err)
		}
	} else {
		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %w", err)
		}
	}
	
	// Parse HTML to extract events (simplified approach)
	events, err := p.parseHTMLForEvents(string(body))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}
	
	return events, nil
}

// parseHTMLForEvents extracts events from HTML response
func (p *FonbetParser) parseHTMLForEvents(html string) ([]FonbetEvent, error) {
	// TODO: Implement real HTML parsing using goquery
	// For now, return empty slice to see what we get from Fonbet
	fmt.Printf("Received HTML response length: %d characters\n", len(html))
	fmt.Printf("First 500 characters: %s\n", html[:min(500, len(html))])
	
	// Return empty slice for now - we'll implement real parsing later
	return []FonbetEvent{}, nil
}

// parseEvent parses a specific event and extracts odds
func (p *FonbetParser) parseEvent(event FonbetEvent) error {
	fmt.Printf("Parsing event: %s vs %s\n", event.HomeTeam, event.AwayTeam)
	
	// Get odds for the event
	odds, err := p.getEventOdds(event.ID, enums.Sport(event.Category))
	if err != nil {
		return fmt.Errorf("failed to get event odds: %w", err)
	}
	
	// Store odds in YDB
	for _, odd := range odds {
		if err := p.ydbClient.StoreOdd(context.Background(), odd); err != nil {
			fmt.Printf("Failed to store odd: %v\n", err)
			continue
		}
	}
	
	return nil
}

// getEventOdds retrieves odds for a specific event
func (p *FonbetParser) getEventOdds(eventID string, sport enums.Sport) ([]*models.Odd, error) {
	// TODO: Implement real API call to get odds from Fonbet
	// For now, return empty slice - we'll implement real odds parsing later
	fmt.Printf("Getting odds for event %s, sport %s\n", eventID, sport)
	
	// Return empty slice for now - we'll implement real odds parsing later
	return []*models.Odd{}, nil
}

// FonbetEvent represents a sports event from Fonbet
type FonbetEvent struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	HomeTeam   string    `json:"home_team"`
	AwayTeam   string    `json:"away_team"`
	StartTime  time.Time `json:"start_time"`
	Category   string    `json:"category"`
	Tournament string    `json:"tournament"`
}

// FonbetOddsResponse represents the response structure from Fonbet API
type FonbetOddsResponse struct {
	Events []FonbetEvent `json:"events"`
	Odds   []FonbetOdd   `json:"odds"`
}

// FonbetOdd represents a single odd from Fonbet
type FonbetOdd struct {
	EventID     string  `json:"event_id"`
	Market      string  `json:"market"`
	Outcome     string  `json:"outcome"`
	Coefficient float64 `json:"coefficient"`
	Timestamp   int64   `json:"timestamp"`
}
