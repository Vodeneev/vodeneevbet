package fonbet

import (
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/line"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// BuildEsportsLineMatch builds line.Match from Fonbet main event and its factors (for dota2/cs).
// Used to feed AddEsportsMatch via line.Match.ToEsportsMatch().
func BuildEsportsLineMatch(mainEvent FonbetAPIEvent, mainFactors []FonbetFactor, sport, league, bookmaker string) *line.Match {
	if mainEvent.Team1 == "" || mainEvent.Team2 == "" {
		return nil
	}
	startTime := time.Unix(mainEvent.StartTime, 0).UTC()
	if league == "" {
		league = "Unknown Tournament"
	}
	if bookmaker == "" {
		bookmaker = "fonbet"
	}

	markets := buildEsportsMarketsFromFactors(mainFactors)
	if len(markets) == 0 {
		return nil
	}

	return &line.Match{
		HomeTeam:  mainEvent.Team1,
		AwayTeam:  mainEvent.Team2,
		StartTime: startTime,
		Sport:     sport,
		League:    league,
		Bookmaker: bookmaker,
		Markets:   markets,
	}
}

func buildEsportsMarketsFromFactors(factors []FonbetFactor) []line.Market {
	var mainMarket line.Market
	mainMarket.EventType = string(models.StandardEventMainMatch)
	mainMarket.MarketName = models.GetMarketName(models.StandardEventMainMatch)
	for _, f := range factors {
		switch f.F {
		case 910, 921:
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "home_win", Parameter: "", Odds: f.V})
		case 912, 922:
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "draw", Parameter: "", Odds: f.V})
		case 923:
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "away_win", Parameter: "", Odds: f.V})
		case 930:
			param := f.Pt
			if param == "" && f.P != 0 {
				param = fmt.Sprintf("%.1f", float64(f.P)/100.0)
			}
			if param != "" && param[0] != '+' && param[0] != '-' {
				mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "total_over", Parameter: param, Odds: f.V})
			}
		case 931:
			param := f.Pt
			if param == "" && f.P != 0 {
				param = fmt.Sprintf("%.1f", float64(f.P)/100.0)
			}
			if param != "" && param[0] != '+' && param[0] != '-' {
				mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "total_under", Parameter: param, Odds: f.V})
			}
		case 927, 928, 989, 991:
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "handicap_home", Parameter: f.Pt, Odds: f.V})
		}
	}
	if len(mainMarket.Outcomes) == 0 {
		return nil
	}
	return []line.Market{mainMarket}
}
