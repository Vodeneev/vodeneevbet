package olimp

// API responses are arrays of one object with payload.

// SportsWithCompetitionsResponse is [0].payload from sports-with-categories-with-competitions?vids=1
type SportsWithCompetitionsResponse []struct {
	Payload *struct {
		ID                         string `json:"id"`
		CategoriesWithCompetitions []struct {
			Competitions []struct {
				ID    string            `json:"id"`
				Name  string            `json:"name"`
				Names map[string]string `json:"names"`
			} `json:"competitions"`
		} `json:"categoriesWithCompetitions"`
	} `json:"payload"`
}

// CompetitionsWithEventsResponse is [0].payload from competitions-with-events?vids[]=id:
type CompetitionsWithEventsResponse []struct {
	Payload *struct {
		Competition *struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"competition"`
		Events []OlimpEvent `json:"events"`
	} `json:"payload"`
}

// OlimpEvent is one match with outcomes (from step 2 or step 3).
type OlimpEvent struct {
	ID            string         `json:"id"`
	Team1Name     string         `json:"team1Name"`
	Team2Name     string         `json:"team2Name"`
	StartDateTime int64          `json:"startDateTime"` // unix seconds
	Names         map[string]string `json:"names"`
	Name          string         `json:"name"`
	Outcomes      []OlimpOutcome `json:"outcomes"`
}

// OlimpOutcome is one coefficient: probability = decimal odds, tableType = RESULT/TOTAL/HANDICAP, groupName for market (e.g. "Угловые", "Фолы").
type OlimpOutcome struct {
	ID              string `json:"id"`
	TableType       string `json:"tableType"`
	GroupName       string `json:"groupName"`
	Probability     string `json:"probability"`
	Param           string `json:"param"`
	ShortName       string `json:"shortName"`
	UnprocessedName string `json:"unprocessedName"`
}

// EventLineResponse is [0].payload from events?vids[]=eventId:&main=false (one event with full outcomes).
type EventLineResponse []struct {
	Payload *OlimpEvent `json:"payload"`
}
