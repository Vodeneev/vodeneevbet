package health

import (
	"sync"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/interfaces"
)

// Global parser registry for on-demand parsing
var (
	globalParsers   []interfaces.Parser
	globalParsersMu sync.RWMutex
)

// RegisterParsers registers parsers for on-demand parsing
func RegisterParsers(parsers []interfaces.Parser) {
	globalParsersMu.Lock()
	defer globalParsersMu.Unlock()
	globalParsers = parsers
}

// GetParsers returns a copy of registered parsers (thread-safe)
func GetParsers() []interfaces.Parser {
	globalParsersMu.RLock()
	defer globalParsersMu.RUnlock()
	
	// Return a copy to avoid race conditions
	parsers := make([]interfaces.Parser, len(globalParsers))
	copy(parsers, globalParsers)
	return parsers
}
