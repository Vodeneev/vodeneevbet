package parsers

import (
	"context"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/storage"
)

// Parser interface for all bookmaker parsers
type Parser interface {
	Start(ctx context.Context) error
	Stop() error
	GetName() string
}

// BaseParser base structure for all parsers
type BaseParser struct {
	ydbClient *storage.YDBWorkingClient
	config    *config.Config
	name      string
}

// NewBaseParser creates base parser
func NewBaseParser(ydbClient *storage.YDBWorkingClient, config *config.Config, name string) *BaseParser {
	return &BaseParser{
		ydbClient: ydbClient,
		config:    config,
		name:      name,
	}
}

// GetName returns parser name
func (p *BaseParser) GetName() string {
	return p.name
}
