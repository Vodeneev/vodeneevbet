package parsers

import (
	"context"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

type YDBClient interface {
	StoreOdd(ctx context.Context, odd *models.Odd) error
	GetOddsByMatch(ctx context.Context, matchID string) ([]*models.Odd, error)
	GetAllMatches(ctx context.Context) ([]string, error)
	Close() error
}

type Parser interface {
	Start(ctx context.Context) error
	Stop() error
	GetName() string
}

type BaseParser struct {
	ydbClient YDBClient
	config    *config.Config
	name      string
}

func NewBaseParser(ydbClient YDBClient, config *config.Config, name string) *BaseParser {
	return &BaseParser{
		ydbClient: ydbClient,
		config:    config,
		name:      name,
	}
}

func (p *BaseParser) GetName() string {
	return p.name
}
