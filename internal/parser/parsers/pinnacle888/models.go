package pinnacle888

// Minimal Pinnacle guest API models (Arcadia v0.1).
// Same as pinnacle parser models.

type Sport struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	MatchupCount int    `json:"matchupCount"`
	IsHidden     bool   `json:"isHidden"`
}

type RelatedMatchup struct {
	ID        int64  `json:"id"`
	ParentID  *int64 `json:"parentId,omitempty"`
	StartTime string `json:"startTime"` // RFC3339
	Type      string `json:"type"`      // "matchup"
	Units     string `json:"units"`

	League struct {
		Name  string `json:"name"`
		Sport struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"sport"`
	} `json:"league"`

	Participants []Participant `json:"participants"`
}

type Participant struct {
	Alignment string `json:"alignment"` // "home" | "away"
	Name      string `json:"name"`
}

type Market struct {
	MatchupID   int64   `json:"matchupId"`
	Period      int     `json:"period"` // 0=full game, 1=1st half, etc.
	Type        string  `json:"type"`   // moneyline | spread | total | team_total
	Key         string  `json:"key"`
	IsAlternate bool    `json:"isAlternate"`
	Status      string  `json:"status"`
	Prices      []Price `json:"prices"`
}

type Price struct {
	Designation string   `json:"designation"` // home/away/draw OR over/under
	Points      *float64 `json:"points,omitempty"`
	Price       int      `json:"price"` // American odds
}

// LeagueListItem is a league from /sports-service/sv/euro/leagues
type LeagueListItem struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	TotalEvents   int    `json:"totalEvents"`
	FeaturesOrder int    `json:"featuresOrder"`
	EnglishName   string `json:"englishName"`
	LeagueCode    string `json:"leagueCode"`
}

// EventOddsResponse is the response from /sports-service/sv/euro/odds/event
type EventOddsResponse struct {
	Info     EventOddsInfo `json:"info"`
	Normal   Event         `json:"normal"`
	Corners  *Event        `json:"corners,omitempty"`  // Statistical event for corners
	Bookings *Event        `json:"bookings,omitempty"` // Statistical event for bookings (yellow cards)
}

type EventOddsInfo struct {
	SportID       int64  `json:"sportId"`
	SportName     string `json:"sportName"`
	LeagueCode    string `json:"leagueCode"`
	LeagueName    string `json:"leagueName"`
	ResultingUnit string `json:"resultingUnit"`
	LeagueID      int64  `json:"leagueId"`
	Container     string `json:"container"`
	SeriesID      int64  `json:"seriesId"`
}

// Models for new odds endpoint (sports-service/sv/euro/odds)
type OddsResponse struct {
	SportID int64    `json:"sportId"`
	Version int64    `json:"version"`
	Leagues []League `json:"leagues"`
}

type League struct {
	SportID    int64   `json:"sportId"`
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	LeagueCode string  `json:"leagueCode"`
	Container  string  `json:"container"`
	Events     []Event `json:"events"`
}

type Event struct {
	ID                int64                 `json:"id"`
	ParentID          int64                 `json:"parentId"`
	Time              int64                 `json:"time"` // Unix timestamp in milliseconds
	Participants      []EventParticipant    `json:"participants"`
	Periods           map[string]PeriodData `json:"periods"`
	HomeTeamType      int                   `json:"homeTeamType"`
	AwayTeamType      int                   `json:"awayTeamType"`
	ParlayRestriction int                   `json:"parlayRestriction"`
	RotNum            string                `json:"rotNum"`
	ResultingUnit     string                `json:"resultingUnit"`
	HasLiveStream     bool                  `json:"hasLiveStream"`
	HasScoreboard     bool                  `json:"hasScoreboard"`
	Live              bool                  `json:"live"`
}

type EventParticipant struct {
	Name        string `json:"name"`
	EnglishName string `json:"englishName"`
	Type        string `json:"type"` // "HOME" or "AWAY"
	Fav         bool   `json:"fav"`
}

type PeriodData struct {
	Handicap         []HandicapMarket `json:"handicap"`
	OverUnder        []TotalMarket    `json:"overUnder"`
	MoneyLine        MoneyLineMarket  `json:"moneyLine"`
	IndexMainLineHdp int              `json:"indexMainLineHdp"`
	IndexMainLineOU  int              `json:"indexMainLineOU"`
}

type HandicapMarket struct {
	LineID      int64  `json:"lineId"`
	IsAlt       bool   `json:"isAlt"`
	HomeSpread  string `json:"homeSpread"`
	AwaySpread  string `json:"awaySpread"`
	HomeOdds    string `json:"homeOdds"`
	AwayOdds    string `json:"awayOdds"`
	Offline     bool   `json:"offline"`
	Unavailable bool   `json:"unavailable"`
}

type TotalMarket struct {
	LineID      int64  `json:"lineId"`
	IsAlt       bool   `json:"isAlt"`
	OverOdds    string `json:"overOdds"`
	UnderOdds   string `json:"underOdds"`
	Points      string `json:"points"`
	Offline     bool   `json:"offline"`
	Unavailable bool   `json:"unavailable"`
}

type MoneyLineMarket struct {
	LineID      int64  `json:"lineId"`
	HomePrice   string `json:"homePrice"`
	AwayPrice   string `json:"awayPrice"`
	DrawPrice   string `json:"drawPrice"`
	Offline     bool   `json:"offline"`
	Unavailable bool   `json:"unavailable"`
}
