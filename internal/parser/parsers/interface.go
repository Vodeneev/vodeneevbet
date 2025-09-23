package parsers

import (
	"context"
	"vodeneevbet/internal/pkg/config"
	"vodeneevbet/internal/pkg/storage"
)

// Parser интерфейс для всех парсеров букмекеров
type Parser interface {
	Start(ctx context.Context) error
	Stop() error
	GetName() string
}

// BaseParser базовая структура для всех парсеров
type BaseParser struct {
	ydbClient *storage.YDBWorkingClient
	config    *config.Config
	name      string
}

// NewBaseParser создает базовый парсер
func NewBaseParser(ydbClient *storage.YDBWorkingClient, config *config.Config, name string) *BaseParser {
	return &BaseParser{
		ydbClient: ydbClient,
		config:    config,
		name:      name,
	}
}

// GetName возвращает имя парсера
func (p *BaseParser) GetName() string {
	return p.name
}
