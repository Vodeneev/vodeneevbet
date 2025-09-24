package parsers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// FonbetParser implements parser for Fonbet bookmaker
type FonbetParser struct {
	*BaseParser
	httpClient *http.Client
	baseURL    string
}

// NewFonbetParser creates a new Fonbet parser instance
func NewFonbetParser(ydbClient *storage.YDBWorkingClient, config *config.Config) *FonbetParser {
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
	
	// Parse football events
	if err := p.parseFootballEvents(); err != nil {
		return fmt.Errorf("failed to parse football events: %w", err)
	}
	
	return nil
}

// Stop stops the parser
func (p *FonbetParser) Stop() error {
	fmt.Println("Stopping Fonbet parser...")
	return nil
}

// parseFootballEvents parses football events from Fonbet
func (p *FonbetParser) parseFootballEvents() error {
	fmt.Println("Parsing football events from Fonbet...")
	
	// Get football events list
	events, err := p.getFootballEvents()
	if err != nil {
		return fmt.Errorf("failed to get football events: %w", err)
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

// getFootballEvents retrieves football events from Fonbet
func (p *FonbetParser) getFootballEvents() ([]FonbetEvent, error) {
	url := fmt.Sprintf("%s/sports/football?dateInterval=3", p.baseURL)
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	// Set headers to mimic browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	
	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
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
	// This is a simplified implementation
	// In real scenario, you would use HTML parsing library like goquery
	// For now, we'll create mock events for testing
	
	events := []FonbetEvent{
		{
			ID:          "test-event-1",
			Name:        "Test Match 1",
			HomeTeam:    "Team A",
			AwayTeam:    "Team B",
			StartTime:   time.Now().Add(2 * time.Hour),
			Category:    "football",
			Tournament:  "Test League",
		},
		{
			ID:          "test-event-2", 
			Name:        "Test Match 2",
			HomeTeam:    "Team C",
			AwayTeam:    "Team D",
			StartTime:   time.Now().Add(4 * time.Hour),
			Category:    "football",
			Tournament:  "Test League",
		},
	}
	
	return events, nil
}

// parseEvent parses a specific event and extracts odds
func (p *FonbetParser) parseEvent(event FonbetEvent) error {
	fmt.Printf("Parsing event: %s vs %s\n", event.HomeTeam, event.AwayTeam)
	
	// Get odds for the event
	odds, err := p.getEventOdds(event.ID)
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
func (p *FonbetParser) getEventOdds(eventID string) ([]*models.Odd, error) {
	// This would be the actual API call to get odds
	// For now, we'll create mock odds for testing
	
	odds := []*models.Odd{
		{
			MatchID:   eventID,
			Bookmaker: "Fonbet",
			Market:    "1X2",
			Outcomes: map[string]float64{
				"win_a": 1.85,
				"draw":  3.20,
				"win_b": 4.10,
			},
			UpdatedAt: time.Now(),
			MatchName: fmt.Sprintf("Test Match %s", eventID),
			MatchTime: time.Now().Add(2 * time.Hour),
			Sport:     "football",
		},
	}
	
	return odds, nil
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
