package calculator

import (
	"testing"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

func TestMatchGroupKey_SameMatchDifferentBookmakerNames(t *testing.T) {
	start := time.Date(2026, 2, 13, 19, 30, 0, 0, time.UTC)

	// Fonbet: "Hades" vs "Heist"
	fonbet := models.Match{
		ID:        "hades|heist|2026-02-13T19:30:00Z",
		HomeTeam:  "Hades",
		AwayTeam:  "Heist",
		StartTime: start,
		Sport:     "football",
	}

	// 1xbet/Pinnacle888: "RC Hades" vs "K.S.K. Heist"
	xbet := models.Match{
		ID:        "ksk heist|rc hades|2026-02-13T19:30:00Z",
		HomeTeam:  "RC Hades",
		AwayTeam:  "K.S.K. Heist",
		StartTime: start,
		Sport:     "football",
	}

	k1 := matchGroupKey(fonbet)
	k2 := matchGroupKey(xbet)

	if k1 != k2 {
		t.Errorf("same match should have same group key: fonbet=%q xbet=%q", k1, k2)
	}
}

func TestNormalizeTeam_StripPrefixes(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"RC Hades", "hades"},
		{"Hades", "hades"},
		{"K.S.K. Heist", "heist"},
		{"Heist", "heist"},
		{"FC Barcelona", "barcelona"},
		{"  rc   Hades  ", "hades"},
	}
	for _, tt := range tests {
		got := normalizeTeam(tt.in)
		if got != tt.want {
			t.Errorf("normalizeTeam(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
