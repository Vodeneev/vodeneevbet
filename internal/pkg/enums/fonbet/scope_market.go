package fonbet

import "github.com/Vodeneev/vodeneevbet/internal/pkg/enums"

type ScopeMarket string

const (
	Football   ScopeMarket = "1600"
	// Киберспорт — тот же scopeMarket, фильтрация по sportCategoryId в ответе (19=Dota2, 20=CS)
	Esports ScopeMarket = "1600"
)

func GetScopeMarket(sport enums.Sport) ScopeMarket {
	switch sport {
	case enums.Football:
		return Football
	case enums.Dota2, enums.CS:
		return Esports
	default:
		return Football
	}
}

func (sm ScopeMarket) String() string {
	return string(sm)
}
