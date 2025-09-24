package fonbet

import "github.com/Vodeneev/vodeneevbet/internal/pkg/enums"

type ScopeMarket string

const (
	Football   ScopeMarket = "1600"
)

func GetScopeMarket(sport enums.Sport) ScopeMarket {
	switch sport {
	case enums.Football:
		return Football

	default:
		return Football
	}
}

func (sm ScopeMarket) String() string {
	return string(sm)
}
