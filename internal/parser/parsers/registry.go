package parsers

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
)

// Factory constructs a parser instance from config.
type Factory func(cfg *config.Config) Parser

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register registers a parser factory under a name (e.g. "fonbet").
// Names are normalized to lower-case and trimmed.
func Register(name string, f Factory) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		panic("parsers: empty name in Register")
	}
	if f == nil {
		panic("parsers: nil factory in Register for " + n)
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[n]; exists {
		panic("parsers: duplicate registration for " + n)
	}
	registry[n] = f
}

// FactoryByName returns a registered factory by name.
func FactoryByName(name string) (Factory, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	registryMu.RLock()
	defer registryMu.RUnlock()
	f, ok := registry[n]
	return f, ok
}

// Available returns a copy of the registry.
func Available() map[string]Factory {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make(map[string]Factory, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

// AvailableNames returns sorted registered parser names.
func AvailableNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MustFactoryByName returns factory or panics with helpful error.
func MustFactoryByName(name string) Factory {
	if f, ok := FactoryByName(name); ok {
		return f
	}
	return func(*config.Config) Parser {
		panic(fmt.Sprintf("parsers: unknown parser %q (available: %v)", name, AvailableNames()))
	}
}

