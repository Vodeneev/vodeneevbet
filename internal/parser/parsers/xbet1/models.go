package xbet1

// Models for 1xbet API responses

// ChampsResponse represents the response from GetChampsZip
type ChampsResponse struct {
	Error     string      `json:"Error"`
	ErrorCode int         `json:"ErrorCode"`
	Success   bool        `json:"Success"`
	Value     []ChampItem `json:"Value"`
}

// ChampItem represents a single championship/league
type ChampItem struct {
	LI  int64  `json:"LI"`  // League ID
	L   string `json:"L"`   // League name (Russian)
	LE  string `json:"LE"`  // League name (English)
	CI  int    `json:"CI"`  // Country ID
	CID int    `json:"CID"` // Country ID (duplicate?)
	CN  string `json:"CN"`  // Country name (Russian)
	CE  string `json:"CE"`  // Country name (English)
	SI  int    `json:"SI"`  // Sport ID
	SN  string `json:"SN"`  // Sport name
	SE  string `json:"SE"`  // Sport name (English)
	T   int    `json:"T"`   // Type? (1000 = main league)
	// For grouped championships (countries with sub-leagues)
	SC  []ChampItem `json:"SC,omitempty"` // Sub-championships
	GC  int         `json:"GC,omitempty"` // Group count
	CSC int         `json:"CSC,omitempty"` // Count of sub-championships
}

// MatchesResponse represents the response from Get1x2_VZip
type MatchesResponse struct {
	Error     string   `json:"Error"`
	ErrorCode int      `json:"ErrorCode"`
	Success   bool     `json:"Success"`
	Value     []Match  `json:"Value"`
}

// Match represents a single match from Get1x2_VZip
type Match struct {
	I    int64  `json:"I"`    // Match ID
	N    int64  `json:"N"`    // Match number?
	O1   string `json:"O1"`   // Home team name
	O1E  string `json:"O1E"`  // Home team name (English)
	O1I  int64  `json:"O1I"`  // Home team ID
	O2   string `json:"O2"`   // Away team name
	O2E  string `json:"O2E"`  // Away team name (English)
	O2I  int64  `json:"O2I"`  // Away team ID
	S    int64  `json:"S"`    // Start time (Unix timestamp)
	L    string `json:"L"`    // League name (Russian)
	LE   string `json:"LE"`   // League name (English)
	LI   int64  `json:"LI"`   // League ID
	CI   int64  `json:"CI"`   // Country ID
	CID  int    `json:"CID"`  // Country ID
	CN   string `json:"CN"`   // Country name (Russian)
	CE   string `json:"CE"`   // Country name (English)
	COI  int    `json:"COI"`  // Country ID
	E    []Event `json:"E"`   // Events/odds
	AE   []AdditionalEvent `json:"AE,omitempty"` // Additional events (statistical)
	MS   []int  `json:"MS"`   // Match status?
	SS   int    `json:"SS"`   // Sport status?
	SI   int    `json:"SI"`   // Sport ID
	SN   string `json:"SN"`   // Sport name
	SE   string `json:"SE"`   // Sport name (English)
}

// Event represents a single betting event/outcome
type Event struct {
	C   float64 `json:"C"`   // Coefficient (odds)
	CV  string  `json:"CV"`  // Coefficient value (string)
	G   int     `json:"G"`   // Group ID (1=moneyline, 2=handicap, 17=total, etc.)
	T   int     `json:"T"`   // Type ID (1=home, 2=draw, 3=away for moneyline)
	P   float64 `json:"P"`   // Parameter (handicap/total line value)
	CE  int     `json:"CE"`  // Coefficient enabled? (1 = enabled)
}

// AdditionalEvent represents additional/statistical events
type AdditionalEvent struct {
	G   int     `json:"G"`   // Group ID
	ME  []Event `json:"ME"`  // Market events
}

// GameResponse represents the response from GetGameZip
type GameResponse struct {
	Error     string      `json:"Error"`
	ErrorCode int         `json:"ErrorCode"`
	Success   bool        `json:"Success"`
	Value     GameDetails `json:"Value"`
}

// GameDetails represents detailed game information
type GameDetails struct {
	I    int64       `json:"I"`    // Match ID
	N    int64       `json:"N"`    // Match number
	O1   string      `json:"O1"`   // Home team name
	O1E  string      `json:"O1E"`  // Home team name (English)
	O1I  int64       `json:"O1I"`  // Home team ID
	O2   string      `json:"O2"`   // Away team name
	O2E  string      `json:"O2E"`  // Away team name (English)
	O2I  int64       `json:"O2I"`  // Away team ID
	S    int64       `json:"S"`    // Start time (Unix timestamp)
	L    string      `json:"L"`   // League name (Russian)
	LE   string      `json:"LE"`   // League name (English)
	LI   int64       `json:"LI"`   // League ID
	CI   int64       `json:"CI"`   // Country ID
	CID  int         `json:"CID"`  // Country ID
	CN   string      `json:"CN"`   // Country name (Russian)
	CE   string      `json:"CE"`   // Country name (English)
	COI  int         `json:"COI"`  // Country ID
	GE   []GroupEvent `json:"GE"`  // Grouped events
	EC   int         `json:"EC"`   // Event count
	EGC  int         `json:"EGC"`  // Event group count
	MS   []int       `json:"MS"`   // Match status
	SS   int         `json:"SS"`   // Sport status
	SI   int         `json:"SI"`   // Sport ID
	SN   string      `json:"SN"`   // Sport name
	SE   string      `json:"SE"`   // Sport name (English)
}

// GroupEvent represents a group of events (market)
type GroupEvent struct {
	G   int       `json:"G"`   // Group ID
	GS  int       `json:"GS"`  // Group sub-ID
	E   [][]Event `json:"E"`   // Events (array of arrays - different outcomes)
}
