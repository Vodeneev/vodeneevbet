package fonbet

import "time"

// FonbetEvent represents a sports event from Fonbet
type FonbetEvent struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	HomeTeam   string    `json:"home_team"`
	AwayTeam   string    `json:"away_team"`
	StartTime  time.Time `json:"start_time"`
	Category   string    `json:"category"`
	Tournament string    `json:"tournament"`
	Kind       int64     `json:"kind"`
	RootKind   int64     `json:"root_kind"`
	Level      int       `json:"level"`
	ParentID   int64     `json:"parent_id"`
}

// FonbetAPIResponse represents the response structure from Fonbet API
type FonbetAPIResponse struct {
	PacketVersion                int64           `json:"packetVersion"`
	FromVersion                  int64           `json:"fromVersion"`
	CatalogTablesVersion         int64           `json:"catalogTablesVersion"`
	CatalogSpecialTablesVersion  int64           `json:"catalogSpecialTablesVersion"`
	CatalogEventViewVersion      int64           `json:"catalogEventViewVersion"`
	SportBasicMarketsVersion     int64           `json:"sportBasicMarketsVersion"`
	SportBasicFactorsVersion     int64           `json:"sportBasicFactorsVersion"`
	IndependentFactorsVersion    int64           `json:"independentFactorsVersion"`
	FactorsVersion               int64           `json:"factorsVersion"`
	ComboFactorsVersion          int64           `json:"comboFactorsVersion"`
	SportKindsVersion            int64           `json:"sportKindsVersion"`
	TopCompetitionsVersion       int64           `json:"topCompetitionsVersion"`
	EventSmartFiltersVersion     int64           `json:"eventSmartFiltersVersion"`
	GeoCategoriesVersion         int64           `json:"geoCategoriesVersion"`
	SportCategoriesVersion       int64           `json:"sportCategoriesVersion"`
	PublicPromos                 []interface{}     `json:"publicPromos"`
	TournamentInfos              []FonbetTournament `json:"tournamentInfos"`
	Sports                       []FonbetSport      `json:"sports"`
	Events                       []FonbetAPIEvent   `json:"events"`
	CustomFactors                []FonbetFactorGroup `json:"customFactors"`
}

// FonbetTournament represents a tournament from Fonbet API
type FonbetTournament struct {
	ID               int    `json:"id"`
	BasicSportID     int    `json:"basicSportId,omitempty"`
	Caption          string `json:"caption"`
	BackgroundColor  int    `json:"backgroundColor,omitempty"`
	Icon             string `json:"icon,omitempty"`
	TabCaption       string `json:"tabCaption,omitempty"`
}

// FonbetSport represents a sport from Fonbet API
type FonbetSport struct {
	ID       int    `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Alias    string `json:"alias"`
}

// FonbetAPIEvent represents an event from Fonbet API
type FonbetAPIEvent struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	StartTime int64  `json:"startTime"`
	SportID   int64  `json:"sportId"`
	Kind      int64  `json:"kind"`
	RootKind  int64  `json:"rootKind"`
	Level     int    `json:"level"`
	ParentID  int64  `json:"parentId,omitempty"`
	Team1ID   int64  `json:"team1Id,omitempty"`
	Team2ID   int64  `json:"team2Id,omitempty"`
	Team1     string `json:"team1,omitempty"`
	Team2     string `json:"team2,omitempty"`
}

// FonbetFactorGroup represents a group of factors for an event
type FonbetFactorGroup struct {
	EventID   int64          `json:"e"`
	CountAll  int            `json:"countAll"`
	Factors   []FonbetFactor `json:"factors"`
}

// FonbetFactor represents a betting factor from Fonbet API
type FonbetFactor struct {
	F  int     `json:"f"`  // Factor ID
	V  float64 `json:"v"`  // Value (coefficient)
	P  int     `json:"p"`  // Parameter
	Pt string  `json:"pt"` // Parameter text
}

