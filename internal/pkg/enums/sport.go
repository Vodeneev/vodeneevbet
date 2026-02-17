package enums

// Sport represents supported sports types
type Sport string

const (
	Football   Sport = "football"
	Basketball Sport = "basketball"
	Tennis     Sport = "tennis"
	Hockey     Sport = "hockey"
	Volleyball Sport = "volleyball"
	Baseball   Sport = "baseball"
	// Киберспорт (Fonbet: sportCategoryId 19 = Dota2, 20 = CS, 21 = Valorant, 22 = LOL, 78 = KOG, 148 = CrossFire, 169 = CallOfDuty; xbet: sports=40)
	Dota2      Sport = "dota2"
	CS         Sport = "cs"
	Valorant   Sport = "valorant"
	LOL        Sport = "lol"
	KOG        Sport = "kog"
	CrossFire  Sport = "crossfire"
	CallOfDuty Sport = "callofduty"
)

// SportInfo contains additional information about a sport
type SportInfo struct {
	Name  string
	Alias string
}

// GetSportInfo returns sport information
func (s Sport) GetSportInfo() SportInfo {
	switch s {
	case Football:
		return SportInfo{
			Name:  "Football",
			Alias: "football",
		}
	case Basketball:
		return SportInfo{
			Name:  "Basketball",
			Alias: "basketball",
		}
	case Tennis:
		return SportInfo{
			Name:  "Tennis",
			Alias: "tennis",
		}
	case Hockey:
		return SportInfo{
			Name:  "Hockey",
			Alias: "hockey",
		}
	case Volleyball:
		return SportInfo{
			Name:  "Volleyball",
			Alias: "volleyball",
		}
	case Baseball:
		return SportInfo{
			Name:  "Baseball",
			Alias: "baseball",
		}
	case Dota2:
		return SportInfo{
			Name:  "Dota 2",
			Alias: "dota2",
		}
	case CS:
		return SportInfo{
			Name:  "Counter-Strike",
			Alias: "cs",
		}
	case Valorant:
		return SportInfo{
			Name:  "Valorant",
			Alias: "valorant",
		}
	case LOL:
		return SportInfo{
			Name:  "League of Legends",
			Alias: "lol",
		}
	case KOG:
		return SportInfo{
			Name:  "King of Glory",
			Alias: "kog",
		}
	case CrossFire:
		return SportInfo{
			Name:  "CrossFire",
			Alias: "crossfire",
		}
	case CallOfDuty:
		return SportInfo{
			Name:  "Call of Duty",
			Alias: "callofduty",
		}
	default:
		return SportInfo{
			Name:  "Unknown",
			Alias: "unknown",
		}
	}
}

// IsValid checks if sport is supported
func (s Sport) IsValid() bool {
	switch s {
	case Football, Basketball, Tennis, Hockey, Volleyball, Baseball, Dota2, CS, Valorant, LOL, KOG, CrossFire, CallOfDuty:
		return true
	default:
		return false
	}
}

// String returns string representation
func (s Sport) String() string {
	return string(s)
}

// GetAllSports returns all supported sports
func GetAllSports() []Sport {
	return []Sport{
		Football,
		Basketball,
		Tennis,
		Hockey,
		Volleyball,
		Baseball,
		Dota2,
		CS,
		Valorant,
		LOL,
		KOG,
		CrossFire,
		CallOfDuty,
	}
}

// ParseSport parses string to Sport enum
func ParseSport(s string) (Sport, bool) {
	sport := Sport(s)
	return sport, sport.IsValid()
}
