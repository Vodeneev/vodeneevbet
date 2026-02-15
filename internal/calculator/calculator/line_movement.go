package calculator

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// computeAndStoreLineMovements builds current odds per (match, bet, bookmaker), compares current
// with stored max_odd and min_odd (so gradual moves like 4.15→4.0→3.45 are caught as 4.15→3.45),
// stores current snapshot (updating max/min), and returns line movements. Threshold is in percent
// (e.g. 5.0 = 5%) so 1.9→1.5 (~21%) matters more than 9.5→9.1 (~4%).
func computeAndStoreLineMovements(ctx context.Context, matches []models.Match, snapshotStorage storage.OddsSnapshotStorage, thresholdPercent float64) ([]LineMovement, error) {
	if snapshotStorage == nil || thresholdPercent <= 0 {
		return nil, nil
	}

	now := time.Now()

	// matchGroupKey -> betKey -> bookmaker -> odd
	type betMap map[string]map[string]float64
	groups := map[string]betMap{}
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
				if prev, ok := groups[gk][betKey][bk]; !ok || odd > prev {
					groups[gk][betKey][bk] = odd
				}
			}
		}
	}

	var movements []LineMovement
	for gk, bets := range groups {
		gm := meta[gk]
		for betKey, byBook := range bets {
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

			for bookmaker, currentOdd := range byBook {
				_, maxOdd, minOdd, _, err := snapshotStorage.GetLastOddsSnapshot(ctx, gk, betKey, bookmaker)
				if err != nil {
					slog.Debug("GetLastOddsSnapshot failed", "match", gk, "bet", betKey, "bookmaker", bookmaker, "error", err)
				}

				// Compare with extremes in percent: (current - ref) / ref * 100
				if maxOdd > 0 {
					dropPercent := (maxOdd - currentOdd) / maxOdd * 100
					if dropPercent >= thresholdPercent {
						changeAbs := currentOdd - maxOdd
						movements = append(movements, LineMovement{
							MatchGroupKey:   gk,
							MatchName:       gm.name,
							StartTime:       gm.startTime,
							Sport:           gm.sport,
							EventType:       evType,
							OutcomeType:     outType,
							Parameter:       param,
							BetKey:          betKey,
							Bookmaker:       bookmaker,
							PreviousOdd:     maxOdd,
							CurrentOdd:      currentOdd,
							ChangeAbs:       changeAbs,
							ChangePercent:   changeAbs / maxOdd * 100,
							RecordedAt:      now,
						})
					}
				}
				if minOdd > 0 {
					risePercent := (currentOdd - minOdd) / minOdd * 100
					if risePercent >= thresholdPercent {
						changeAbs := currentOdd - minOdd
						movements = append(movements, LineMovement{
							MatchGroupKey:   gk,
							MatchName:       gm.name,
							StartTime:       gm.startTime,
							Sport:           gm.sport,
							EventType:       evType,
							OutcomeType:     outType,
							Parameter:       param,
							BetKey:          betKey,
							Bookmaker:       bookmaker,
							PreviousOdd:     minOdd,
							CurrentOdd:      currentOdd,
							ChangeAbs:       changeAbs,
							ChangePercent:   changeAbs / minOdd * 100,
							RecordedAt:      now,
						})
					}
				}

				err = snapshotStorage.StoreOddsSnapshot(ctx, gk, gm.name, gm.sport, evType, outType, param, betKey, bookmaker, gm.startTime, currentOdd, now)
				if err != nil {
					slog.Warn("StoreOddsSnapshot failed", "match", gk, "bet", betKey, "bookmaker", bookmaker, "error", err)
				}
				if err := snapshotStorage.AppendOddsHistory(ctx, gk, betKey, bookmaker, gm.startTime, currentOdd, now); err != nil {
					slog.Debug("AppendOddsHistory failed", "match", gk, "bet", betKey, "bookmaker", bookmaker, "error", err)
				}
			}
		}
	}

	return movements, nil
}
