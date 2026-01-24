package pinnacle888

import (
	"context"

	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
)

type ParserWrapper struct {
	parser *Parser
	name   string
}

func init() {
	parsers.Register("pinnacle888", func(cfg *config.Config) parsers.Parser {
		return NewParserWrapper(cfg)
	})
}

func NewParserWrapper(cfg *config.Config) *ParserWrapper {
	return &ParserWrapper{
		parser: NewParser(cfg),
		name:   "Pinnacle888",
	}
}

func (p *ParserWrapper) Start(ctx context.Context) error { return p.parser.Start(ctx) }
func (p *ParserWrapper) Stop() error                    { return p.parser.Stop() }
func (p *ParserWrapper) GetName() string                { return p.name }
func (p *ParserWrapper) ParseOnce(ctx context.Context) error { return p.parser.ParseOnce(ctx) }
