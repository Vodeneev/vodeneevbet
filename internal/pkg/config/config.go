package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Postgres        PostgresConfig        `yaml:"postgres"`
	Parser          ParserConfig          `yaml:"parser"`
	ValueCalculator ValueCalculatorConfig `yaml:"value_calculator"`
	Health          HealthConfig          `yaml:"health"`
	Logging         LoggingConfig         `yaml:"logging"`
}

type PostgresConfig struct {
	DSN string `yaml:"dsn"`
}

type ParserConfig struct {
	EnabledParsers    []string          `yaml:"enabled_parsers"`
	Interval          time.Duration     `yaml:"interval"`
	UserAgent         string            `yaml:"user_agent"`
	Timeout           time.Duration     `yaml:"timeout"`
	Headers           map[string]string `yaml:"headers"`
	// BookmakerServices: name -> base URL. If set, parser runs in orchestrator mode:
	// no local parsers, /matches aggregates from these URLs, /parse proxies to them.
	BookmakerServices map[string]string `yaml:"bookmaker_services"`
	// IncrementalParsing enables continuous incremental parsing for bookmaker services
	// When enabled, parsers work in background, parsing data in batches and updating storage incrementally
	// This allows /matches endpoint to return partially ready data without blocking
	IncrementalParsing IncrementalParsingConfig `yaml:"incremental_parsing"`
	Fonbet            FonbetConfig      `yaml:"fonbet"`
	Pinnacle          PinnacleConfig    `yaml:"pinnacle"`
	Pinnacle888       Pinnacle888Config `yaml:"pinnacle888"`
	Marathonbet       MarathonbetConfig `yaml:"marathonbet"`
	Xbet1             Xbet1Config       `yaml:"xbet1"`
}

// MarathonbetConfig configures Marathonbet HTML parser (all-events → leagues → event pages).
type MarathonbetConfig struct {
	BaseURL   string        `yaml:"base_url"`   // e.g. "https://www.marathonbet.ru"
	SportID   int           `yaml:"sport_id"`   // Football = 11 (default)
	Timeout   time.Duration `yaml:"timeout"`    // HTTP timeout (default: 30s)
	UserAgent string        `yaml:"user_agent"` // Override from Parser.UserAgent if empty
	ProxyList []string      `yaml:"proxy_list"` // List of proxies to try in order
}

// IncrementalParsingConfig configures incremental parsing for each parser
type IncrementalParsingConfig struct {
	// Enabled enables incremental parsing mode (default: false)
	Enabled bool `yaml:"enabled"`
	// Timeout is the maximum time allowed for one parsing cycle (default: 0 = no timeout)
	// If 0 or not set, parsing cycle will process all leagues without time limit
	// Parsers will parse continuously without pauses between batches
	Timeout time.Duration `yaml:"timeout"`
}

type FonbetConfig struct {
	BaseURL string `yaml:"base_url"`
	Lang    string `yaml:"lang"`
	Version string `yaml:"version"`
}

type PinnacleConfig struct {
	BaseURL    string   `yaml:"base_url"`
	APIKey     string   `yaml:"api_key"`
	DeviceUUID string   `yaml:"device_uuid"`
	MatchupIDs []int64  `yaml:"matchup_ids"`
	ProxyList  []string `yaml:"proxy_list"` // List of proxies to try in order
}

type Pinnacle888Config struct {
	BaseURL         string   `yaml:"base_url"`
	MirrorURL       string   `yaml:"mirror_url"` // Mirror URL to resolve actual baseURL
	OddsURL         string   `yaml:"odds_url"`   // Path for odds endpoint (e.g., "/sports-service/sv/euro/odds"), domain resolved from mirror_url
	APIKey          string   `yaml:"api_key"`
	DeviceUUID      string   `yaml:"device_uuid"`
	MatchupIDs      []int64  `yaml:"matchup_ids"`
	ProxyList       []string `yaml:"proxy_list"`       // List of proxies to try in order
	IncludePrematch bool     `yaml:"include_prematch"` // Include pre-match/line matches (default: false)
	LeagueWorkers   int      `yaml:"league_workers"`   // Max concurrent leagues (default: 5); events within a league are processed sequentially
	// Authentication headers for logged-in user
	Cookies         string `yaml:"cookies"`          // Cookie header value for authenticated requests
	XAppData        string `yaml:"x_app_data"`      // x-app-data header
	XCustID         string `yaml:"x_custid"`         // x-custid header
	UseAuthHeaders  bool   `yaml:"use_auth_headers"` // Enable authenticated headers for odds requests (default: false)
}

type Xbet1Config struct {
	BaseURL         string   `yaml:"base_url"`
	MirrorURL       string   `yaml:"mirror_url"` // Mirror URL to resolve actual baseURL (e.g., "https://1xbet-skwu.top/link")
	ProxyList       []string `yaml:"proxy_list"` // List of proxies to try in order
	IncludePrematch bool     `yaml:"include_prematch"` // Include pre-match matches (default: true)
	SportID         int      `yaml:"sport_id"`   // Sport ID (1 = Football, default: 1)
	CountryID      int      `yaml:"country_id"` // Country ID (1 = all countries, default: 1)
	VirtualSports  bool     `yaml:"virtual_sports"` // Include virtual sports (default: true)
}

type ValueCalculatorConfig struct {
	MinValuePercent  float64            `yaml:"min_value_percent"` // Minimum value percent for value bets (default: 5.0)
	Sports           []string           `yaml:"sports"`            // Sports to parse (used by parsers)
	BookmakerWeights map[string]float64 `yaml:"bookmaker_weights"` // Optional: weights for reference bookmakers (default: 1.0 for all)
	ParserURL        string             `yaml:"parser_url"`        // URL to parser's /matches endpoint

	// Async processing settings
	AsyncEnabled         bool    `yaml:"async_enabled"`          // Enable async processing
	AsyncInterval        string  `yaml:"async_interval"`         // Interval for async processing (e.g., "30s")
	AlertThreshold       float64 `yaml:"alert_threshold"`        // Single alert threshold in percent (preferred)
	AlertThreshold10     float64 `yaml:"alert_threshold_10"`     // Alert threshold for 10% diffs (backward compatibility)
	AlertThreshold20     float64 `yaml:"alert_threshold_20"`     // Alert threshold for 20% diffs (backward compatibility)
	AlertCooldownMinutes int     `yaml:"alert_cooldown_minutes"` // Minutes to wait before sending duplicate alerts for same diff (default: 60)
	AlertMinIncrease     float64 `yaml:"alert_min_increase"`     // Minimum diff_percent increase to send alert again (default: 5.0)
	TelegramBotToken     string  `yaml:"telegram_bot_token"`     // Telegram bot token for notifications
	TelegramChatID       int64   `yaml:"telegram_chat_id"`       // Telegram chat ID to send notifications
}

type HealthConfig struct {
	ReadHeaderTimeout   time.Duration `yaml:"read_header_timeout"`   // HTTP server read header timeout (default: 5s)
	Port                int           `yaml:"port"`                  // HTTP server listen port (default: 8080)
	AsyncParsingTimeout time.Duration `yaml:"async_parsing_timeout"` // Timeout for async parsing triggered by /matches endpoint (default: 10s)
}

type LoggingConfig struct {
	Enabled       bool          `yaml:"enabled"`        // Включить отправку в Yandex Cloud Logging
	GroupName     string        `yaml:"group_name"`     // Имя лог-группы (например, "default")
	GroupID       string        `yaml:"group_id"`       // ID лог-группы (альтернатива group_name)
	FolderID      string        `yaml:"folder_id"`      // ID каталога (можно задать через YC_FOLDER_ID env)
	Level         string        `yaml:"level"`          // Минимальный уровень логирования (DEBUG, INFO, WARN, ERROR)
	BatchSize     int           `yaml:"batch_size"`     // Размер батча для отправки (по умолчанию 10)
	FlushInterval time.Duration `yaml:"flush_interval"` // Интервал отправки батча (по умолчанию 5s)
	// Метки для логирования (отображаются в Yandex Cloud Logging)
	ProjectLabel string `yaml:"project_label"` // Название проекта (по умолчанию "vodeneevbet")
	ServiceLabel string `yaml:"service_label"` // Название сервиса (по умолчанию имя сервиса из кода)
	ClusterLabel string `yaml:"cluster_label"` // Название кластера/каталога (по умолчанию "production")
}

func Load(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}
