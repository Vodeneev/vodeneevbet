package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// YandexLoggingConfig содержит настройки для отправки логов в Yandex Cloud Logging
type YandexLoggingConfig struct {
	Enabled       bool          `yaml:"enabled"`        // Включить отправку в Cloud Logging
	GroupName     string        `yaml:"group_name"`     // Имя лог-группы (например, "default")
	GroupID       string        `yaml:"group_id"`       // ID лог-группы (альтернатива group_name)
	IAMToken      string        `yaml:"iam_token"`      // IAM токен (можно задать через YC_IAM_TOKEN env)
	FolderID      string        `yaml:"folder_id"`      // ID каталога (можно задать через YC_FOLDER_ID env)
	Level         string        `yaml:"level"`          // Минимальный уровень логирования (DEBUG, INFO, WARN, ERROR)
	BatchSize     int           `yaml:"batch_size"`     // Размер батча для отправки (по умолчанию 10)
	FlushInterval time.Duration `yaml:"flush_interval"` // Интервал отправки батча (по умолчанию 5s)
}

// YandexLoggingHandler реализует slog.Handler для отправки логов в Yandex Cloud Logging
type YandexLoggingHandler struct {
	config      YandexLoggingConfig
	client      *http.Client
	buffer      []LogEntry
	bufferMutex sync.Mutex
	ticker      *time.Ticker
	done        chan struct{}
	wg          sync.WaitGroup
	level       slog.Level
}

// LogEntry представляет одну запись лога для отправки в Cloud Logging
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
}

// NewYandexLoggingHandler создает новый handler для Yandex Cloud Logging
func NewYandexLoggingHandler(config YandexLoggingConfig) (*YandexLoggingHandler, error) {
	// Получаем IAM токен из env, если не указан в конфиге
	if config.IAMToken == "" {
		config.IAMToken = os.Getenv("YC_IAM_TOKEN")
	}
	if config.IAMToken == "" {
		return nil, fmt.Errorf("IAM token is required (set YC_IAM_TOKEN env var or in config)")
	}

	// Получаем folder_id из env, если не указан в конфиге
	if config.FolderID == "" {
		config.FolderID = os.Getenv("YC_FOLDER_ID")
	}

	// Получаем group_name из env, если не указан в конфиге
	if config.GroupName == "" {
		if envGroupName := os.Getenv("YC_LOG_GROUP_NAME"); envGroupName != "" {
			config.GroupName = envGroupName
		}
	}

	// Получаем group_id из env, если не указан в конфиге
	if config.GroupID == "" {
		if envGroupID := os.Getenv("YC_LOG_GROUP_ID"); envGroupID != "" {
			config.GroupID = envGroupID
		}
	}

	// Устанавливаем значения по умолчанию
	if config.BatchSize <= 0 {
		config.BatchSize = 10
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 5 * time.Second
	}

	// Парсим уровень логирования
	var level slog.Level
	switch config.Level {
	case "DEBUG":
		level = slog.LevelDebug
	case "INFO":
		level = slog.LevelInfo
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := &YandexLoggingHandler{
		config: config,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		buffer: make([]LogEntry, 0, config.BatchSize),
		ticker: time.NewTicker(config.FlushInterval),
		done:   make(chan struct{}),
		level:  level,
	}

	// Запускаем горутину для периодической отправки батчей
	handler.wg.Add(1)
	go handler.flushLoop()

	return handler, nil
}

// Enabled проверяет, должен ли быть залогирован запрос с данным уровнем
func (h *YandexLoggingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

// Handle обрабатывает запись лога
func (h *YandexLoggingHandler) Handle(ctx context.Context, record slog.Record) error {
	if !h.Enabled(ctx, record.Level) {
		return nil
	}

	entry := LogEntry{
		Timestamp: record.Time,
		Level:     record.Level.String(),
		Message:   record.Message,
		Payload:   make(map[string]interface{}),
	}

	// Извлекаем атрибуты из записи
	record.Attrs(func(a slog.Attr) bool {
		entry.Payload[a.Key] = a.Value.Any()
		return true
	})

	h.bufferMutex.Lock()
	h.buffer = append(h.buffer, entry)
	shouldFlush := len(h.buffer) >= h.config.BatchSize
	h.bufferMutex.Unlock()

	if shouldFlush {
		go h.flush()
	}

	return nil
}

// WithAttrs возвращает новый handler с дополнительными атрибутами
func (h *YandexLoggingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Для простоты возвращаем тот же handler
	// В реальной реализации можно создать обертку
	return h
}

// WithGroup возвращает новый handler с группой атрибутов
func (h *YandexLoggingHandler) WithGroup(name string) slog.Handler {
	// Для простоты возвращаем тот же handler
	return h
}

// flushLoop периодически отправляет накопленные логи
func (h *YandexLoggingHandler) flushLoop() {
	defer h.wg.Done()
	for {
		select {
		case <-h.ticker.C:
			h.flush()
		case <-h.done:
			return
		}
	}
}

// flush отправляет накопленные логи в Cloud Logging
func (h *YandexLoggingHandler) flush() {
	h.bufferMutex.Lock()
	if len(h.buffer) == 0 {
		h.bufferMutex.Unlock()
		return
	}

	entries := make([]LogEntry, len(h.buffer))
	copy(entries, h.buffer)
	h.buffer = h.buffer[:0]
	h.bufferMutex.Unlock()

	if err := h.sendLogs(entries); err != nil {
		// Логируем ошибку в stderr, чтобы не создавать цикл
		fmt.Fprintf(os.Stderr, "Failed to send logs to Yandex Cloud Logging: %v\n", err)
	}
}

// sendLogs отправляет логи в Yandex Cloud Logging через REST API
// Использует формат API, совместимый с командой yc logging write
// Формат: параметры передаются через form-data (application/x-www-form-urlencoded)
func (h *YandexLoggingHandler) sendLogs(entries []LogEntry) error {
	// Формируем URL для API
	apiURL := "https://ingester.logging.yandexcloud.net/write"

	// Отправляем каждый лог отдельно
	for _, entry := range entries {
		// Формируем URL с параметрами группы и каталога в query string
		reqURL, err := url.Parse(apiURL)
		if err != nil {
			continue
		}
		
		q := reqURL.Query()
		if h.config.GroupID != "" {
			q.Set("groupId", h.config.GroupID)
		} else if h.config.GroupName != "" {
			q.Set("groupName", h.config.GroupName)
		} else {
			q.Set("groupName", "default")
		}
		if h.config.FolderID != "" {
			q.Set("folderId", h.config.FolderID)
		}
		reqURL.RawQuery = q.Encode()

		// Формируем form data (точно как в команде yc logging write и curl)
		formData := url.Values{}
		formData.Set("message", entry.Message)
		formData.Set("level", entry.Level)
		
		// Добавляем JSON payload если есть
		if len(entry.Payload) > 0 {
			jsonPayloadBytes, err := json.Marshal(entry.Payload)
			if err == nil {
				formData.Set("json_payload", string(jsonPayloadBytes))
			}
		}

		// Создаем запрос с form data в теле (точно как в curl)
		reqBody := formData.Encode()
		req, err := http.NewRequest("POST", reqURL.String(), bytes.NewBufferString(reqBody))
		if err != nil {
			continue
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+h.config.IAMToken)

		resp, err := h.client.Do(req)
		if err != nil {
			// Продолжаем отправку остальных логов даже при ошибке
			continue
		}

		// Читаем тело ответа для диагностики
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			// Логируем ошибку с подробностями для диагностики
			fmt.Fprintf(os.Stderr, "Yandex Cloud Logging error: status %d, body: %s, url: %s\n", 
				resp.StatusCode, string(body), reqURL.String())
		}
	}

	return nil
}

// Close закрывает handler и отправляет оставшиеся логи
func (h *YandexLoggingHandler) Close() error {
	close(h.done)
	h.ticker.Stop()
	h.wg.Wait()
	h.flush()
	return nil
}
