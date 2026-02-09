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

func TestCanonicalMatchID_CrossBookmakerMatching(t *testing.T) {
	t1 := time.Date(2026, 2, 10, 18, 15, 0, 0, time.UTC)

	tests := []struct {
		name  string
		home1 string
		away1 string
		home2 string
		away2 string
	}{
		{"Hyphen normalization", "Al-Hilal", "Al Wahda", "Al Hilal", "Al Wahda"},
		{"SFC suffix", "Al-Hilal SFC", "Shabab Al Ahli", "Al-Hilal", "Shabab Al Ahli Dubai"},
		{"City suffix (Jeddah)", "Al-Ittihad", "Al Gharafa", "Al-Ittihad Jeddah", "Al Gharafa"},
		{"City suffix (Tehran)", "Esteghlal", "Al Hussein SC", "Esteghlal Tehran", "Al Hussein"},
		{"Transliteration", "Tractor", "Al Sadd", "Traktor Sazi", "Al Sadd"},
		{"Preposition 'de'", "Guarany de Bage", "Monsoon", "Guarany Bage", "Monsoon"},
		{"Preposition 'de' (Estudiantes)", "Estudiantes de La Plata", "Deportivo Riestra", "Estudiantes La Plata", "Deportivo Riestra"},
		{"Racing variants", "Banfield", "Racing Club", "Banfield", "Racing Avellaneda"},
		{"Ulsan variants", "Ulsan HD", "Melbourne City", "Ulsan Hyundai", "Melbourne City"},
		{"Sharjah variants", "Al Duhail", "Sharjah FC", "Al Duhail", "Al Sharjah"},
		{"Tottenham", "Tottenham Hotspur", "Newcastle United", "Tottenham", "Newcastle"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id1 := CanonicalMatchID(tt.home1, tt.away1, t1)
			id2 := CanonicalMatchID(tt.home2, tt.away2, t1)
			if id1 != id2 {
				t.Errorf("IDs should match:\n  %s vs %s → %s\n  %s vs %s → %s",
					tt.home1, tt.away1, id1, tt.home2, tt.away2, id2)
			}
		})
	}
}
