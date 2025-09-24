package enums

// Sport represents supported sports types
type Sport string

const (
	Football Sport = "football"
	Basketball Sport = "basketball"
	Tennis Sport = "tennis"
	Hockey Sport = "hockey"
	Volleyball Sport = "volleyball"
	Baseball Sport = "baseball"
)

// SportInfo contains additional information about a sport
type SportInfo struct {
	Name        string
	ScopeMarket string
	Alias       string
}

// GetSportInfo returns sport information including scope market
func (s Sport) GetSportInfo() SportInfo {
	switch s {
	case Football:
		return SportInfo{
			Name:        "Football",
			ScopeMarket: "1600",
			Alias:       "football",
		}
	default:
		return SportInfo{
			Name:        "Unknown",
			ScopeMarket: "1600", // Default to football
			Alias:       "unknown",
		}
	}
}

// IsValid checks if sport is supported
func (s Sport) IsValid() bool {
	switch s {
	case Football, Basketball, Tennis, Hockey, Volleyball, Baseball:
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
	}
}

// ParseSport parses string to Sport enum
func ParseSport(s string) (Sport, bool) {
	sport := Sport(s)
	return sport, sport.IsValid()
}
