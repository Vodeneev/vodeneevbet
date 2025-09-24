package tests

import (
	"context"
	"testing"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/enums"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers/fonbet"
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
func createTestParser() (*fonbet.ParserWrapper, *MockYDBClient) {
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
	parser := fonbet.NewParserWrapper(mockYDB, cfg)
	
	return parser, mockYDB
}

func TestFonbetParser_Stop(t *testing.T) {
	parser, _ := createTestParser()

	// Test stopping parser
	err := parser.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}
