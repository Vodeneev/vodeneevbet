package calculator

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// handleTopLineMovements returns top line movements (прогрузы) — largest odds changes in the same bookmaker.
func (c *ValueCalculator) handleTopLineMovements(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 50 {
				n = 50
			}
			limit = n
		}
	}

	if c.httpClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "parser URL is not configured"})
		return
	}
	if c.oddsSnapshotStorage == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "line movement storage is not configured (enable line_movement_enabled)"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	matches, err := c.httpClient.GetMatches(ctx)
	if err != nil {
		slog.Error("Failed to load matches in handleTopLineMovements", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch matches from parser", "details": err.Error()})
		return
	}

	movements, err := getLineMovementsForTop(ctx, matches, c.oddsSnapshotStorage)
	if err != nil {
		slog.Error("getLineMovementsForTop failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to compute line movements", "details": err.Error()})
		return
	}

	// Exclude movements where current odds > 8 (high odds прогрузы not needed)
	const maxCurrentOdd = 8.0
	filtered := movements[:0]
	for _, m := range movements {
		if m.CurrentOdd <= maxCurrentOdd {
			filtered = append(filtered, m)
		}
	}
	movements = filtered

	// Sort by absolute change percent descending (largest movements first)
	sort.Slice(movements, func(i, j int) bool {
		absI := movements[i].ChangePercent
		if absI < 0 {
			absI = -absI
		}
		absJ := movements[j].ChangePercent
		if absJ < 0 {
			absJ = -absJ
		}
		return absI > absJ
	})

	if limit > len(movements) {
		limit = len(movements)
	}

	w.Header().Set("Content-Type", "application/json")
	if len(movements) > 0 {
		_ = json.NewEncoder(w).Encode(movements[:limit])
	} else {
		_ = json.NewEncoder(w).Encode([]LineMovement{})
	}
}
