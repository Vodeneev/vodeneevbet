package zenit

// LineResponse is the root response from /ajax/line/printer/react
type LineResponse struct {
	Sport      []SportItem           `json:"sport"`
	League     map[string]LeagueItem `json:"league"`
	Games      map[string]Game       `json:"games"`
	Dict       Dict                  `json:"dict"`
	Pagination map[string]int        `json:"pagination"`
	Sort       map[string]interface{} `json:"sort"`
	TB         map[string]TBBlock    `json:"t_b"`
	Filter     interface{}           `json:"filter"`
}

type SportItem struct {
	ID               int     `json:"id"`
	Count            int     `json:"count"`
	TopLeague        []int   `json:"top_league"`
	TournamentRegion []interface{} `json:"tournament_region"`
	Tournament       []interface{} `json:"tournament"`
	TournamentInfo   []interface{} `json:"tournament_info"`
	League           []interface{} `json:"league"`
}

type LeagueItem struct {
	ID         int   `json:"id"`
	Sid        int   `json:"sid"`
	Rid        int   `json:"rid"`
	Tid        int   `json:"tid"`
	TiID       *int  `json:"ti_id"`
	Count      int   `json:"count"`
	Popular    string `json:"popular"`
	GamesTime  int64  `json:"games_time"`
	Games      []int  `json:"games"`
}

type Game struct {
	ID          int        `json:"id"`
	Sid         int        `json:"sid"`
	Lid         int        `json:"lid"`
	Rid         int        `json:"rid"`
	Tid         int        `json:"tid"`
	TiID        *int       `json:"ti_id"`
	Time        int64      `json:"time"`
	Date        string     `json:"date"`
	C1ID        int        `json:"c1_id"`
	C2ID        int        `json:"c2_id"`
	Prio        *string    `json:"prio"`
	Number      string     `json:"number"`
	Stats       int        `json:"stats"`
	FL          []FLItem   `json:"f_l"` // main line odds
	HD          []HDItem   `json:"hd"`  // headers for f_l groups
	Bets        []interface{} `json:"bets"`
	Color       string     `json:"color"`
	Description []interface{} `json:"description"`
}

type FLItem struct {
	ClsVis string      `json:"cls_vis"`
	Sfs    bool        `json:"sfs"`
	Fbt    bool        `json:"fbt"`
	H      interface{} `json:"h"` // float64 or string "0"
	ID     string      `json:"id"`
	OddKey string      `json:"oddKey"`
	O      string      `json:"o"`
	T      string      `json:"t"`
	St     int         `json:"st"`
}

type HDItem struct {
	N      string `json:"n"`
	ClsVis string `json:"cls_vis"`
	Sfs    bool   `json:"sfs"`
	Fbt    bool   `json:"fbt"`
}

type Dict struct {
	Sport           map[string]string            `json:"sport"`
	League          map[string]string            `json:"league"`
	TournamentRegion map[string]string           `json:"tournament_region"`
	Tournament      map[string]string            `json:"tournament"`
	Cmd             map[string]string            `json:"cmd"`
	Eng             *DictEng                     `json:"eng"`
}

type DictEng struct {
	Sport           map[string]string `json:"sport"`
	League          map[string]string `json:"league"`
	TournamentRegion map[string]string `json:"tournament_region"`
	Tournament      map[string]string `json:"tournament"`
	Cmd             map[string]string `json:"cmd"`
}

// TBBlock is the t_b[gameID] block with extended markets
type TBBlock struct {
	Filter TBFilter `json:"filter"`
	Data   TBData   `json:"data"`
}

type TBFilter struct {
	Data  map[string]TBFilterCategory `json:"data"`
	Order []int                       `json:"order"`
}

type TBFilterCategory struct {
	Gid        int   `json:"gid"`
	ID         int   `json:"id"`
	N          string `json:"n"`
	CategoryID string `json:"categoryID"`
	Count      int   `json:"count"`
	Tids       []int `json:"tids"`
}

type TBData struct {
	Data map[string]TBTidBlock `json:"data"`
}

// TBTidBlock is t_b[gid].data.data[tid] - one market block with nested "ch" (children)
type TBTidBlock struct {
	Ch   []TBChNode `json:"ch"`
	Data *TBTidMeta `json:"data"`
}

type TBTidMeta struct {
	CID          string   `json:"c_id"`
	CategoryIDs  []string `json:"categoryIDs"`
	ID           int     `json:"id"`
	Count        int     `json:"count"`
	ClsVis       string  `json:"cls_vis"`
	Sfs          bool    `json:"sfs"`
	Fbt          bool    `json:"fbt"`
	TableID      string  `json:"tableID"`
}

// TBChNode is a recursive node in t_b tree (has "ch" children and/or "h", "id", "oddKey" for odds)
type TBChNode struct {
	Header int        `json:"header"`
	Ch     []TBChNode `json:"ch"`
	W      int        `json:"w"`
	H      interface{} `json:"h"` // string label or float64 odds
	ID     string     `json:"id"`
	OddKey string     `json:"oddKey"`
	O      string     `json:"o"`
	T      string     `json:"t"`
	St     int        `json:"st"`
	Align  string     `json:"align"`
	Cls    int        `json:"cls"`
}
