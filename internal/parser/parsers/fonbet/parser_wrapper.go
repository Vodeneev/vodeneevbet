package fonbet

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
	parsers.Register("fonbet", func(cfg *config.Config) parsers.Parser {
		return NewParserWrapper(cfg)
	})
}

func NewParserWrapper(config *config.Config) *ParserWrapper {
	fonbetCoreParser := NewParser(config)

	return &ParserWrapper{
		parser: fonbetCoreParser,
		name:   "Fonbet",
	}
}

func (p *ParserWrapper) Start(ctx context.Context) error {
	return p.parser.Start(ctx)
}

func (p *ParserWrapper) Stop() error {
	return p.parser.Stop()
}

func (p *ParserWrapper) GetName() string {
	return p.name
}

func (p *ParserWrapper) ParseOnce(ctx context.Context) error {
	return p.parser.ParseOnce(ctx)
}
