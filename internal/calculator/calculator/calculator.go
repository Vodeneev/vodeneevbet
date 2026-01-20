package calculator

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

type ydbReader interface {
	GetAllMatches(ctx context.Context) ([]models.Match, error)
	GetMatchesWithLimit(ctx context.Context, limit int) ([]models.Match, error)
}

// ValueCalculator reads odds from YDB and calculates top diffs between bookmakers.
// It keeps a small in-memory cache and exposes it via HTTP handlers.
type ValueCalculator struct {
	ydb ydbReader
	cfg *config.ValueCalculatorConfig

	mu          sync.RWMutex
	topDiffs    []DiffBet
	lastCalcAt  time.Time
	lastCalcErr string
}

func NewValueCalculator(ydb ydbReader, cfg *config.ValueCalculatorConfig) *ValueCalculator {
	// If caller passes a typed-nil pointer (e.g. (*storage.YDBClient)(nil)),
	// it becomes a non-nil interface and would panic on method calls.
	if isNilReader(ydb) {
		ydb = nil
	}
	return &ValueCalculator{
		ydb: ydb,
		cfg: cfg,
	}
}

func isNilReader(r ydbReader) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	// Most likely a pointer receiver.
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return true
	}
	return false
}

func (c *ValueCalculator) Start(ctx context.Context) error {
	interval := parseInterval(c.cfg)
	if interval <= 0 {
		interval = 30 * time.Second
	}

	// Initial calculation.
	c.recalculate(ctx)

	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			c.recalculate(ctx)
		}
	}
}

// RegisterHTTP registers calculator endpoints onto mux.
func (c *ValueCalculator) RegisterHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/diffs/top", c.handleTopDiffs)
	mux.HandleFunc("/diffs/status", c.handleStatus)
}

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

	c.mu.RLock()
	diffs := c.topDiffs
	c.mu.RUnlock()

	if diffs == nil {
		diffs = []DiffBet{}
	}
	if limit > len(diffs) {
		limit = len(diffs)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(diffs[:limit])
}

func (c *ValueCalculator) handleStatus(w http.ResponseWriter, r *http.Request) {
	c.mu.RLock()
	lastAt := c.lastCalcAt
	lastErr := c.lastCalcErr
	count := len(c.topDiffs)
	c.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"last_calculated_at": lastAt,
		"last_error":         lastErr,
		"cached_items":       count,
	})
}

func (c *ValueCalculator) recalculate(ctx context.Context) {
	calcAt := time.Now()

	if c.ydb == nil {
		c.mu.Lock()
		c.topDiffs = []DiffBet{}
		c.lastCalcAt = calcAt
		c.lastCalcErr = "ydb is not configured"
		c.mu.Unlock()
		return
	}

	// Avoid long-hanging reads.
	readCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	matches, err := c.ydb.GetAllMatches(readCtx)
	if err != nil {
		// Try a safer fallback.
		matches, err = c.ydb.GetMatchesWithLimit(readCtx, 2000)
	}
	if err != nil {
		log.Printf("calculator: failed to load matches: %v", err)
		c.mu.Lock()
		c.lastCalcAt = calcAt
		c.lastCalcErr = err.Error()
		c.mu.Unlock()
		return
	}

	top := computeTopDiffs(matches, 100)

	c.mu.Lock()
	c.topDiffs = top
	c.lastCalcAt = calcAt
	c.lastCalcErr = ""
	c.mu.Unlock()
}

func parseInterval(cfg *config.ValueCalculatorConfig) time.Duration {
	// Prefer check_interval, fallback to test_interval, then default.
	if cfg == nil {
		return 0
	}
	if cfg.CheckInterval != "" {
		if d, err := time.ParseDuration(strings.TrimSpace(cfg.CheckInterval)); err == nil {
			return d
		}
	}
	if cfg.TestInterval != "" {
		if d, err := time.ParseDuration(strings.TrimSpace(cfg.TestInterval)); err == nil {
			return d
		}
	}
	return 0
}

func computeTopDiffs(matches []models.Match, keepTop int) []DiffBet {
	if keepTop <= 0 {
		keepTop = 100
	}
	now := time.Now()

	// matchGroupKey -> betKey -> bookmaker -> odd
	type betMap map[string]map[string]float64
	groups := map[string]betMap{}

	// Some metadata for group: choose "best" human-readable match fields (first seen is fine).
	type groupMeta struct {
		name      string
		startTime time.Time
		sport     string
	}
	meta := map[string]groupMeta{}

	for i := range matches {
		m := matches[i]
		gk := matchGroupKey(m)
		if gk == "" {
			continue
		}
		if _, ok := meta[gk]; !ok {
			meta[gk] = groupMeta{
				name:      strings.TrimSpace(m.HomeTeam) + " vs " + strings.TrimSpace(m.AwayTeam),
				startTime: m.StartTime,
				sport:     m.Sport,
			}
		}
		if _, ok := groups[gk]; !ok {
			groups[gk] = betMap{}
		}

		for _, ev := range m.Events {
			for _, out := range ev.Outcomes {
				bk := strings.TrimSpace(out.Bookmaker)
				if bk == "" {
					bk = strings.TrimSpace(ev.Bookmaker)
				}
				if bk == "" {
					bk = strings.TrimSpace(m.Bookmaker)
				}
				if bk == "" {
					continue
				}

				odd := out.Odds
				if !isFinitePositiveOdd(odd) {
					continue
				}

				eventType := strings.TrimSpace(ev.EventType)
				outcomeType := strings.TrimSpace(out.OutcomeType)
				param := strings.TrimSpace(out.Parameter)
				if eventType == "" || outcomeType == "" {
					continue
				}

				betKey := eventType + "|" + outcomeType + "|" + param
				if _, ok := groups[gk][betKey]; !ok {
					groups[gk][betKey] = map[string]float64{}
				}

				// Keep latest/maximum? For diffs we just keep the best (max) seen per bookmaker+bet.
				if prev, ok := groups[gk][betKey][bk]; !ok || odd > prev {
					groups[gk][betKey][bk] = odd
				}
			}
		}
	}

	var diffs []DiffBet
	for gk, bets := range groups {
		gm := meta[gk]
		for betKey, byBook := range bets {
			if len(byBook) < 2 {
				continue
			}
			minOdd := math.MaxFloat64
			maxOdd := -math.MaxFloat64
			minBk, maxBk := "", ""
			for bk, odd := range byBook {
				if odd < minOdd {
					minOdd = odd
					minBk = bk
				}
				if odd > maxOdd {
					maxOdd = odd
					maxBk = bk
				}
			}
			if minOdd <= 0 || maxOdd <= 0 || maxOdd <= minOdd {
				continue
			}

			diffAbs := maxOdd - minOdd
			diffPct := (maxOdd/minOdd - 1.0) * 100.0

			parts := strings.SplitN(betKey, "|", 3)
			evType, outType, param := "", "", ""
			if len(parts) >= 1 {
				evType = parts[0]
			}
			if len(parts) >= 2 {
				outType = parts[1]
			}
			if len(parts) >= 3 {
				param = parts[2]
			}

			diffs = append(diffs, DiffBet{
				MatchGroupKey: gk,
				MatchName:     gm.name,
				StartTime:     gm.startTime,
				Sport:         gm.sport,
				EventType:     evType,
				OutcomeType:   outType,
				Parameter:     param,
				BetKey:        betKey,
				Bookmakers:    len(byBook),
				MinBookmaker:  minBk,
				MinOdd:        minOdd,
				MaxBookmaker:  maxBk,
				MaxOdd:        maxOdd,
				DiffAbs:       diffAbs,
				DiffPercent:   diffPct,
				CalculatedAt:  now,
			})
		}
	}

	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].DiffPercent > diffs[j].DiffPercent
	})

	if len(diffs) > keepTop {
		diffs = diffs[:keepTop]
	}
	return diffs
}

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

func normalizeTeam(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	// collapse whitespace
	s = strings.Join(strings.Fields(s), " ")
	return s
}

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

func isFinitePositiveOdd(v float64) bool {
	return v > 1.000001 && !math.IsInf(v, 0) && !math.IsNaN(v)
}

