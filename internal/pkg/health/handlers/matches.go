package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

type GetMatchesFunc func() []models.Match

var getMatchesFunc GetMatchesFunc

func SetGetMatchesFunc(fn GetMatchesFunc) {
	getMatchesFunc = fn
}

// HandleMatches returns cached matches (parsing runs continuously in background)
func HandleMatches(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.URL.Query().Get("test") != "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"test": "DEPLOYED_V2",
		})
		return
	}

	var matches []models.Match
	if getMatchesFunc != nil {
		matches = getMatchesFunc()
	}

	duration := time.Since(startTime)
	matchCount := len(matches)

	w.Header().Set("X-Query-Duration", duration.String())
	w.Header().Set("X-Matches-Count", fmt.Sprintf("%d", matchCount))
	w.Header().Set("X-Source", "memory")

	log.Printf("✅ Retrieved %d matches from memory in %v", matchCount, duration)

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"matches": matches,
		"meta": map[string]interface{}{
			"count":    matchCount,
			"duration": duration.String(),
			"source":   "memory",
		},
	}); err != nil {
		log.Printf("❌ Failed to encode matches: %v", err)
		http.Error(w, fmt.Sprintf("Failed to encode matches: %v", err), http.StatusInternalServerError)
		return
	}
}
