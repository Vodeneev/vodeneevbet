package tests

import (
	"testing"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers/fonbet"
)

// createTestParser creates a parser for testing
func createTestParser() *fonbet.ParserWrapper {
	cfg := &config.Config{
		ValueCalculator: config.ValueCalculatorConfig{
			Sports: []string{"football"},
		},
		Parser: config.ParserConfig{
			Timeout: 30 * time.Second,
		},
	}

	return fonbet.NewParserWrapper(cfg)
}

func TestFonbetParser_Stop(t *testing.T) {
	parser := createTestParser()

	// Test stopping parser
	err := parser.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}
