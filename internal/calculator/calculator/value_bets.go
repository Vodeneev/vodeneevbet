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

// handleTopValueBets returns top value bets calculated using weighted average of all bookmakers
func (c *ValueCalculator) handleTopValueBets(w http.ResponseWriter, r *http.Request) {
	limit := 5
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 50 {
				n = 50
			}
			limit = n
		}
	}

	// Filter by match status: "live" (started), "upcoming" (not started), or empty (all)
	statusFilter := r.URL.Query().Get("status")

	// Fetch fresh data from parser on each request
	var valueBets []ValueBet
	if c.httpClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "parser URL is not configured"})
		return
	}

	// Get bookmaker weights from config (optional - defaults to 1.0 for all)
	// We use ALL bookmakers with weighted average
	var bookmakerWeights map[string]float64
	if c.cfg != nil && c.cfg.BookmakerWeights != nil {
		bookmakerWeights = c.cfg.BookmakerWeights
	}

	minValuePercent := 5.0 // Default
	if c.cfg != nil && c.cfg.MinValuePercent > 0 {
		minValuePercent = c.cfg.MinValuePercent
	}

	// Create context with timeout for the request
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	matches, err := c.httpClient.GetMatches(ctx)
	if err != nil {
		slog.Error("Failed to load matches in handleTopValueBets", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch matches from parser", "details": err.Error()})
		return
	}

	// Calculate value bets using weighted average
	valueBets = computeValueBets(matches, bookmakerWeights, minValuePercent, 100)

	// Filter by status if specified
	now := time.Now().UTC()
	maxLiveAge := 3 * time.Hour
	if statusFilter != "" {
		filtered := make([]ValueBet, 0, len(valueBets))
		for _, vb := range valueBets {
			hasStarted := !vb.StartTime.IsZero() && (vb.StartTime.Before(now) || vb.StartTime.Equal(now))
			notTooOld := !vb.StartTime.IsZero() && now.Sub(vb.StartTime) <= maxLiveAge
			isLive := hasStarted && notTooOld
			switch statusFilter {
			case "live":
				if isLive {
					filtered = append(filtered, vb)
				}
			case "upcoming":
				if !hasStarted {
					filtered = append(filtered, vb)
				}
			default:
				filtered = append(filtered, vb)
			}
		}
		valueBets = filtered
	}

	// Re-sort after filtering
	sort.Slice(valueBets, func(i, j int) bool {
		return valueBets[i].ValuePercent > valueBets[j].ValuePercent
	})

	if limit > len(valueBets) {
		limit = len(valueBets)
	}

	w.Header().Set("Content-Type", "application/json")
	if len(valueBets) > 0 {
		_ = json.NewEncoder(w).Encode(valueBets[:limit])
	} else {
		_ = json.NewEncoder(w).Encode([]ValueBet{})
	}
}
