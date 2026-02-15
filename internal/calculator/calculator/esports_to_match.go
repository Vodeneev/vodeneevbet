package calculator

import (
	"strings"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// EsportsMatchesToMatches converts esports matches to the unified models.Match form
// so they can be fed into computeTopDiffs and computeValueBets together with football.
func EsportsMatchesToMatches(esports []models.EsportsMatch) []models.Match {
	if len(esports) == 0 {
		return nil
	}
	out := make([]models.Match, 0, len(esports))
	for i := range esports {
		m := esportsMatchToMatch(&esports[i])
		out = append(out, m)
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
