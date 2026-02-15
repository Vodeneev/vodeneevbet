package calculator

import (
	"strings"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// EsportsConversionSummary holds counts and samples after converting esports to Match.
type EsportsConversionSummary struct {
	MatchCount       int
	ByDiscipline     map[string]int   // dota2, cs
	ByBookmaker      map[string]int   // fonbet, 1xbet
	TotalEvents      int
	UniqueEventTypes []string
	SampleMatches    []string // first 5 "name (discipline)"
}

// EsportsMatchesToMatches converts esports matches to the unified models.Match form
// so they can be fed into computeTopDiffs and computeValueBets together with football.
// Summary is filled for logging (how many merged, by discipline, event types, etc.).
func EsportsMatchesToMatches(esports []models.EsportsMatch, summary *EsportsConversionSummary) []models.Match {
	if len(esports) == 0 {
		return nil
	}
	if summary != nil {
		summary.ByDiscipline = make(map[string]int)
		summary.ByBookmaker = make(map[string]int)
	}
	eventTypeSet := make(map[string]struct{})
	out := make([]models.Match, 0, len(esports))
	for i := range esports {
		e := &esports[i]
		m := esportsMatchToMatch(e)
		out = append(out, m)
		if summary != nil {
			summary.MatchCount++
			disc := strings.TrimSpace(e.Discipline)
			if disc == "" {
				disc = "unknown"
			}
			summary.ByDiscipline[disc]++
			bk := strings.TrimSpace(e.Bookmaker)
			if bk == "" {
				bk = "unknown"
			}
			summary.ByBookmaker[bk]++
			for _, ev := range e.Markets {
				summary.TotalEvents++
				et := strings.TrimSpace(ev.MarketType)
				if et != "" {
					eventTypeSet[et] = struct{}{}
				}
			}
			if len(summary.SampleMatches) < 5 {
				summary.SampleMatches = append(summary.SampleMatches, e.Name+" ("+disc+")")
			}
		}
	}
	if summary != nil {
		summary.UniqueEventTypes = make([]string, 0, len(eventTypeSet))
		for et := range eventTypeSet {
			summary.UniqueEventTypes = append(summary.UniqueEventTypes, et)
		}
	}
	return out
}

func esportsMatchToMatch(e *models.EsportsMatch) models.Match {
	events := make([]models.Event, 0, len(e.Markets))
	for j := range e.Markets {
		ev := esportsMarketToEvent(e.Markets[j], e.ID, e.Bookmaker)
		events = append(events, ev)
	}
	return models.Match{
		ID:        e.ID,
		Name:      strings.TrimSpace(e.Name),
		HomeTeam:  strings.TrimSpace(e.HomeTeam),
		AwayTeam:  strings.TrimSpace(e.AwayTeam),
		StartTime: e.StartTime,
		Sport:     strings.TrimSpace(e.Discipline), // dota2, cs — used in matchGroupKey
		Tournament: strings.TrimSpace(e.Tournament),
		Bookmaker:  strings.TrimSpace(e.Bookmaker),
		Events:     events,
		CreatedAt:  e.CreatedAt,
		UpdatedAt:  e.UpdatedAt,
	}
}

func esportsMarketToEvent(m models.EsportsMarket, matchID, fallbackBookmaker string) models.Event {
	outcomes := make([]models.Outcome, 0, len(m.Outcomes))
	for k := range m.Outcomes {
		o := m.Outcomes[k]
		bk := strings.TrimSpace(o.Bookmaker)
		if bk == "" {
			bk = strings.TrimSpace(m.Bookmaker)
		}
		if bk == "" {
			bk = strings.TrimSpace(fallbackBookmaker)
		}
		outcomes = append(outcomes, models.Outcome{
			ID:          o.ID,
			EventID:     m.ID,
			OutcomeType: strings.TrimSpace(o.OutcomeType),
			Parameter:   strings.TrimSpace(o.Parameter),
			Odds:        o.Odds,
			Bookmaker:   bk,
			CreatedAt:   o.CreatedAt,
			UpdatedAt:   o.UpdatedAt,
		})
	}
	bk := strings.TrimSpace(m.Bookmaker)
	if bk == "" {
		bk = strings.TrimSpace(fallbackBookmaker)
	}
	return models.Event{
		ID:         m.ID,
		MatchID:    matchID,
		EventType:  strings.TrimSpace(m.MarketType),  // main_match, total_maps, etc.
		MarketName: strings.TrimSpace(m.MarketName),
		Bookmaker:  bk,
		Outcomes:   outcomes,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}
