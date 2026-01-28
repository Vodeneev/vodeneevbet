package calculator

import (
	"math"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// matchGroupKey creates a unique key for grouping matches from different bookmakers.
// Format: "sport|team1|team2|start_time" where teams are sorted alphabetically.
func matchGroupKey(m models.Match) string {
	home := normalizeTeam(m.HomeTeam)
	away := normalizeTeam(m.AwayTeam)
	if home == "" || away == "" {
		// fallback to name parsing if teams are missing
		n := strings.TrimSpace(m.Name)
		if n != "" {
			if h, a, ok := splitTeamsFromName(n); ok {
				home = normalizeTeam(h)
				away = normalizeTeam(a)
			}
		}
	}
	if home == "" || away == "" {
		return ""
	}

	// Normalize team order (sort alphabetically) to handle different home/away assignments
	// This ensures same match from different bookmakers gets same group key
	teams := []string{home, away}
	if teams[0] > teams[1] {
		teams[0], teams[1] = teams[1], teams[0]
	}

	sport := strings.ToLower(strings.TrimSpace(m.Sport))
	if sport == "" {
		sport = "unknown"
	}

	// Time rounding to tolerate small differences between APIs.
	t := m.StartTime.UTC().Truncate(30 * time.Minute)
	if t.IsZero() {
		// If no start time, group only by teams.
		return sport + "|" + teams[0] + "|" + teams[1]
	}
	return sport + "|" + teams[0] + "|" + teams[1] + "|" + t.Format(time.RFC3339)
}

// normalizeTeam normalizes team name for comparison.
func normalizeTeam(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// collapse whitespace
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// splitTeamsFromName extracts team names from match name string.
// Supports separators: " vs ", " - ", " — ", " – "
func splitTeamsFromName(name string) (string, string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", false
	}
	separators := []string{" vs ", " - ", " — ", " – "}
	for _, sep := range separators {
		parts := strings.Split(name, sep)
		if len(parts) != 2 {
			continue
		}
		home := strings.TrimSpace(parts[0])
		away := strings.TrimSpace(parts[1])
		if home == "" || away == "" {
			return "", "", false
		}
		return home, away, true
	}
	return "", "", false
}

// isFinitePositiveOdd checks if a value is a valid positive odd (> 1.0).
func isFinitePositiveOdd(v float64) bool {
	return v > 1.000001 && !math.IsInf(v, 0) && !math.IsNaN(v)
}
