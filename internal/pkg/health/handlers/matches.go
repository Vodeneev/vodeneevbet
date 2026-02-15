package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

type GetMatchesFunc func() []models.Match

var getMatchesFunc GetMatchesFunc

func SetGetMatchesFunc(fn GetMatchesFunc) {
	getMatchesFunc = fn
}

type GetEsportsMatchesFunc func() []models.EsportsMatch

var getEsportsMatchesFunc GetEsportsMatchesFunc

func SetGetEsportsMatchesFunc(fn GetEsportsMatchesFunc) {
	getEsportsMatchesFunc = fn
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

	slog.Info("Retrieved matches from memory", "count", matchCount, "duration", duration)

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"matches": matches,
		"meta": map[string]interface{}{
			"count":    matchCount,
			"duration": duration.String(),
			"source":   "memory",
		},
	}); err != nil {
		slog.Error("Failed to encode matches", "error", err)
		http.Error(w, fmt.Sprintf("Failed to encode matches: %v", err), http.StatusInternalServerError)
		return
	}
}

// HandleEsportsMatches returns cached esports matches (киберспорт, отдельно от футбола)
func HandleEsportsMatches(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var matches []models.EsportsMatch
	if getEsportsMatchesFunc != nil {
		matches = getEsportsMatchesFunc()
	}
	duration := time.Since(startTime)
	w.Header().Set("X-Query-Duration", duration.String())
	w.Header().Set("X-Matches-Count", fmt.Sprintf("%d", len(matches)))
	w.Header().Set("X-Source", "memory")

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"matches": matches,
		"meta": map[string]interface{}{
			"count":    len(matches),
			"duration": duration.String(),
			"source":   "memory",
		},
	}); err != nil {
		slog.Error("Failed to encode esports matches", "error", err)
		http.Error(w, fmt.Sprintf("Failed to encode esports matches: %v", err), http.StatusInternalServerError)
		return
	}
}
