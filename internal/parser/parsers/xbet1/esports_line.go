package xbet1

import (
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/line"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// BuildLineMatchFromGameDetails builds line.Match from xbet GameDetails (для sport_id=40, киберспорт).
func BuildLineMatchFromGameDetails(game *GameDetails, leagueName, discipline, bookmaker string) *line.Match {
	if game == nil {
		return nil
	}
	homeTeam := game.O1E
	if homeTeam == "" {
		homeTeam = game.O1
	}
	awayTeam := game.O2E
	if awayTeam == "" {
		awayTeam = game.O2
	}
	if homeTeam == "" || awayTeam == "" {
		return nil
	}
	if discipline == "" {
		discipline = "esports"
	}
	if bookmaker == "" {
		bookmaker = "1xbet"
	}
	startTime := time.Unix(game.S, 0).UTC()
	league := leagueName
	if league == "" {
		league = game.LE
	}
	if league == "" {
		league = game.L
	}

	markets := buildMarketsFromGroupEvents(game.GE)
	if len(markets) == 0 {
		return nil
	}

	return &line.Match{
		HomeTeam:  homeTeam,
		AwayTeam:  awayTeam,
		StartTime: startTime,
		Sport:     discipline,
		League:    league,
		Bookmaker: bookmaker,
		Markets:   markets,
	}
}

func buildMarketsFromGroupEvents(ge []GroupEvent) []line.Market {
	var markets []line.Market
	mainEventType := string(models.StandardEventMainMatch)
	mainName := models.GetMarketName(models.StandardEventMainMatch)

	for _, g := range ge {
		var outcomes []line.Outcome
		switch g.G {
		case 1:
			for _, eventArray := range g.E {
				for _, e := range eventArray {
					switch e.T {
					case 1:
						outcomes = append(outcomes, line.Outcome{OutcomeType: "home_win", Parameter: "", Odds: e.C})
					case 2:
						outcomes = append(outcomes, line.Outcome{OutcomeType: "draw", Parameter: "", Odds: e.C})
					case 3:
						outcomes = append(outcomes, line.Outcome{OutcomeType: "away_win", Parameter: "", Odds: e.C})
					}
				}
			}
			if len(outcomes) > 0 {
				markets = append(markets, line.Market{EventType: mainEventType, MarketName: mainName, Outcomes: outcomes})
			}
		case 2:
			for _, eventArray := range g.E {
				for _, e := range eventArray {
					switch e.T {
					case 7:
						outcomes = append(outcomes, line.Outcome{OutcomeType: "handicap_home", Parameter: formatSignedLine(e.P), Odds: e.C})
					case 8:
						outcomes = append(outcomes, line.Outcome{OutcomeType: "handicap_away", Parameter: formatSignedLine(e.P), Odds: e.C})
					}
				}
			}
			if len(outcomes) > 0 {
				markets = append(markets, line.Market{EventType: mainEventType, MarketName: mainName, Outcomes: outcomes})
			}
		case 17:
			for _, eventArray := range g.E {
				for _, e := range eventArray {
					param := formatLine(e.P)
					switch e.T {
					case 9:
						outcomes = append(outcomes, line.Outcome{OutcomeType: "total_over", Parameter: param, Odds: e.C})
					case 10:
						outcomes = append(outcomes, line.Outcome{OutcomeType: "total_under", Parameter: param, Odds: e.C})
					}
				}
			}
			if len(outcomes) > 0 {
				markets = append(markets, line.Market{EventType: mainEventType, MarketName: mainName, Outcomes: outcomes})
			}
		}
	}
	return markets
}

// formatSignedLine and formatLine are in odds_parser.go (same package)
