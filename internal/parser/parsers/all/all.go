// Package all imports all available parsers for side-effect registration.
//
// Import this package from your main to ensure all parsers are registered:
//   import _ "github.com/Vodeneev/vodeneevbet/internal/parser/parsers/all"
package all

import (
	_ "github.com/Vodeneev/vodeneevbet/internal/parser/parsers/fonbet"
	_ "github.com/Vodeneev/vodeneevbet/internal/parser/parsers/pinnacle"
)

