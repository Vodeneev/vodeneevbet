package fonbet

import (
	"fmt"
	"time"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/line"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

// BuildEsportsLineMatch builds line.Match from Fonbet main event and its factors (for esports: dota2, cs, valorant, lol, kog, crossfire, callofduty).
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
	
	// Group totals by parameter to deduplicate (only one pair per line)
	type totalPair struct {
		overOdds  float64
		underOdds float64
	}
	totalsByParam := make(map[string]*totalPair)
	
	for _, f := range factors {
		switch f.F {
		case 910, 921:
			// F=910 can be handicap if it has parameter; F=921 is always result
			if f.F == 910 && f.Pt != "" {
				// Skip - this is handicap, not result
				continue
			}
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "home_win", Parameter: "", Odds: f.V})
		case 912, 922:
			// F=912 can be handicap if it has parameter; F=922 is always draw
			if f.F == 912 && f.Pt != "" {
				// Skip - this is handicap, not draw
				continue
			}
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "draw", Parameter: "", Odds: f.V})
		case 923:
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "away_win", Parameter: "", Odds: f.V})
		case 930:
			// Total rounds over (standard)
			param := f.Pt
			if param == "" && f.P != 0 {
				param = fmt.Sprintf("%.1f", float64(f.P)/100.0)
			}
			if param != "" && param[0] != '+' && param[0] != '-' {
				if totalsByParam[param] == nil {
					totalsByParam[param] = &totalPair{overOdds: f.V}
				}
			}
		case 931:
			// Total rounds under (standard)
			param := f.Pt
			if param == "" && f.P != 0 {
				param = fmt.Sprintf("%.1f", float64(f.P)/100.0)
			}
			if param != "" && param[0] != '+' && param[0] != '-' {
				if totalsByParam[param] == nil {
					totalsByParam[param] = &totalPair{underOdds: f.V}
				} else {
					totalsByParam[param].underOdds = f.V
				}
			}
		// Total rounds: alternative F IDs (CS esports uses different F for different total lines)
		case 1733: // 46.5 over
			if f.Pt == "46.5" {
				if totalsByParam["46.5"] == nil {
					totalsByParam["46.5"] = &totalPair{overOdds: f.V}
				}
			}
		case 1734: // 46.5 under
			if f.Pt == "46.5" {
				if totalsByParam["46.5"] == nil {
					totalsByParam["46.5"] = &totalPair{underOdds: f.V}
				} else {
					totalsByParam["46.5"].underOdds = f.V
				}
			}
		case 1727: // 47.5 over
			if f.Pt == "47.5" {
				if totalsByParam["47.5"] == nil {
					totalsByParam["47.5"] = &totalPair{overOdds: f.V}
				}
			}
		case 1728: // 47.5 under
			if f.Pt == "47.5" {
				if totalsByParam["47.5"] == nil {
					totalsByParam["47.5"] = &totalPair{underOdds: f.V}
				} else {
					totalsByParam["47.5"].underOdds = f.V
				}
			}
		case 1696: // 49.5 over
			if f.Pt == "49.5" {
				if totalsByParam["49.5"] == nil {
					totalsByParam["49.5"] = &totalPair{overOdds: f.V}
				}
			}
		case 1697: // 49.5 under
			if f.Pt == "49.5" {
				if totalsByParam["49.5"] == nil {
					totalsByParam["49.5"] = &totalPair{underOdds: f.V}
				} else {
					totalsByParam["49.5"].underOdds = f.V
				}
			}
		case 1730: // 50.5 over
			if f.Pt == "50.5" {
				if totalsByParam["50.5"] == nil {
					totalsByParam["50.5"] = &totalPair{overOdds: f.V}
				}
			}
		case 1731: // 50.5 under
			if f.Pt == "50.5" {
				if totalsByParam["50.5"] == nil {
					totalsByParam["50.5"] = &totalPair{underOdds: f.V}
				} else {
					totalsByParam["50.5"].underOdds = f.V
				}
			}
		case 1736: // 51.5 over
			if f.Pt == "51.5" {
				if totalsByParam["51.5"] == nil {
					totalsByParam["51.5"] = &totalPair{overOdds: f.V}
				}
			}
		case 1737: // 51.5 under
			if f.Pt == "51.5" {
				if totalsByParam["51.5"] == nil {
					totalsByParam["51.5"] = &totalPair{underOdds: f.V}
				} else {
					totalsByParam["51.5"].underOdds = f.V
				}
			}
		case 1739: // 52.5 over
			if f.Pt == "52.5" {
				if totalsByParam["52.5"] == nil {
					totalsByParam["52.5"] = &totalPair{overOdds: f.V}
				}
			}
		case 1791: // 52.5 under
			if f.Pt == "52.5" {
				if totalsByParam["52.5"] == nil {
					totalsByParam["52.5"] = &totalPair{underOdds: f.V}
				} else {
					totalsByParam["52.5"].underOdds = f.V
				}
			}
		// Total maps 2.5 (CS esports)
		case 3274: // Total maps 2.5 over
			if f.Pt == "2.5" {
				if totalsByParam["2.5"] == nil {
					totalsByParam["2.5"] = &totalPair{overOdds: f.V}
				}
			}
		case 3275: // Total maps 2.5 under
			if f.Pt == "2.5" {
				if totalsByParam["2.5"] == nil {
					totalsByParam["2.5"] = &totalPair{underOdds: f.V}
				} else {
					totalsByParam["2.5"].underOdds = f.V
				}
			}
		case 927, 928, 989, 991:
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "handicap_home", Parameter: f.Pt, Odds: f.V})
		}
	}
	
	// Add totals from totalsByParam (only pairs with both over and under)
	for param, t := range totalsByParam {
		if t.overOdds > 0 && t.underOdds > 0 {
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "total_over", Parameter: param, Odds: t.overOdds})
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "total_under", Parameter: param, Odds: t.underOdds})
		} else if t.overOdds > 0 {
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "total_over", Parameter: param, Odds: t.overOdds})
		} else if t.underOdds > 0 {
			mainMarket.Outcomes = append(mainMarket.Outcomes, line.Outcome{OutcomeType: "total_under", Parameter: param, Odds: t.underOdds})
		}
	}
	
	if len(mainMarket.Outcomes) == 0 {
		return nil
	}
	return []line.Market{mainMarket}
}
