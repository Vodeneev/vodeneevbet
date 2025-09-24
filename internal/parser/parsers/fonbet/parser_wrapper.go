package fonbet

import (
	"context"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/parser/parsers"
)

type ParserWrapper struct {
	*parsers.BaseParser
	parser *Parser
}

func NewParserWrapper(ydbClient parsers.YDBClient, config *config.Config) *ParserWrapper {
	fonbetCoreParser := NewParser(ydbClient, config)

	return &ParserWrapper{
		BaseParser: parsers.NewBaseParser(ydbClient, config, "Fonbet"),
		parser:     fonbetCoreParser,
	}
}

func (p *ParserWrapper) Start(ctx context.Context) error {
	return p.parser.Start(ctx)
}

func (p *ParserWrapper) Stop() error {
	return p.parser.Stop()
}
