package leon

// API models for Leon (leon.ru) betline API.
// Sports: GET /api-2/betline/sports?ctag=ru-RU&flags=urlv2
// Events: GET /api-2/betline/events/all?ctag=ru-RU&league_id=...&hideClosed=true&flags=...
// Event:  GET /api-2/betline/event/all?ctag=ru-RU&eventId=...&flags=...

// SportItem — один вид спорта из /betline/sports (верхний уровень массива).
type SportItem struct {
	ID       int64        `json:"id"`
	Name     string       `json:"name"`
	Weight   int          `json:"weight"`
	Family   string       `json:"family"` // "Soccer"
	Regions  []RegionItem `json:"regions"`
}

// RegionItem — регион/страна внутри спорта.
type RegionItem struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	NameDefault string       `json:"nameDefault"`
	Family      string       `json:"family"`
	URL         string       `json:"url"`
	Leagues     []LeagueItem `json:"leagues"`
}

// LeagueItem — лига (чемпионат).
type LeagueItem struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	NameDefault string `json:"nameDefault"`
	URL      string `json:"url"`
	Weight   int    `json:"weight"`
	Prematch int    `json:"prematch"`
	Inplay   int    `json:"inplay"`
	Outright int    `json:"outright"`
	Top      bool   `json:"top"`
	TopOrder int    `json:"topOrder"`
}

// EventsResponse — ответ /betline/events/all.
type EventsResponse struct {
	Enabled    bool        `json:"enabled"`
	Betline    interface{} `json:"betline"`
	TotalCount int         `json:"totalCount"`
	Events     []LeonEvent `json:"events"`
}

// LeonEvent — матч в списке событий лиги.
type LeonEvent struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	NameDefault  string            `json:"nameDefault"`
	Competitors  []LeonCompetitor  `json:"competitors"`
	Kickoff      int64             `json:"kickoff"` // ms
	LastUpdated  int64             `json:"lastUpdated"`
	League       LeonEventLeague   `json:"league"`
	Betline      string            `json:"betline"`
	Open         bool              `json:"open"`
	Status       string            `json:"status"`
	Markets      []LeonMarket      `json:"markets"`
	MatchPhase   string            `json:"matchPhase"`
}

// LeonCompetitor — участник (команда).
type LeonCompetitor struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	HomeAway   string `json:"homeAway"` // "HOME" | "AWAY"
	LogoSource string `json:"logoSource"`
	Logo       string `json:"logo"`
}

// LeonEventLeague — лига внутри события (может быть только id).
type LeonEventLeague struct {
	ID   int64  `json:"id"`
	Name string `json:"name,omitempty"`
	NameDefault string `json:"nameDefault,omitempty"`
	URL  string `json:"url,omitempty"`
}

// LeonMarket — рынок (исход, тотал, фора).
type LeonMarket struct {
	ID             int64        `json:"id"`
	TypeTag        string       `json:"typeTag"` // "REGULAR" | "TOTAL" | "HANDICAP"
	Name           string       `json:"name"`
	MarketTypeID   int64        `json:"marketTypeId"`
	Open           bool         `json:"open"`
	Primary        bool         `json:"primary"`
	Handicap       string       `json:"handicap,omitempty"`
	Runners        []LeonRunner `json:"runners"`
	Specifiers     map[string]string `json:"specifiers"`
	SelectionTypes []string     `json:"selectionTypes"`
	IsMainMarket   bool         `json:"isMainMarket,omitempty"`
}

// LeonRunner — исход (коэффициент).
type LeonRunner struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Open     bool    `json:"open"`
	Tags     []string `json:"tags"` // "HOME","AWAY","DRAW","OVER","UNDER"
	Price    float64 `json:"price"`
	PriceStr string  `json:"priceStr"`
	Handicap string  `json:"handicap,omitempty"`
}
