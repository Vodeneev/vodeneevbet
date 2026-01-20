package models

import (
	"strings"
	"time"
)

// CanonicalMatchID builds a stable cross-bookmaker match identifier.
//
// IMPORTANT: this assumes team names are in the same language/format across sources.
// For best results, keep both parsers in English (e.g. Fonbet lang=en, Pinnacle is English).
func CanonicalMatchID(sport, homeTeam, awayTeam string, startTime time.Time) string {
	sport = normalizeKeyPart(sport)
	if sport == "" {
		sport = "unknown"
	}
	home := normalizeKeyPart(homeTeam)
	away := normalizeKeyPart(awayTeam)

	// Round to reduce small API differences.
	t := startTime.UTC().Truncate(30 * time.Minute)
	ts := "unknown-time"
	if !t.IsZero() {
		ts = t.Format(time.RFC3339)
	}

	return sport + "|" + home + "|" + away + "|" + ts
}

func normalizeKeyPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	// Keep it URL-friendly-ish (YDB key is Utf8, but we still avoid odd whitespace).
	s = strings.ReplaceAll(s, "/", " ")
	s = strings.ReplaceAll(s, "\\", " ")
	s = strings.ReplaceAll(s, "|", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

