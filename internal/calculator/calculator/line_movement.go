package calculator

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// computeAndStoreLineMovements builds current odds per (match, bet, bookmaker), compares with
// last snapshot from storage, detects significant changes (any direction), stores current snapshot,
// and returns line movements where |current - previous| >= threshold (absolute).
func computeAndStoreLineMovements(ctx context.Context, matches []models.Match, snapshotStorage storage.OddsSnapshotStorage, thresholdAbs float64) ([]LineMovement, error) {
	if snapshotStorage == nil || thresholdAbs <= 0 {
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
				prevOdd, _, err := snapshotStorage.GetLastOddsSnapshot(ctx, gk, betKey, bookmaker)
				if err != nil {
					slog.Debug("GetLastOddsSnapshot failed", "match", gk, "bet", betKey, "bookmaker", bookmaker, "error", err)
				}

				err = snapshotStorage.StoreOddsSnapshot(ctx, gk, gm.name, gm.sport, evType, outType, param, betKey, bookmaker, gm.startTime, currentOdd, now)
				if err != nil {
					slog.Warn("StoreOddsSnapshot failed", "match", gk, "bet", betKey, "bookmaker", bookmaker, "error", err)
				}

				if prevOdd > 0 {
					changeAbs := currentOdd - prevOdd
					if changeAbs < 0 {
						changeAbs = -changeAbs
					}
					if changeAbs >= thresholdAbs {
						movements = append(movements, LineMovement{
							MatchGroupKey: gk,
							MatchName:     gm.name,
							StartTime:     gm.startTime,
							Sport:         gm.sport,
							EventType:     evType,
							OutcomeType:   outType,
							Parameter:     param,
							BetKey:        betKey,
							Bookmaker:     bookmaker,
							PreviousOdd:   prevOdd,
							CurrentOdd:    currentOdd,
							ChangeAbs:     currentOdd - prevOdd, // signed: + рост, - падение
							RecordedAt:    now,
						})
					}
				}
			}
		}
	}

	return movements, nil
}
