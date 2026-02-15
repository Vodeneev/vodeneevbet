package line

import (
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// ToModelsMatch converts a unified line.Match to the football storage model models.Match.
// Для киберспорта используйте ToEsportsMatch(), чтобы не смешивать с футбольной моделью.
func (m *Match) ToModelsMatch() *models.Match {
	if m == nil {
		return nil
	}
	now := time.Now()
	matchID := models.CanonicalMatchID(m.HomeTeam, m.AwayTeam, m.StartTime)
	name := fmt.Sprintf("%s vs %s", m.HomeTeam, m.AwayTeam)

	events := make([]models.Event, 0, len(m.Markets))
	for _, market := range m.Markets {
		eventID := fmt.Sprintf("%s_%s_%s", matchID, m.Bookmaker, market.EventType)
		marketName := market.MarketName
		if marketName == "" {
			marketName = models.GetMarketName(models.StandardEventType(market.EventType))
		}
		ev := models.Event{
			ID:         eventID,
			MatchID:    matchID,
			EventType:  market.EventType,
			MarketName: marketName,
			Bookmaker:  m.Bookmaker,
			Outcomes:   make([]models.Outcome, 0, len(market.Outcomes)),
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		for _, o := range market.Outcomes {
			outcomeID := fmt.Sprintf("%s_%s_%s", eventID, o.OutcomeType, o.Parameter)
			ev.Outcomes = append(ev.Outcomes, models.Outcome{
				ID:          outcomeID,
				EventID:     eventID,
				OutcomeType: o.OutcomeType,
				Parameter:   o.Parameter,
				Odds:        o.Odds,
				Bookmaker:   m.Bookmaker,
				CreatedAt:   now,
				UpdatedAt:   now,
			})
		}
		if len(ev.Outcomes) > 0 {
			events = append(events, ev)
		}
	}

	return &models.Match{
		ID:         matchID,
		Name:       name,
		HomeTeam:   m.HomeTeam,
		AwayTeam:   m.AwayTeam,
		StartTime:  m.StartTime,
		Sport:      m.Sport,
		Tournament: m.League,
		Bookmaker:  m.Bookmaker,
		Events:     events,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// ToEsportsMatch converts a unified line.Match to the esports model models.EsportsMatch.
// Используется для киберспорта (dota2, cs); футбольная модель Match не трогается.
func (m *Match) ToEsportsMatch() *models.EsportsMatch {
	if m == nil {
		return nil
	}
	now := time.Now()
	matchID := models.CanonicalMatchID(m.HomeTeam, m.AwayTeam, m.StartTime)
	name := fmt.Sprintf("%s vs %s", m.HomeTeam, m.AwayTeam)

	markets := make([]models.EsportsMarket, 0, len(m.Markets))
	for _, market := range m.Markets {
		marketID := fmt.Sprintf("%s_%s_%s", matchID, m.Bookmaker, market.EventType)
		em := models.EsportsMarket{
			ID:         marketID,
			MatchID:    matchID,
			MarketType: market.EventType,
			MarketName: market.MarketName,
			Bookmaker:  m.Bookmaker,
			Outcomes:   make([]models.EsportsOutcome, 0, len(market.Outcomes)),
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		for _, o := range market.Outcomes {
			outcomeID := fmt.Sprintf("%s_%s_%s", marketID, o.OutcomeType, o.Parameter)
			em.Outcomes = append(em.Outcomes, models.EsportsOutcome{
				ID:          outcomeID,
				MarketID:    marketID,
				OutcomeType: o.OutcomeType,
				Parameter:   o.Parameter,
				Odds:        o.Odds,
				Bookmaker:   m.Bookmaker,
				CreatedAt:   now,
				UpdatedAt:   now,
			})
		}
		if len(em.Outcomes) > 0 {
			markets = append(markets, em)
		}
	}

	return &models.EsportsMatch{
		ID:         matchID,
		Name:       name,
		HomeTeam:   m.HomeTeam,
		AwayTeam:   m.AwayTeam,
		StartTime:  m.StartTime,
		Discipline: m.Sport,
		Tournament: m.League,
		Bookmaker:  m.Bookmaker,
		Markets:    markets,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}
