package leon

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

const bookmakerName = "Leon"

// Main market type IDs из league.sport.mainMarkets (футбол). Используем для однозначного отличия основной линии от угловых/карточек/таймов.
const (
	mainHandicapMarketTypeIDSoccer int64 = 1970324836975100 // "Фора" по голам (основное время)
	mainTotalMarketTypeIDSoccer   int64 = 1970324836974992 // "Тотал" по голам
	main1X2MarketTypeIDSoccer     int64 = 1970324836974645 // "Победитель" (1X2)
)

// Угловые: только маркеты по общему тоталу матча (без таймов и без тоталов по командам). По marketTypeId из API.
var cornersMainMarketTypeIDs = map[int64]bool{
	1970324836975158: true, // Тотал угловых (2 исхода)
	1970324836978008: true, // Тотал угловых (3 исхода)
	1970324836975160: true, // Кто подаст больше угловых
	1970324836978528: true, // Фора по угловым
	1970324836978807: true, // Точное количество угловых (0-5, 6-8, …)
	1970324836978517: true, // Двойной исход для угловых
}

const (
	cornersWhoMoreMarketTypeID    int64 = 1970324836975160 // Кто подаст больше угловых
	cornersExactCountMarketTypeID int64 = 1970324836978807 // Точное количество угловых
)

// Фолы: маркеты по матчу (тотал/кто больше/фора/количество по команде). По marketTypeId.
var foulsMainMarketTypeIDs = map[int64]bool{
	1970324837126390: true, // Количество фолов (по команде: Лидс, Манчестер Сити — exact_count)
	1970324837126376: true, // Количество фолов (по игроку — exact_count)
}

const foulsWhoMoreMarketTypeID int64 = 0 // TODO: подставить ID маркета "Кто больше фолов", когда будет известен

// Желтые карточки: только маркеты по матчу (без таймов, без тоталов по командам). По marketTypeId.
var yellowCardsMainMarketTypeIDs = map[int64]bool{
	1970324836978524: true, // Тотал желтых карточек
	1970324836978515: true, // Кто получит больше желтых карточек
	1970324836978523: true, // Фора по желтым карточкам
	1970324836978520: true, // Двойной исход для желтых карточек
}

const yellowCardsWhoMoreMarketTypeID int64 = 1970324836978515 // Кто получит больше желтых карточек

// LeonEventToMatch конвертирует LeonEvent (полный ответ event/all или элемент из events) в models.Match.
// Включает: main_match (1X2, тотал, фора), corners (тотал угловых, фора, кто больше), fouls (тотал фолов, фора, кто больше, количество по команде).
// Названия команд всегда берутся из ev.NameDefault (англ.) при наличии — для матчинга с другими конторами.
func LeonEventToMatch(ev *LeonEvent, leagueName string) *models.Match {
	if ev == nil {
		return nil
	}
	home, away := extractTeams(ev)
	if home == "" || away == "" {
		return nil
	}
	startTime := time.Unix(0, ev.Kickoff*int64(time.Millisecond)).UTC()
	if startTime.Before(time.Now().UTC()) {
		return nil
	}
	matchID := models.CanonicalMatchID(home, away, startTime)
	now := time.Now()
	match := &models.Match{
		ID:         matchID,
		Name:       fmt.Sprintf("%s vs %s", home, away),
		HomeTeam:   home,
		AwayTeam:   away,
		StartTime:  startTime,
		Sport:      "football",
		Tournament: leagueName,
		Bookmaker:  bookmakerName,
		Events:     []models.Event{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	mainEvent := buildMainEvent(matchID, ev, now)
	if len(mainEvent.Outcomes) > 0 {
		match.Events = append(match.Events, mainEvent)
	}
	if cornersEvent := buildStatisticalEvent(matchID, ev, now, models.StandardEventCorners, cornersMainMarketTypeIDs); len(cornersEvent.Outcomes) > 0 {
		match.Events = append(match.Events, cornersEvent)
	}
	if foulsEvent := buildStatisticalEvent(matchID, ev, now, models.StandardEventFouls, foulsMainMarketTypeIDs); len(foulsEvent.Outcomes) > 0 {
		match.Events = append(match.Events, foulsEvent)
	}
	if yellowCardsEvent := buildStatisticalEvent(matchID, ev, now, models.StandardEventYellowCards, yellowCardsMainMarketTypeIDs); len(yellowCardsEvent.Outcomes) > 0 {
		match.Events = append(match.Events, yellowCardsEvent)
	}
	return match
}

func extractTeams(ev *LeonEvent) (home, away string) {
	if ev.NameDefault != "" {
		parts := strings.SplitN(ev.NameDefault, " - ", 2)
		if len(parts) == 2 {
			home = strings.TrimSpace(parts[0])
			away = strings.TrimSpace(parts[1])
			if home != "" && away != "" {
				return home, away
			}
		}
	}
	for _, c := range ev.Competitors {
		switch c.HomeAway {
		case "HOME":
			home = strings.TrimSpace(c.Name)
		case "AWAY":
			away = strings.TrimSpace(c.Name)
		}
	}
	if home == "" && away == "" && ev.Name != "" {
		parts := strings.SplitN(ev.Name, " - ", 2)
		if len(parts) == 2 {
			home = strings.TrimSpace(parts[0])
			away = strings.TrimSpace(parts[1])
		}
	}
	return home, away
}

// Берём только основные маркеты: 1X2 (primary), тотал/фора с primary или isMainMarket,
// чтобы не тащить в main_match угловые, тотал хозяев/гостей, азиатские линии и т.д.
func buildMainEvent(matchID string, ev *LeonEvent, now time.Time) models.Event {
	eventID := matchID + "_leon_main_match"
	e := models.Event{
		ID:         eventID,
		MatchID:    matchID,
		EventType:  string(models.StandardEventMainMatch),
		MarketName: models.GetMarketName(models.StandardEventMainMatch),
		Bookmaker:  bookmakerName,
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	for _, m := range ev.Markets {
		if !m.Open {
			continue
		}
		switch m.TypeTag {
		case "REGULAR":
			if is1X2Market(m) {
				for _, r := range m.Runners {
					if !r.Open {
						continue
					}
					ot := leonTagToOutcomeType(r.Tags)
					if ot == "" {
						continue
					}
					e.Outcomes = append(e.Outcomes, newOutcome(eventID, ot, "", r.Price, now))
				}
			}
		case "TOTAL":
			if !isMainTotalMarket(m) {
				continue
			}
			line := m.Handicap
			if line == "" && m.Specifiers != nil {
				line = m.Specifiers["total"]
			}
			for _, r := range m.Runners {
				if !r.Open {
					continue
				}
				ot := ""
				for _, t := range r.Tags {
					if t == "OVER" {
						ot = "total_over"
						break
					}
					if t == "UNDER" {
						ot = "total_under"
						break
					}
				}
				if ot != "" {
					e.Outcomes = append(e.Outcomes, newOutcome(eventID, ot, line, r.Price, now))
				}
			}
		case "HANDICAP":
			if !isMainHandicapMarket(m) {
				continue
			}
			line := m.Handicap
			if line == "" && m.Specifiers != nil {
				line = m.Specifiers["hcp"]
			}
			for _, r := range m.Runners {
				if !r.Open {
					continue
				}
				// Определяем home/away по имени раннера ("1 (-1)" = хозяева, "2 (+1)" = гости), т.к. в API теги иногда перепутаны.
				ot := leonHandicapOutcomeType(r)
				if ot != "" {
					param := line
					if r.Handicap != "" {
						param = r.Handicap
					}
					e.Outcomes = append(e.Outcomes, newOutcome(eventID, ot, param, r.Price, now))
				}
			}
		}
	}
	return e
}

func isMainTotalMarket(m LeonMarket) bool {
	return m.MarketTypeID == mainTotalMarketTypeIDSoccer
}

func isMainHandicapMarket(m LeonMarket) bool {
	return m.MarketTypeID == mainHandicapMarketTypeIDSoccer
}

func is1X2Market(m LeonMarket) bool {
	return m.MarketTypeID == main1X2MarketTypeIDSoccer
}

func leonTagToOutcomeType(tags []string) string {
	for _, t := range tags {
		switch t {
		case "HOME":
			return "home_win"
		case "AWAY":
			return "away_win"
		case "DRAW":
			return "draw"
		}
	}
	return ""
}

// leonHandicapOutcomeType возвращает handicap_home или handicap_away. Приоритет — по имени раннера
// ("1 (-1)", "2 (+1)"), т.к. в API теги HOME/AWAY иногда приходят перепутанными для фор.
func leonHandicapOutcomeType(r LeonRunner) string {
	name := strings.TrimSpace(r.Name)
	if strings.HasPrefix(name, "1 ") || strings.HasPrefix(name, "1(") {
		return "handicap_home"
	}
	if strings.HasPrefix(name, "2 ") || strings.HasPrefix(name, "2(") {
		return "handicap_away"
	}
	for _, t := range r.Tags {
		if t == "HOME" {
			return "handicap_home"
		}
		if t == "AWAY" {
			return "handicap_away"
		}
	}
	return ""
}

func newOutcome(eventID, outcomeType, param string, odds float64, now time.Time) models.Outcome {
	id := fmt.Sprintf("%s_%s_%s", eventID, outcomeType, param)
	return models.Outcome{
		ID:          id,
		EventID:     eventID,
		OutcomeType: outcomeType,
		Parameter:   param,
		Odds:        odds,
		Bookmaker:   bookmakerName,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// CollectLeagueIDs собирает все league ID из ответа sports (только футбол).
func CollectLeagueIDs(sports []SportItem, family string) []int64 {
	if family == "" {
		family = "Soccer"
	}
	var ids []int64
	for _, s := range sports {
		if s.Family != family {
			continue
		}
		for _, r := range s.Regions {
			for _, l := range r.Leagues {
				if l.Prematch > 0 {
					ids = append(ids, l.ID)
				}
			}
		}
	}
	return ids
}

// ParseFloat безопасно парсит строку в float64 (для handicap/total).
func ParseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// buildStatisticalEvent собирает одно событие (corners, fouls, yellow_cards) по allowList marketTypeId.
func buildStatisticalEvent(matchID string, ev *LeonEvent, now time.Time, eventType models.StandardEventType, allowList map[int64]bool) models.Event {
	eventID := matchID + "_leon_" + string(eventType)
	e := models.Event{
		ID:         eventID,
		MatchID:    matchID,
		EventType:  string(eventType),
		MarketName: models.GetMarketName(eventType),
		Bookmaker:  bookmakerName,
		Outcomes:   []models.Outcome{},
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	for _, m := range ev.Markets {
		if !m.Open || !allowList[m.MarketTypeID] {
			continue
		}
		switch m.TypeTag {
		case "REGULAR":
			// «Кто больше» и «точное количество» только по marketTypeId.
			isWhoMore := (eventType == models.StandardEventCorners && m.MarketTypeID == cornersWhoMoreMarketTypeID) ||
				(eventType == models.StandardEventFouls && foulsWhoMoreMarketTypeID != 0 && m.MarketTypeID == foulsWhoMoreMarketTypeID) ||
				(eventType == models.StandardEventYellowCards && m.MarketTypeID == yellowCardsWhoMoreMarketTypeID)
			if isWhoMore {
				for _, r := range m.Runners {
					if !r.Open {
						continue
					}
					ot := leonTagToOutcomeType(r.Tags)
					if ot != "" {
						e.Outcomes = append(e.Outcomes, newOutcome(eventID, ot, "", r.Price, now))
					}
				}
			}
			isExactCount := (eventType == models.StandardEventCorners && m.MarketTypeID == cornersExactCountMarketTypeID) ||
				(eventType == models.StandardEventFouls && foulsMainMarketTypeIDs[m.MarketTypeID]) // фолы: оба ID дают exact_count
			if isExactCount {
				for _, r := range m.Runners {
					if !r.Open {
						continue
					}
					param := strings.TrimSpace(r.Name)
					if param != "" {
						e.Outcomes = append(e.Outcomes, newOutcome(eventID, "exact_count", param, r.Price, now))
					}
				}
			}
		case "TOTAL":
			line := m.Handicap
			if line == "" && m.Specifiers != nil {
				line = m.Specifiers["total"]
			}
			for _, r := range m.Runners {
				if !r.Open {
					continue
				}
				ot := ""
				for _, t := range r.Tags {
					if t == "OVER" {
						ot = "total_over"
						break
					}
					if t == "UNDER" {
						ot = "total_under"
						break
					}
				}
				if ot != "" {
					e.Outcomes = append(e.Outcomes, newOutcome(eventID, ot, line, r.Price, now))
				}
			}
		case "HANDICAP":
			line := m.Handicap
			if line == "" && m.Specifiers != nil {
				line = m.Specifiers["hcp"]
			}
			for _, r := range m.Runners {
				if !r.Open {
					continue
				}
				ot := leonHandicapOutcomeType(r)
				if ot != "" {
					param := line
					if r.Handicap != "" {
						param = r.Handicap
					}
					e.Outcomes = append(e.Outcomes, newOutcome(eventID, ot, param, r.Price, now))
				}
			}
		}
	}
	return e
}
