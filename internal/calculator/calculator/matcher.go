package calculator

import (
	"math"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// matchGroupKey creates a unique key for grouping matches from different bookmakers.
// Format: "sport|team1|team2|start_time"
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

	sport := strings.ToLower(strings.TrimSpace(m.Sport))
	if sport == "" {
		sport = "unknown"
	}

	// Time rounding to tolerate small differences between APIs.
	t := m.StartTime.UTC().Truncate(30 * time.Minute)
	if t.IsZero() {
		// If no start time, group only by teams.
		return sport + "|" + home + "|" + away
	}
	return sport + "|" + home + "|" + away + "|" + t.Format(time.RFC3339)
}

// teamNamePrefixes are stripped for grouping so "RC Hades" and "Hades" match the same match.
var teamNamePrefixes = []string{
	"r.c. ", "rc ", "k.s.k. ", "k.s. k. ", "ksk ", "f.c. ", "fc ", "f.k. ", "fk ",
	"c.f. ", "cf ", "s.c. ", "sc ", "s.s.c. ", "ssc ", "a.c. ", "ac ", "a.s. ", "as ",
	"u.d. ", "ud ", "c.d. ", "cd ", "n.k. ", "nk ", "b.c. ", "bc ", "bk ",
}

// normalizeTeam normalizes team name for comparison and grouping.
// Strips common club prefixes (RC, K.S.K., FC, etc.) so "RC Hades" and "Hades" get the same key.
func normalizeTeam(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// Strip known prefixes (order: try longer first)
	for _, p := range teamNamePrefixes {
		if strings.HasPrefix(s, p) {
			s = strings.TrimSpace(s[len(p):])
			break
		}
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
