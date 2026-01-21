package parsers

import (
	"context"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

type YDBClient interface {
	StoreMatch(ctx context.Context, match *models.Match) error
	GetMatch(ctx context.Context, matchID string) (*models.Match, error)
	GetAllMatches(ctx context.Context) ([]models.Match, error)
	Close() error
}

type Parser interface {
	Start(ctx context.Context) error
	Stop() error
	GetName() string
	ParseOnce(ctx context.Context) error
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
