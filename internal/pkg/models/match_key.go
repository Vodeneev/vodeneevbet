package models

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

//go:embed team_patterns.json
var teamPatternsJSON []byte

var (
	teamPatternsOnce sync.Once
	teamPatterns     map[string]string
)

// CanonicalMatchID builds a stable cross-bookmaker match identifier.
//
// IMPORTANT: this assumes team names are in the same language/format across sources.
// For best results, keep both parsers in English (e.g. Fonbet lang=en, Pinnacle is English).
// Format: team1|team2|time (sport removed as we only work with football)
// Teams are sorted alphabetically to handle different home/away assignments across bookmakers
func CanonicalMatchID(homeTeam, awayTeam string, startTime time.Time) string {
	home := normalizeKeyPart(homeTeam)
	away := normalizeKeyPart(awayTeam)

	// Normalize team order (sort alphabetically) to handle different home/away assignments
	// This ensures same match from different bookmakers gets same ID
	if home > away {
		home, away = away, home
	}

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
	// Normalize hyphens to spaces for consistent matching (Al-Hilal → al hilal)
	s = strings.ReplaceAll(s, "-", " ")
	// Normalize apostrophes - remove them for consistent matching (Newell's → Newells, Queen's → Queens)
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, "’", "") // Also handle typographic apostrophe
	// Normalize dots - remove them for consistent matching (D.C. → DC)
	s = strings.ReplaceAll(s, ".", "")
	s = strings.Join(strings.Fields(s), " ")
	// Keep it URL-friendly-ish (YDB key is Utf8, but we still avoid odd whitespace).
	s = strings.ReplaceAll(s, "/", " ")
	s = strings.ReplaceAll(s, "\\", " ")
	s = strings.ReplaceAll(s, "|", " ")
	s = strings.Join(strings.Fields(s), " ")

	// Remove prepositions that differ between bookmakers (e.g. "de", "da", "del")
	s = removePrepositions(s)

	// Normalize team name using intelligent extraction of key words
	s = normalizeTeamName(s)

	return s
}

// removePrepositions removes common prepositions that vary between bookmakers.
// "Internacional de Palmira" → "Internacional Palmira", "Vasco da Gama" → "Vasco Gama"
func removePrepositions(s string) string {
	preps := map[string]bool{
		"de": true, "da": true, "do": true, "di": true, "del": true, "la": true,
	}
	words := strings.Fields(s)
	filtered := make([]string, 0, len(words))
	for _, w := range words {
		if !preps[w] {
			filtered = append(filtered, w)
		}
	}
	if len(filtered) == 0 {
		return s
	}
	return strings.Join(filtered, " ")
}

// normalizeTeamName normalizes team names by extracting key words and removing common suffixes
func normalizeTeamName(name string) string {
	// First, check for known full name variations (before processing)
	normalized := applyKnownFullNameVariations(name)
	if normalized != name {
		return normalized
	}

	// Common suffixes to remove (club designations, etc.)
	commonSuffixes := map[string]bool{
		"fc": true, "cf": true, "c.f.": true, "c f": true,
		"club": true, "football club": true, "f.c.": true,
		"ac": true, "a.c.": true, "a c": true,
		"as": true, "a.s.": true, "a s": true,
		"sc": true, "s.c.": true, "s c": true,
		"sfc": true, "s.f.c.": true, // e.g. Al-Hilal SFC
		"bk": true, "b.k.": true, // e.g. Odense BK
		"bb": true, "b.b.": true, // e.g. Erzurumspor BB
		"hd": true, // e.g. Ulsan HD (Hyundai)
		"ff": true, "f.f.": true,
		"fk": true, "f.k.": true,
		"if": true, "i.f.": true,
		"og": true, "o.g.": true,
		"cp": true, // e.g. Sporting CP
		"ca": true, // e.g. CA Tigre
		"cd": true, // e.g. CD Tenerife
	}

	// Common generic words that don't help identify teams
	genericWords := map[string]bool{
		"united": true, "city": true, "town": true,
		"rovers": true, "wanderers": true,
		"athletic": true, "athletico": true, "atletico": true,
		"sporting": true, "sports": true,
	}

	// Split into words
	words := strings.Fields(name)
	if len(words) == 0 {
		return name
	}

	// Remove only suffixes first, then check for known combinations
	// This preserves generic words that might be part of team names
	wordsWithoutSuffixes := make([]string, 0, len(words))
	for _, word := range words {
		word = strings.Trim(word, ".,-")
		if word == "" {
			continue
		}
		wordLower := strings.ToLower(word)
		if !commonSuffixes[wordLower] {
			wordsWithoutSuffixes = append(wordsWithoutSuffixes, word)
		}
	}

	// Check if name without suffixes matches a known combination
	if len(wordsWithoutSuffixes) > 0 {
		nameWithoutSuffixes := strings.ToLower(strings.Join(wordsWithoutSuffixes, " "))
		knownName := applyKnownFullNameVariations(nameWithoutSuffixes)
		// Check if this is a known pattern (even if normalization returns the same value)
		if isKnownPattern(nameWithoutSuffixes) {
			return knownName
		}
	}

	// Now filter out generic words for teams not in known combinations
	filteredWords := make([]string, 0, len(wordsWithoutSuffixes))
	for _, word := range wordsWithoutSuffixes {
		wordLower := strings.ToLower(word)
		if !genericWords[wordLower] {
			filteredWords = append(filteredWords, word)
		}
	}

	// If we removed everything, keep original
	if len(filteredWords) == 0 {
		return name
	}

	// Extract key identifier
	// For multi-word names, take first 2 words to ensure uniqueness
	// Examples: "Union Saint-Gilloise" -> "union saint-gilloise", "Union Berlin" -> "union berlin"
	// But for known teams like "Bayern Munich", we use special handling (already done above)

	// Common prefixes that should be skipped (take next word instead)
	commonPrefixes := map[string]bool{
		"fc": true, "cf": true, "ac": true, "as": true, "sc": true,
		"real": true, "atletico": true, "athletic": true,
	}

	// If first word is a common prefix, skip it
	startIdx := 0
	if len(filteredWords) > 0 && commonPrefixes[strings.ToLower(filteredWords[0])] {
		startIdx = 1
	}

	// Take first 2 words (or all if less than 2) for uniqueness
	// This ensures "Union Saint-Gilloise" != "Union Berlin"
	var keyWords []string
	if startIdx < len(filteredWords) {
		endIdx := startIdx + 2
		if endIdx > len(filteredWords) {
			endIdx = len(filteredWords)
		}
		keyWords = filteredWords[startIdx:endIdx]
	}

	if len(keyWords) == 0 {
		return name
	}

	// Join words and apply known word-level normalizations
	normalized = strings.Join(keyWords, " ")
	normalized = applyKnownWordVariations(strings.ToLower(normalized))

	return normalized
}

// isKnownPattern checks if a name matches a known team name pattern
func isKnownPattern(name string) bool {
	fullNamePatterns := getFullNamePatterns()

	// Check for exact match
	if _, ok := fullNamePatterns[name]; ok {
		return true
	}

	// Check if name starts with any pattern
	for pattern := range fullNamePatterns {
		if strings.HasPrefix(name, pattern+" ") || name == pattern {
			return true
		}
		if strings.HasPrefix(name, pattern) && len(name) > len(pattern) {
			nextChar := name[len(pattern)]
			if nextChar == ' ' || nextChar == '-' {
				return true
			}
		}
	}

	return false
}

// loadTeamPatterns loads team name patterns from JSON file
func loadTeamPatterns() {
	var data struct {
		Patterns map[string]string `json:"patterns"`
	}
	if err := json.Unmarshal(teamPatternsJSON, &data); err != nil {
		// If loading fails, use empty map (fallback to default behavior)
		teamPatterns = make(map[string]string)
		return
	}
	teamPatterns = data.Patterns
}

// getFullNamePatterns returns the map of known team name patterns
// Patterns are loaded from team_patterns.json file
func getFullNamePatterns() map[string]string {
	teamPatternsOnce.Do(loadTeamPatterns)
	return teamPatterns
}

// applyKnownFullNameVariations handles complete team name patterns
func applyKnownFullNameVariations(name string) string {
	fullNamePatterns := getFullNamePatterns()

	// Check for exact matches
	if normalized, ok := fullNamePatterns[name]; ok {
		return normalized
	}

	// Check if name starts with any pattern (handles cases like "Bayern Munich FC")
	for pattern, normalized := range fullNamePatterns {
		if strings.HasPrefix(name, pattern+" ") || name == pattern {
			return normalized
		}
		// Check if pattern is prefix followed by space or hyphen
		if strings.HasPrefix(name, pattern) && len(name) > len(pattern) {
			nextChar := name[len(pattern)]
			if nextChar == ' ' || nextChar == '-' {
				return normalized
			}
		}
	}

	return name
}

// applyKnownWordVariations handles single-word normalizations
func applyKnownWordVariations(word string) string {
	// Map of word-level variations
	wordVariations := map[string]string{
		"man": "manchester",
		"utd": "manchester",
	}

	if normalized, ok := wordVariations[word]; ok {
		return normalized
	}

	return word
}
