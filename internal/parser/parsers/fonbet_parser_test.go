package parsers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// MockYDBClient for testing
type MockYDBClient struct{}

func (m *MockYDBClient) StoreOdd(ctx context.Context, odd *models.Odd) error {
	return nil
}

func (m *MockYDBClient) GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error) {
	return []*models.Odd{}, nil
}

func (m *MockYDBClient) GetAllMatches(ctx context.Context) ([]string, error) {
	return []string{}, nil
}

func (m *MockYDBClient) Close() error {
	return nil
}

// createTestParser creates a parser for testing
func createTestParser() (*FonbetParser, *MockYDBClient) {
	cfg := &config.Config{
		ValueCalculator: config.ValueCalculatorConfig{
			Sports: []string{"football"},
		},
		Parser: config.ParserConfig{
			Timeout: 30 * time.Second,
		},
	}

	mockYDB := &MockYDBClient{}
	
	// Create parser with mock client
	parser := &FonbetParser{
		BaseParser: &BaseParser{
			ydbClient: mockYDB,
			config:    cfg,
			name:      "Fonbet",
		},
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    "https://fon.bet",
	}
	
	return parser, mockYDB
}

func TestFonbetParser_parseSportEvents(t *testing.T) {
	parser, _ := createTestParser()

	// Test parsing football events
	err := parser.parseSportEvents(enums.Football)
	if err != nil {
		t.Errorf("parseSportEvents failed: %v", err)
	}
}

func TestFonbetParser_getEventOdds(t *testing.T) {
	parser, _ := createTestParser()

	// Test getting odds for football event
	odds, err := parser.getEventOdds("test-event-1", enums.Football)
	if err != nil {
		t.Errorf("getEventOdds failed: %v", err)
	}

	// Should return empty slice for now (no mocks)
	if len(odds) != 0 {
		t.Errorf("Expected empty odds slice, got %d items", len(odds))
	}
}

func TestFonbetParser_parseEvent(t *testing.T) {
	parser, _ := createTestParser()

	// Create test event
	event := FonbetEvent{
		ID:         "test-event-1",
		Name:       "Test Match",
		HomeTeam:   "Team A",
		AwayTeam:   "Team B",
		StartTime:  time.Now().Add(2 * time.Hour),
		Category:   "football",
		Tournament: "Test League",
	}

	// Test parsing event
	err := parser.parseEvent(event)
	if err != nil {
		t.Errorf("parseEvent failed: %v", err)
	}
}

func TestFonbetParser_Start(t *testing.T) {
	parser, _ := createTestParser()

	// Test starting parser
	ctx := context.Background()
	err := parser.Start(ctx)
	if err != nil {
		t.Errorf("Start failed: %v", err)
	}
}

func TestFonbetParser_Stop(t *testing.T) {
	parser, _ := createTestParser()

	// Test stopping parser
	err := parser.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

// Test with mock data for development
func TestFonbetParser_WithMockData(t *testing.T) {
	parser, _ := createTestParser()

	// Test with mock events
	mockEvents := []FonbetEvent{
		{
			ID:         "mock-event-1",
			Name:       "Mock Match 1",
			HomeTeam:   "Team A",
			AwayTeam:   "Team B",
			StartTime:  time.Now().Add(2 * time.Hour),
			Category:   "football",
			Tournament: "Mock League",
		},
		{
			ID:         "mock-event-2",
			Name:       "Mock Match 2",
			HomeTeam:   "Team C",
			AwayTeam:   "Team D",
			StartTime:  time.Now().Add(4 * time.Hour),
			Category:   "football",
			Tournament: "Mock League",
		},
	}

	// Test parsing each mock event
	for _, event := range mockEvents {
		err := parser.parseEvent(event)
		if err != nil {
			t.Errorf("parseEvent failed for %s: %v", event.ID, err)
		}
	}
}
