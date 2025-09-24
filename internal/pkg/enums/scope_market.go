package enums

type ScopeMarket string

const (
	ScopeFootball   ScopeMarket = "1600"
	ScopeBasketball ScopeMarket = "1800"
	ScopeTennis     ScopeMarket = "1200"
	ScopeHockey     ScopeMarket = "1700"
	ScopeVolleyball ScopeMarket = "1900"
	ScopeBaseball   ScopeMarket = "2000"
)

// GetScopeMarket returns scope market for sport
func GetScopeMarket(sport Sport) ScopeMarket {
	switch sport {
	case Football:
		return ScopeFootball
	case Basketball:
		return ScopeBasketball
	case Tennis:
		return ScopeTennis
	case Hockey:
		return ScopeHockey
	case Volleyball:
		return ScopeVolleyball
	case Baseball:
		return ScopeBaseball
	default:
		return ScopeFootball // Default to football
	}
}

// String returns string representation
func (sm ScopeMarket) String() string {
	return string(sm)
}
