package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yandex-cloud/go-genproto/yandex/cloud/logging/v1"
	ycsdk "github.com/yandex-cloud/go-sdk"
	logingestion "github.com/yandex-cloud/go-sdk/gen/logingestion"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	sdk         *ycsdk.SDK
	client      *logingestion.LogIngestionServiceClient
	buffer      []LogEntry
	bufferMutex sync.Mutex
	ticker      *time.Ticker
	done        chan struct{}
	wg          sync.WaitGroup
	level       slog.Level
	destination *logging.Destination
	groupName   string // Сохраняем имя группы для использования в запросах
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

	// Инициализируем Yandex Cloud SDK
	sdk, err := ycsdk.Build(context.Background(), ycsdk.Config{
		Credentials: ycsdk.NewIAMTokenCredentials(config.IAMToken),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Yandex Cloud SDK: %w", err)
	}

	// Получаем клиент для отправки логов через SDK
	// SDK знает правильный endpoint для LogIngestionService
	logIngestion := sdk.LogIngestion()
	client := logIngestion.LogIngestion()

	// Определяем destination (лог-группу)
	// Если указан group_id, используем его напрямую
	// Если указано только group_name, нужно найти group_id через LogGroupService
	destination := &logging.Destination{}
	groupID := config.GroupID

	ctx := context.Background()
	if groupID == "" && config.GroupName != "" && config.FolderID != "" {
		// Пытаемся найти группу по имени через LogGroupService
		logGroupClient := sdk.Logging().LogGroup()
		listReq := &logging.ListLogGroupsRequest{
			FolderId: config.FolderID,
		}
		listResp, err := logGroupClient.List(ctx, listReq)
		if err == nil {
			// Ищем группу с нужным именем
			for _, group := range listResp.Groups {
				if group.Name == config.GroupName {
					groupID = group.Id
					break
				}
			}
		}
	}

	if groupID != "" {
		destination.Destination = &logging.Destination_LogGroupId{
			LogGroupId: groupID,
		}
	} else if config.FolderID != "" {
		// Если не удалось найти группу, используем folder_id как fallback
		destination.Destination = &logging.Destination_FolderId{
			FolderId: config.FolderID,
		}
	} else {
		return nil, fmt.Errorf("either group_id, group_name with folder_id, or folder_id must be specified")
	}

	// Определяем имя группы для использования в запросах
	groupName := config.GroupName
	if groupName == "" {
		groupName = "default"
	}

	handler := &YandexLoggingHandler{
		config:      config,
		sdk:         sdk,
		client:      client,
		buffer:      make([]LogEntry, 0, config.BatchSize),
		ticker:      time.NewTicker(config.FlushInterval),
		done:        make(chan struct{}),
		level:       level,
		destination: destination,
		groupName:   groupName,
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

// sendLogs отправляет логи в Yandex Cloud Logging через gRPC API
func (h *YandexLoggingHandler) sendLogs(entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Преобразуем записи логов в формат API
	logEntries := make([]*logging.IncomingLogEntry, 0, len(entries))
	for _, entry := range entries {
		// Преобразуем timestamp
		timestamp := timestamppb.New(entry.Timestamp)

		// Преобразуем уровень логирования
		level := logging.LogLevel_LEVEL_UNSPECIFIED
		switch strings.ToUpper(entry.Level) {
		case "DEBUG":
			level = logging.LogLevel_DEBUG
		case "INFO":
			level = logging.LogLevel_INFO
		case "WARN":
			level = logging.LogLevel_WARN
		case "ERROR":
			level = logging.LogLevel_ERROR
		default:
			level = logging.LogLevel_INFO
		}

		// Преобразуем payload в structpb.Struct
		var jsonPayload *structpb.Struct
		if len(entry.Payload) > 0 {
			jsonBytes, err := json.Marshal(entry.Payload)
			if err == nil {
				var jsonMap map[string]interface{}
				if err := json.Unmarshal(jsonBytes, &jsonMap); err == nil {
					jsonPayload, _ = structpb.NewStruct(jsonMap)
				}
			}
		}

		// Создаем запись лога
		logEntry := &logging.IncomingLogEntry{
			Timestamp: timestamp,
			Level:     level,
			Message:   entry.Message,
		}

		// Добавляем JSON payload если есть
		if jsonPayload != nil {
			logEntry.JsonPayload = jsonPayload
		}

		logEntries = append(logEntries, logEntry)
	}

	// Отправляем логи батчем
	// Если указано имя группы, нужно найти её ID или использовать folder_id
	req := &logging.WriteRequest{
		Destination: h.destination,
		Entries:     logEntries,
	}

	// Если указано имя группы, но не указан ID, пытаемся найти группу по имени
	// Для этого нужно использовать LogGroupService, но пока используем folder_id
	// и надеемся, что API сможет найти группу по имени внутри каталога

	_, err := h.client.Write(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to write logs: %w", err)
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
