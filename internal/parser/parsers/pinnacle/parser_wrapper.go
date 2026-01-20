package pinnacle

import (
	"context"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
)

type ParserWrapper struct {
	parser *Parser
	name   string
}

func NewParserWrapper(cfg *config.Config) *ParserWrapper {
	return &ParserWrapper{
		parser: NewParser(cfg),
		name:   "Pinnacle",
	}
}

func (p *ParserWrapper) Start(ctx context.Context) error { return p.parser.Start(ctx) }
func (p *ParserWrapper) Stop() error                    { return p.parser.Stop() }
func (p *ParserWrapper) GetName() string                { return p.name }

