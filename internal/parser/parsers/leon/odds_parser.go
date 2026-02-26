package leon

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

const bookmakerName = "Leon"

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
	if cornersEvent := buildStatisticalEvent(matchID, ev, now, models.StandardEventCorners, isCornersMarket); len(cornersEvent.Outcomes) > 0 {
		match.Events = append(match.Events, cornersEvent)
	}
	if foulsEvent := buildStatisticalEvent(matchID, ev, now, models.StandardEventFouls, isFoulsMarket); len(foulsEvent.Outcomes) > 0 {
		match.Events = append(match.Events, foulsEvent)
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
	lower := strings.ToLower(m.Name)
	// Сначала исключаем тоталы не по голам матча: таймы, тотал хозяев/гостей, карточки, комбо.
	if strings.Contains(lower, "1-й тайм") || strings.Contains(lower, "2-й тайм") {
		return false
	}
	if strings.Contains(lower, "хозяев") || strings.Contains(lower, "гостей") {
		return false
	}
	if strings.Contains(lower, "карточ") || strings.Contains(lower, "углов") || strings.Contains(lower, "фол") {
		return false
	}
	if strings.Contains(lower, "победитель и тотал") || strings.Contains(lower, "двойной исход и тотал") || strings.Contains(lower, "в каждом тайме") {
		return false
	}
	// Только простой "Тотал" (голы матча). isMainMarket у "Тотал хозяев" бывает true — не полагаемся.
	if m.IsMainMarket && lower == "тотал" {
		return true
	}
	if m.Primary && lower == "тотал" {
		return true
	}
	return lower == "тотал"
}

func isMainHandicapMarket(m LeonMarket) bool {
	if m.IsMainMarket {
		return true
	}
	if m.Primary {
		return true
	}
	lower := strings.ToLower(m.Name)
	if strings.Contains(lower, "тайм") || strings.Contains(lower, "хозяев") || strings.Contains(lower, "гостей") {
		return false
	}
	return lower == "фора" || strings.HasPrefix(lower, "фора ")
}

func is1X2Market(m LeonMarket) bool {
	// "Исход 1Х2 (основное время)" или marketTypeId основного исхода
	lower := strings.ToLower(m.Name)
	return strings.Contains(lower, "исход") && (strings.Contains(lower, "1х2") || strings.Contains(lower, "1x2"))
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

// marketFilter возвращает true, если маркет относится к нужному статистическому типу (угловые/фолы).
type marketFilter func(LeonMarket) bool

func isCornersMarket(m LeonMarket) bool {
	lower := strings.ToLower(m.Name)
	return strings.Contains(lower, "углов") || strings.Contains(lower, "corner")
}

func isFoulsMarket(m LeonMarket) bool {
	lower := strings.ToLower(m.Name)
	return strings.Contains(lower, "фол") || strings.Contains(lower, "foul")
}

// isMainWhoMoreMarket — только основной маркет "кто больше" (угловых/фолов).
// Исключаем "кто первым подаст N угловых" (другие коэфы) и "двойной исход".
func isMainWhoMoreMarket(m LeonMarket, eventType models.StandardEventType) bool {
	lower := strings.ToLower(m.Name)
	if strings.Contains(lower, "первым") || strings.Contains(lower, "первый") || strings.Contains(lower, "двойной") {
		return false
	}
	switch eventType {
	case models.StandardEventCorners:
		return strings.Contains(lower, "больше") && (strings.Contains(lower, "углов") || strings.Contains(lower, "corner"))
	case models.StandardEventFouls:
		return strings.Contains(lower, "больше") && (strings.Contains(lower, "фол") || strings.Contains(lower, "foul"))
	default:
		return false
	}
}

// buildStatisticalEvent собирает одно событие (corners или fouls) из всех подходящих маркетов.
func buildStatisticalEvent(matchID string, ev *LeonEvent, now time.Time, eventType models.StandardEventType, filter marketFilter) models.Event {
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
		if !m.Open || !filter(m) {
			continue
		}
		// Исключаем маркеты 1-го/2-го тайма для единообразия (основной матч)
		lower := strings.ToLower(m.Name)
		if strings.Contains(lower, "1-й тайм") || strings.Contains(lower, "2-й тайм") {
			continue
		}
		switch m.TypeTag {
		case "REGULAR":
			// Только основной маркет "кто больше" даёт home_win/away_win/draw, иначе путаем с "кто первым N" (другие коэфы).
			if isMainWhoMoreMarket(m, eventType) {
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
			// "Количество фолов" / "Количество угловых" по команде (14+, 15+ ...) -> exact_count
			if strings.Contains(lower, "количество") {
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
