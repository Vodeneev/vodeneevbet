package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// GetMatchesByNameFunc returns matches that match the given name (with full events and outcomes).
type GetMatchesByNameFunc func(name string) []models.Match

var getMatchesByNameFunc GetMatchesByNameFunc

// SetGetMatchesByNameFunc sets the function used by HandleMatchByName (e.g. health.GetMatchesByName).
func SetGetMatchesByNameFunc(fn GetMatchesByNameFunc) {
	getMatchesByNameFunc = fn
}

// HandleMatchByName returns matches whose name contains the query, with all events and coefficients.
// GET /match-by-name?name=Bayern — returns all matches matching "Bayern" with full events and odds.
func HandleMatchByName(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, `missing query parameter "name"`, http.StatusBadRequest)
		return
	}

	var matches []models.Match
	if getMatchesByNameFunc != nil {
		matches = getMatchesByNameFunc(name)
	} else if getMatchesFunc != nil {
		all := getMatchesFunc()
		q := strings.ToLower(name)
		for i := range all {
			m := &all[i]
			n := strings.ToLower(m.Name)
			home := strings.ToLower(m.HomeTeam)
			away := strings.ToLower(m.AwayTeam)
			if strings.Contains(n, q) || strings.Contains(home, q) || strings.Contains(away, q) ||
				strings.Contains(home+" - "+away, q) || strings.Contains(home+" vs "+away, q) {
				matches = append(matches, *m)
			}
		}
	}

	duration := time.Since(startTime)
	w.Header().Set("X-Query-Duration", duration.String())
	w.Header().Set("X-Matches-Count", fmt.Sprintf("%d", len(matches)))

	slog.Info("Match-by-name query", "name", name, "count", len(matches), "duration", duration)

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"matches": matches,
		"meta": map[string]interface{}{
			"query":    name,
			"count":    len(matches),
			"duration": duration.String(),
		},
	}); err != nil {
		slog.Error("Failed to encode match-by-name response", "error", err)
		http.Error(w, fmt.Sprintf("Failed to encode: %v", err), http.StatusInternalServerError)
		return
	}
}
