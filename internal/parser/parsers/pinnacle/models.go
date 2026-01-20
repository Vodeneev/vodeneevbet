package pinnacle

// Minimal Pinnacle guest API models (Arcadia v0.1).

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

