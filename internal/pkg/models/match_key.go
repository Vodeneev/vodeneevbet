package models

import (
	"strings"
	"time"
)

// CanonicalMatchID builds a stable cross-bookmaker match identifier.
//
// IMPORTANT: this assumes team names are in the same language/format across sources.
// For best results, keep both parsers in English (e.g. Fonbet lang=en, Pinnacle is English).
// Format: home|away|time (sport removed as we only work with football)
func CanonicalMatchID(homeTeam, awayTeam string, startTime time.Time) string {
	home := normalizeKeyPart(homeTeam)
	away := normalizeKeyPart(awayTeam)

	// Use exact time without rounding
	ts := "unknown-time"
	if !startTime.IsZero() {
		ts = startTime.UTC().Format(time.RFC3339)
	}

	return home + "|" + away + "|" + ts
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

