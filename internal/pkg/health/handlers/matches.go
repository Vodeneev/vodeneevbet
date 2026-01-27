package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/parserutil"
)

// GetMatchesFunc is a function type for getting matches
type GetMatchesFunc func() []models.Match

var getMatchesFunc GetMatchesFunc

// SetGetMatchesFunc sets the function to get matches
func SetGetMatchesFunc(fn GetMatchesFunc) {
	getMatchesFunc = fn
}

// GetParsersFunc is a function type for getting registered parsers
type GetParsersFunc func() []interfaces.Parser

var getParsersFunc GetParsersFunc

// SetGetParsersFunc sets the function to get parsers
func SetGetParsersFunc(fn GetParsersFunc) {
	getParsersFunc = fn
}

// triggerParsingAsync triggers parsing for all parsers asynchronously (non-blocking)
// Uses a separate context with timeout to allow parsing to complete even after HTTP request ends
func triggerParsingAsync() {
	if getParsersFunc == nil {
		return
	}
	parsers := getParsersFunc()

	if len(parsers) == 0 {
		return
	}

	// Create a separate context with timeout for parsing (60 seconds should be enough for Pinnacle)
	// This allows parsing to continue even after HTTP request completes
	parseCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	// Configure options for async (non-blocking) execution
	opts := parserutil.AsyncRunOptions()
	opts.OnError = func(p interfaces.Parser, err error) {
		log.Printf("On-demand parsing failed for %s: %v", p.GetName(), err)
	}
	opts.OnComplete = func() {
		// Cancel context after all parsers complete (or timeout)
		cancel()
	}

	// Run parsers asynchronously - don't wait for completion
	_ = parserutil.RunParsers(parseCtx, parsers, func(ctx context.Context, p interfaces.Parser) error {
		return p.ParseOnce(ctx)
	}, opts)
}

// HandleMatches handles /matches endpoint
// Flow: request -> async parsing to bookmakers (non-blocking) -> return cached data immediately
// Fresh data will be available on next request
func HandleMatches(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Check for test parameter - return hardcoded word to verify deployment
	if r.URL.Query().Get("test") != "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"test": "DEPLOYED_V2",
		})
		return
	}

	// Trigger asynchronous parsing to all bookmakers in parallel (non-blocking)
	// Don't wait - return cached data immediately, fresh data will be available on next request
	triggerParsingAsync()

	// Get all matches from in-memory store (returns current cached data)
	// Fresh data from async parsing will be available on next request
	var matches []models.Match
	if getMatchesFunc != nil {
		matches = getMatchesFunc()
	}

	duration := time.Since(startTime)
	matchCount := len(matches)

	// Add performance headers
	w.Header().Set("X-Query-Duration", duration.String())
	w.Header().Set("X-Matches-Count", fmt.Sprintf("%d", matchCount))
	w.Header().Set("X-Source", "memory") // Indicate data comes from memory

	log.Printf("✅ Retrieved %d matches from memory in %v", matchCount, duration)

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"matches": matches,
		"meta": map[string]interface{}{
			"count":    matchCount,
			"duration": duration.String(),
			"source":   "memory", // Data comes from in-memory store
		},
	}); err != nil {
		log.Printf("❌ Failed to encode matches: %v", err)
		http.Error(w, fmt.Sprintf("Failed to encode matches: %v", err), http.StatusInternalServerError)
		return
	}
}
