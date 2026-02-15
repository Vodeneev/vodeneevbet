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

// handleTopDiffs returns top differences in odds between bookmakers
func (c *ValueCalculator) handleTopDiffs(w http.ResponseWriter, r *http.Request) {
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
	var diffs []DiffBet
	if c.httpClient == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "parser URL is not configured"})
		return
	}

	// Create context with timeout for the request
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	matches, err := c.httpClient.GetMatchesAll(ctx)
	if err != nil {
		slog.Error("Failed to load matches in handleTopDiffs", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to fetch matches from parser", "details": err.Error()})
		return
	}

	// Calculate diffs from fresh data
	diffs = computeTopDiffs(matches, 100)
	logStatisticalEventsSummary(matches)

	// Filter by status if specified
	// Use UTC for comparison to handle timezones correctly (StartTime is stored in UTC)
	now := time.Now().UTC()
	// Matches typically last up to 2-3 hours, so exclude matches that started more than 3 hours ago
	maxLiveAge := 3 * time.Hour
	if statusFilter != "" {
		filtered := make([]DiffBet, 0, len(diffs))
		for _, diff := range diffs {
			// Match is live if it has started (StartTime is in the past) but not too long ago
			// StartTime is stored in UTC, so we compare with UTC time
			// Use Before with equal check to handle edge cases
			hasStarted := !diff.StartTime.IsZero() && (diff.StartTime.Before(now) || diff.StartTime.Equal(now))
			notTooOld := !diff.StartTime.IsZero() && now.Sub(diff.StartTime) <= maxLiveAge
			isLive := hasStarted && notTooOld
			switch statusFilter {
			case "live":
				if isLive {
					filtered = append(filtered, diff)
				}
			case "upcoming":
				// Upcoming means match hasn't started yet (StartTime is in the future)
				if !hasStarted {
					filtered = append(filtered, diff)
				}
			default:
				// Unknown status filter, return all
				filtered = append(filtered, diff)
			}
		}
		diffs = filtered
	}

	// Re-sort after filtering (computeTopDiffs already sorts, but we filter after)
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].DiffPercent > diffs[j].DiffPercent
	})

	if limit > len(diffs) {
		limit = len(diffs)
	}

	w.Header().Set("Content-Type", "application/json")
	if len(diffs) > 0 {
		_ = json.NewEncoder(w).Encode(diffs[:limit])
	} else {
		_ = json.NewEncoder(w).Encode([]DiffBet{})
	}
}

// handleStatus returns calculator status information
func (c *ValueCalculator) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Status endpoint - data is fetched on-demand, no caching
	status := map[string]any{
		"status":            "ok",
		"parser_configured": c.httpClient != nil,
		"mode":              "on-demand",
		"async_running":     c.IsAsyncRunning(),
	}
	if c.httpClient == nil {
		status["error"] = "parser URL is not configured"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}
