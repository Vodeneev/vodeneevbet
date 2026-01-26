package parsers

import (
	"context"
)

type Parser interface {
	Start(ctx context.Context) error
	Stop() error
	GetName() string
	ParseOnce(ctx context.Context) error
}
