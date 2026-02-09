package logging

import (
	"context"
	"encoding/base64"
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
	"github.com/yandex-cloud/go-sdk/iamkey"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// YandexLoggingConfig содержит настройки для отправки логов в Yandex Cloud Logging
type YandexLoggingConfig struct {
	Enabled       bool          `yaml:"enabled"`        // Включить отправку в Cloud Logging
	GroupName     string        `yaml:"group_name"`     // Имя лог-группы (например, "default")
	GroupID       string        `yaml:"group_id"`       // ID лог-группы (альтернатива group_name)
	FolderID      string        `yaml:"folder_id"`      // ID каталога (можно задать через YC_FOLDER_ID env)
	Level         string        `yaml:"level"`          // Минимальный уровень логирования (DEBUG, INFO, WARN, ERROR)
	BatchSize     int           `yaml:"batch_size"`     // Размер батча для отправки (по умолчанию 10)
	FlushInterval time.Duration `yaml:"flush_interval"` // Интервал отправки батча (по умолчанию 5s)
	// Метки для логирования (отображаются в Yandex Cloud Logging)
	ProjectLabel string `yaml:"project_label"` // Название проекта (по умолчанию cloud_id)
	ServiceLabel string `yaml:"service_label"` // Название сервиса (по умолчанию log_group_id)
	ClusterLabel string `yaml:"cluster_label"` // Название кластера/каталога (по умолчанию folder_id)
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
	// Используем Instance Metadata Service для автоматического получения и обновления токенов
	// Это работает только на VM в Yandex Cloud

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

	// Получаем метки из env, если не указаны в конфиге
	// ВАЖНО: Проверяем переменные окружения ПЕРЕД использованием значений по умолчанию
	if config.ProjectLabel == "" {
		if envProjectLabel := os.Getenv("YC_LOG_PROJECT_LABEL"); envProjectLabel != "" {
			config.ProjectLabel = envProjectLabel
		}
	}
	if config.ServiceLabel == "" {
		if envServiceLabel := os.Getenv("YC_LOG_SERVICE_LABEL"); envServiceLabel != "" {
			config.ServiceLabel = envServiceLabel
		}
	}
	if config.ClusterLabel == "" {
		if envClusterLabel := os.Getenv("YC_LOG_CLUSTER_LABEL"); envClusterLabel != "" {
			config.ClusterLabel = envClusterLabel
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
	// Пытаемся использовать разные способы аутентификации в порядке приоритета:
	// 1. Service Account Key файл (через YC_SERVICE_ACCOUNT_KEY_FILE или YC_SERVICE_ACCOUNT_KEY_JSON)
	// 2. Instance Metadata Service (если работает на VM с привязанным service account)
	var creds ycsdk.Credentials

	// Проверяем наличие Service Account Key файла
	saKeyFile := os.Getenv("YC_SERVICE_ACCOUNT_KEY_FILE")
	saKeyJSONB64 := os.Getenv("YC_SERVICE_ACCOUNT_KEY_JSON_B64")
	saKeyJSON := os.Getenv("YC_SERVICE_ACCOUNT_KEY_JSON") // Fallback для обратной совместимости

	if saKeyJSONB64 != "" {
		// Используем Service Account Key из переменной окружения (base64-encoded JSON)
		saKeyJSONBytes, err := base64.StdEncoding.DecodeString(saKeyJSONB64)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 service account key: %w", err)
		}
		// Очищаем декодированный JSON от форматирующих переносов строк и пробелов
		// Это необходимо, так как base64 может содержать форматированный JSON с переносами строк
		// Используем json.Compact для минификации JSON (сохраняет экранированные \n в строках)
		var jsonObj interface{}
		if err := json.Unmarshal(saKeyJSONBytes, &jsonObj); err != nil {
			// Если JSON невалидный, пробуем очистить от форматирующих символов
			cleaned := strings.ReplaceAll(string(saKeyJSONBytes), "\r\n", " ")
			cleaned = strings.ReplaceAll(cleaned, "\n", " ")
			cleaned = strings.ReplaceAll(cleaned, "\r", " ")
			// Убираем множественные пробелы
			for strings.Contains(cleaned, "  ") {
				cleaned = strings.ReplaceAll(cleaned, "  ", " ")
			}
			saKeyJSONBytes = []byte(cleaned)
		} else {
			// JSON валидный, минифицируем его
			saKeyJSONBytes, err = json.Marshal(jsonObj)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal cleaned JSON: %w", err)
			}
		}
		key, err := iamkey.ReadFromJSONBytes(saKeyJSONBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse service account key from YC_SERVICE_ACCOUNT_KEY_JSON_B64: %w", err)
		}
		creds, err = ycsdk.ServiceAccountKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to create credentials from service account key: %w", err)
		}
	} else if saKeyJSON != "" {
		// Используем Service Account Key из переменной окружения (JSON строка) - для обратной совместимости
		// Убираем возможные экранированные кавычки и переносы строк из .env файла
		saKeyJSON = strings.ReplaceAll(saKeyJSON, "\\\"", "\"")
		saKeyJSON = strings.ReplaceAll(saKeyJSON, "\\n", "\n")
		key, err := iamkey.ReadFromJSONBytes([]byte(saKeyJSON))
		if err != nil {
			return nil, fmt.Errorf("failed to parse service account key from YC_SERVICE_ACCOUNT_KEY_JSON: %w", err)
		}
		creds, err = ycsdk.ServiceAccountKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to create credentials from service account key: %w", err)
		}
	} else if saKeyFile != "" {
		// Используем Service Account Key из файла
		key, err := iamkey.ReadFromJSONFile(saKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read service account key from file %s: %w", saKeyFile, err)
		}
		creds, err = ycsdk.ServiceAccountKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to create credentials from service account key: %w", err)
		}
	} else {
		// Используем Instance Metadata Service (работает только на VM с привязанным service account)
		creds = ycsdk.InstanceServiceAccount()
	}

	sdk, err := ycsdk.Build(context.Background(), ycsdk.Config{
		Credentials: creds,
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
		if err != nil {
			// Если не удалось получить список групп, логируем ошибку, но продолжаем
			slog.Default().Warn("Failed to list log groups", "error", err)
		} else {
			// Ищем группу с нужным именем
			found := false
			for _, group := range listResp.Groups {
				if group.Name == config.GroupName {
					groupID = group.Id
					found = true
					slog.Default().Info("Found log group", "name", config.GroupName, "id", groupID)
					break
				}
			}
			if !found {
				availableGroups := make([]string, len(listResp.Groups))
				for i, group := range listResp.Groups {
					availableGroups[i] = group.Name
				}
				slog.Default().Warn("Log group not found", "name", config.GroupName, "folder_id", config.FolderID, "available_groups", availableGroups)
			}
		}
	}

	if groupID != "" {
		destination.Destination = &logging.Destination_LogGroupId{
			LogGroupId: groupID,
		}
		slog.Default().Info("Using log group ID", "id", groupID)
	} else if config.FolderID != "" {
		// Если не удалось найти группу, используем folder_id как fallback
		// Но это может не работать, если в каталоге несколько групп
		destination.Destination = &logging.Destination_FolderId{
			FolderId: config.FolderID,
		}
		slog.Default().Warn("Using folder_id as destination (log group not found by name). This may cause Permission Denied if multiple groups exist.")
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

	// Устанавливаем значения по умолчанию для меток, если не указаны
	// ВАЖНО: Эти значения используются только если метки не были установлены из конфига или env
	if handler.config.ProjectLabel == "" {
		handler.config.ProjectLabel = "vodeneevbet"
	}
	// ServiceLabel не устанавливаем здесь - он должен быть установлен из env или serviceName в setupLoggerWithConfig
	if handler.config.ServiceLabel == "" {
		handler.config.ServiceLabel = "unknown"
	}
	if handler.config.ClusterLabel == "" {
		handler.config.ClusterLabel = "production"
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
		// Логируем ошибку через default logger, чтобы не создавать цикл
		slog.Default().Error("Failed to send logs to Yandex Cloud Logging", "error", err)
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
		// ВАЖНО: Всегда создаем payloadMap, чтобы гарантировать добавление меток
		payloadMap := make(map[string]interface{})
		
		// Копируем существующие поля из payload
		for k, v := range entry.Payload {
			payloadMap[k] = v
		}

		// ВАЖНО: Добавляем метки в JSON payload для фильтрации в Yandex Cloud Logging
		// Метки должны быть добавлены ПЕРЕД созданием structpb.Struct
		// Метки добавляются всегда, даже если они перезаписывают существующие значения
		if h.config.ProjectLabel != "" {
			payloadMap["project_label"] = h.config.ProjectLabel
		}
		if h.config.ServiceLabel != "" {
			payloadMap["service_label"] = h.config.ServiceLabel
		}
		if h.config.ClusterLabel != "" {
			payloadMap["cluster_label"] = h.config.ClusterLabel
		}

		// Создаем structpb.Struct из map (включая метки)
		// Всегда создаем jsonPayload, если есть хотя бы одно поле (включая метки)
		var jsonPayload *structpb.Struct
		if len(payloadMap) > 0 {
			var err error
			jsonPayload, err = structpb.NewStruct(payloadMap)
			if err != nil {
				// Логируем ошибку, но продолжаем работу
				slog.Default().Warn("Failed to create structpb.Struct from payload", "error", err, "payload_size", len(payloadMap), "has_labels", h.config.ServiceLabel != "")
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

	// Добавляем метки через LogEntryDefaults для всех записей в батче
	// Это более эффективно, чем добавлять метки в каждую запись отдельно
	// Используем правильные имена меток для фильтрации в Yandex Cloud Logging
	if h.config.ProjectLabel != "" || h.config.ServiceLabel != "" || h.config.ClusterLabel != "" {
		defaultsPayload := make(map[string]interface{})
		if h.config.ProjectLabel != "" {
			defaultsPayload["project_label"] = h.config.ProjectLabel
		}
		if h.config.ServiceLabel != "" {
			defaultsPayload["service_label"] = h.config.ServiceLabel
		}
		if h.config.ClusterLabel != "" {
			defaultsPayload["cluster_label"] = h.config.ClusterLabel
		}

		if len(defaultsPayload) > 0 {
			defaultsPayloadStruct, err := structpb.NewStruct(defaultsPayload)
			if err == nil {
				req.Defaults = &logging.LogEntryDefaults{
					JsonPayload: defaultsPayloadStruct,
				}
			}
		}
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
