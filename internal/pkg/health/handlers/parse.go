package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
)

var getParsersFunc func() []interfaces.Parser

func SetGetParsersFunc(fn func() []interfaces.Parser) {
	getParsersFunc = fn
}

// HandleParse triggers parsing for a specific parser or all parsers
// GET /parse?parser=pinnacle888 - parse specific parser
// GET /parse - parse all parsers
func HandleParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	parserName := r.URL.Query().Get("parser")
	var parsers []interfaces.Parser
	if getParsersFunc != nil {
		parsers = getParsersFunc()
	}

	if len(parsers) == 0 {
		http.Error(w, `{"error": "no parsers registered"}`, http.StatusInternalServerError)
		return
	}

	var targetParsers []interfaces.Parser
	var results []map[string]interface{}

	if parserName != "" {
		// Parse specific parser
		parserName = strings.ToLower(strings.TrimSpace(parserName))
		found := false
		for _, p := range parsers {
			if strings.ToLower(p.GetName()) == parserName {
				found = true
				targetParsers = append(targetParsers, p)
				break
			}
		}
		if !found {
			http.Error(w, fmt.Sprintf(`{"error": "parser '%s' not found"}`, parserName), http.StatusNotFound)
			return
		}
	} else {
		// Parse all parsers
		targetParsers = parsers
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Run parsing
	for _, p := range targetParsers {
		parser := p.(interfaces.Parser)

		startTime := time.Now()
		log.Printf("Manual parse triggered for %s", parser.GetName())
		
		err := parser.ParseOnce(ctx)
		duration := time.Since(startTime)

		result := map[string]interface{}{
			"parser":   parser.GetName(),
			"duration": duration.String(),
			"success":  err == nil,
		}
		if err != nil {
			result["error"] = err.Error()
			log.Printf("Manual parse failed for %s: %v (took %v)", parser.GetName(), err, duration)
		} else {
			log.Printf("Manual parse completed for %s (took %v)", parser.GetName(), duration)
		}
		results = append(results, result)
	}

	response := map[string]interface{}{
		"results": results,
		"count":   len(results),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Failed to encode parse response: %v", err)
		http.Error(w, `{"error": "failed to encode response"}`, http.StatusInternalServerError)
		return
	}
}
