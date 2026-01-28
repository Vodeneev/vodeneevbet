package logging

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/config"
)

// SetupLogger настраивает глобальный logger с поддержкой Yandex Cloud Logging
func SetupLogger(cfg *config.LoggingConfig, serviceName string) (*slog.Logger, error) {
	// Конвертируем config.LoggingConfig в YandexLoggingConfig
	loggingConfig := YandexLoggingConfig{
		Enabled:       cfg.Enabled,
		GroupName:     cfg.GroupName,
		GroupID:       cfg.GroupID,
		FolderID:      cfg.FolderID,
		Level:         cfg.Level,
		BatchSize:     cfg.BatchSize,
		FlushInterval: cfg.FlushInterval,
		ProjectLabel:  cfg.ProjectLabel,
		ServiceLabel:  cfg.ServiceLabel,
		ClusterLabel:  cfg.ClusterLabel,
	}

	// Если ServiceLabel не указан, используем имя сервиса из параметра
	if loggingConfig.ServiceLabel == "" {
		loggingConfig.ServiceLabel = serviceName
	}
	return setupLoggerWithConfig(loggingConfig, serviceName)
}

func setupLoggerWithConfig(config YandexLoggingConfig, serviceName string) (*slog.Logger, error) {
	var handlers []slog.Handler

	// Всегда добавляем handler для stdout/stderr
	textHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	handlers = append(handlers, textHandler)

	// Если включено логирование в Yandex Cloud, добавляем соответствующий handler
	if config.Enabled {
		yandexHandler, err := NewYandexLoggingHandler(config)
		if err != nil {
			log.Printf("Warning: failed to initialize Yandex Cloud Logging: %v", err)
			log.Println("Continuing with stdout logging only")
		} else {
			handlers = append(handlers, yandexHandler)
		}
	}

	// Создаем multi-handler для отправки в несколько мест
	multiHandler := &MultiHandler{
		handlers: handlers,
	}

	logger := slog.New(multiHandler)
	logger = logger.With("service", serviceName)

	// Устанавливаем как глобальный logger
	slog.SetDefault(logger)

	return logger, nil
}

// MultiHandler отправляет логи в несколько handlers
type MultiHandler struct {
	handlers []slog.Handler
}

func (m *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *MultiHandler) Handle(ctx context.Context, record slog.Record) error {
	var lastErr error
	for _, h := range m.handlers {
		if h.Enabled(ctx, record.Level) {
			if err := h.Handle(ctx, record); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}

func (m *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &MultiHandler{handlers: handlers}
}

func (m *MultiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &MultiHandler{handlers: handlers}
}
