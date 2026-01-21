package models

import (
	"testing"
	"time"
)

func TestNormalizeTeamName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"bayern munich", "bayern"},
		{"fc bayern munich", "bayern"},
		{"fc bayern", "bayern"},
		{"bayern", "bayern"},
		{"manchester united", "manchester united"},
		{"man united", "manchester united"},
		{"liverpool fc", "liverpool"},
		{"liverpool", "liverpool"},
	}

	for _, tt := range tests {
		result := normalizeTeamName(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeTeamName(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestCanonicalMatchID_BayernNormalization(t *testing.T) {
	t1 := time.Date(2026, 1, 21, 20, 0, 0, 0, time.UTC)

	id1 := CanonicalMatchID("Bayern", "Union Saint-Gilloise", t1)
	id2 := CanonicalMatchID("Bayern Munich", "Union Saint-Gilloise", t1)

	if id1 != id2 {
		t.Errorf("CanonicalMatchID should normalize Bayern and Bayern Munich to same ID:\n  Bayern: %s\n  Bayern Munich: %s", id1, id2)
	}
}
