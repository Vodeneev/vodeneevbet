package line

// Mapping documents how each bookmaker API maps to the unified line model (Match, Market, Outcome).
// Actual conversion stays in internal/parser/parsers/{fonbet,xbet1}/; new bookmakers can
// produce line.Match and call ToModelsMatch().
//
// ## Fonbet (events/list)
//
// Match: events[].team1/team2 → HomeTeam/AwayTeam; startTime (unix) → StartTime;
// sportId → Sport via sports[].sportCategoryId (19=dota2, 20=cs) or alias.
//
// Markets: customFactors[].e=eventId, factors[]={f,v,p,pt}. f=910/912/921→1X2;
// 927/928→handicap; 930/931→total. kind/rootKind→market (1=main, 100201=corners, etc.).
//
// ## xbet (GetGameZip, Get1x2_VZip)
//
// Match: O1E/O2E → HomeTeam/AwayTeam; S (unix) → StartTime; SI (1=football, 40=esports) → Sport; LE → League.
//
// Markets: GE[].G=group. G=1→main_match (T=1/2/3 → home_win/draw/away_win);
// G=2→handicap (T=7/8, P=line); G=17→total (T=9/10, P=line);
// G=100/101/102/103/105 + SG[].TG → corners/yellow_cards/fouls/offsides.
