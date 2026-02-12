package calculator

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// computeTopDiffs calculates differences in odds between bookmakers for the same bets.
// Returns top diffs sorted by diff_percent descending.
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
			
			// Log when comparing statistical events (not main_match)
			if evType != "" && evType != "main_match" {
				bookmakersList := make([]string, 0, len(byBook))
				oddsList := make([]string, 0, len(byBook))
				for bk, odd := range byBook {
					bookmakersList = append(bookmakersList, bk)
					oddsList = append(oddsList, fmt.Sprintf("%s:%.3f", bk, odd))
				}
				slog.Info("Calculator: comparing statistical event",
					"match", gm.name,
					"event_type", evType,
					"outcome_type", outType,
					"parameter", param,
					"bookmakers", strings.Join(bookmakersList, ", "),
					"bookmakers_count", len(byBook),
					"odds", strings.Join(oddsList, ", "))
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

// computeValueBets calculates value bets using weighted average of ALL bookmakers.
// For each bet, it calculates fair probability from all bookmakers (weighted average),
// then finds value bets where bookmaker odds are higher than fair odds.
func computeValueBets(matches []models.Match, bookmakerWeights map[string]float64, minValuePercent float64, keepTop int) []ValueBet {
	if keepTop <= 0 {
		keepTop = 100
	}
	if minValuePercent <= 0 {
		minValuePercent = 5.0 // Default: 5% minimum value
	}

	// Default weight is 1.0 if not specified
	getWeight := func(bookmaker string) float64 {
		if bookmakerWeights != nil {
			if w, ok := bookmakerWeights[strings.ToLower(bookmaker)]; ok && w > 0 {
				return w
			}
		}
		return 1.0 // Default weight
	}

	now := time.Now()

	// matchGroupKey -> betKey -> bookmaker -> odd
	type betMap map[string]map[string]float64
	groups := map[string]betMap{}

	// Metadata for group
	type groupMeta struct {
		name      string
		startTime time.Time
		sport     string
	}
	meta := map[string]groupMeta{}

	// Collect all odds
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

				// Keep best (max) odd per bookmaker+bet
				bkLower := strings.ToLower(bk)
				if prev, ok := groups[gk][betKey][bkLower]; !ok || odd > prev {
					groups[gk][betKey][bkLower] = odd
				}
			}
		}
	}

	var valueBets []ValueBet

	// For each match group and bet
	for gk, bets := range groups {
		gm := meta[gk]
		for betKey, byBook := range bets {
			// Need at least 2 bookmakers to calculate fair probability
			if len(byBook) < 2 {
				continue
			}

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
			
			// Log when comparing statistical events (not main_match)
			if evType != "" && evType != "main_match" {
				bookmakersList := make([]string, 0, len(byBook))
				oddsList := make([]string, 0, len(byBook))
				for bk, odd := range byBook {
					bookmakersList = append(bookmakersList, bk)
					oddsList = append(oddsList, fmt.Sprintf("%s:%.3f", bk, odd))
				}
				slog.Info("Calculator: comparing statistical event for value bets",
					"match", gm.name,
					"event_type", evType,
					"outcome_type", outType,
					"parameter", param,
					"bookmakers", strings.Join(bookmakersList, ", "),
					"bookmakers_count", len(byBook),
					"odds", strings.Join(oddsList, ", "))
			}

			// Calculate fair probability using weighted average of ALL bookmakers
			// Convert odds to probabilities: prob = 1 / odd
			var totalWeightedProb float64
			var totalWeight float64
			var allBookmakers []string
			var allOdds []float64

			for bk, odd := range byBook {
				prob := 1.0 / odd
				weight := getWeight(bk)
				totalWeightedProb += prob * weight
				totalWeight += weight
				allBookmakers = append(allBookmakers, bk)
				allOdds = append(allOdds, odd)
			}

			if totalWeight <= 0 {
				continue
			}

			// Fair probability (weighted average from all bookmakers)
			fairProb := totalWeightedProb / totalWeight
			if fairProb <= 0 || fairProb >= 1 {
				continue // Invalid probability
			}

			// Fair odd
			fairOdd := 1.0 / fairProb

			// Find value bets: compare each bookmaker with fair odd
			for i, bk := range allBookmakers {
				odd := allOdds[i]

				// Calculate value: (bookmaker_odd / fair_odd - 1) * 100
				valuePercent := (odd/fairOdd - 1.0) * 100.0

				// Only include if value is positive and above threshold
				if valuePercent < minValuePercent {
					continue
				}

				// Calculate expected value: (bookmaker_odd * fair_probability) - 1
				expectedValue := (odd * fairProb) - 1.0

				// Create map of all bookmaker odds for this outcome
				allOddsMap := make(map[string]float64)
				for i, b := range allBookmakers {
					allOddsMap[b] = allOdds[i]
				}

				valueBets = append(valueBets, ValueBet{
					MatchGroupKey:    gk,
					MatchName:        gm.name,
					StartTime:        gm.startTime,
					Sport:            gm.sport,
					EventType:        evType,
					OutcomeType:      outType,
					Parameter:        param,
					BetKey:           betKey,
					AllBookmakerOdds: allOddsMap, // Все коэффициенты от всех контор для этого исхода
					FairOdd:          fairOdd,
					FairProbability:  fairProb,
					Bookmaker:        bk,
					BookmakerOdd:     odd,
					ValuePercent:     valuePercent,
					ExpectedValue:    expectedValue,
					CalculatedAt:     now,
				})
			}
		}
	}

	// Sort by value percent (descending)
	sort.Slice(valueBets, func(i, j int) bool {
		return valueBets[i].ValuePercent > valueBets[j].ValuePercent
	})

	if len(valueBets) > keepTop {
		valueBets = valueBets[:keepTop]
	}

	return valueBets
}
