package fonbet

import (
	"context"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
)

type ParserWrapper struct {
	parser *Parser
	name   string
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
