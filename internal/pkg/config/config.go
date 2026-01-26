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
}

type PostgresConfig struct {
	DSN string `yaml:"dsn"`
}

type ParserConfig struct {
	EnabledParsers []string          `yaml:"enabled_parsers"`
	Interval       time.Duration     `yaml:"interval"`
	UserAgent      string            `yaml:"user_agent"`
	Timeout        time.Duration     `yaml:"timeout"`
	Headers        map[string]string `yaml:"headers"`
	Fonbet         FonbetConfig      `yaml:"fonbet"`
	Pinnacle       PinnacleConfig    `yaml:"pinnacle"`
	Pinnacle888    Pinnacle888Config `yaml:"pinnacle888"`
}

type FonbetConfig struct {
	BaseURL   string `yaml:"base_url"`
	Lang      string `yaml:"lang"`
	Version   string `yaml:"version"`
	TestLimit int    `yaml:"test_limit"`
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
	MirrorURL       string   `yaml:"mirror_url"`        // Mirror URL to resolve actual baseURL
	LiveEventsURL   string   `yaml:"live_events_url"`   // URL for live events (compact format)
	LineEventsURL   string   `yaml:"line_events_url"`   // URL for pre-match/line events (compact format)
	APIKey          string   `yaml:"api_key"`
	DeviceUUID      string   `yaml:"device_uuid"`
	MatchupIDs      []int64  `yaml:"matchup_ids"`
	ProxyList       []string `yaml:"proxy_list"`       // List of proxies to try in order
	IncludeLive     bool     `yaml:"include_live"`      // Include live matches (default: false)
	IncludePrematch bool     `yaml:"include_prematch"`  // Include pre-match/line matches (default: false)
}

type ValueCalculatorConfig struct {
	ReferenceMethod     string            `yaml:"reference_method"`      // "diff" or "value_bet"
	MinValuePercent     float64           `yaml:"min_value_percent"`    // Minimum value percent for value bets (default: 5.0)
	MaxRiskPercent      float64           `yaml:"max_risk_percent"`
	MinStake            int               `yaml:"min_stake"`
	MaxStake            int               `yaml:"max_stake"`
	CheckInterval       string            `yaml:"check_interval"`
	TestInterval        string            `yaml:"test_interval"`
	Sports              []string          `yaml:"sports"`
	Markets             []string          `yaml:"markets"`
	BookmakerWeights    map[string]float64 `yaml:"bookmaker_weights"`    // Optional: weights for reference bookmakers (default: 1.0 for all)
	ParserURL           string            `yaml:"parser_url"`            // URL to parser's /matches endpoint
	
	// Async processing settings
	AsyncEnabled        bool    `yaml:"async_enabled"`        // Enable async processing
	AsyncInterval       string  `yaml:"async_interval"`        // Interval for async processing (e.g., "30s")
	AlertThreshold      float64 `yaml:"alert_threshold"`       // Single alert threshold in percent (preferred)
	AlertThreshold10    float64 `yaml:"alert_threshold_10"`   // Alert threshold for 10% diffs
	AlertThreshold20    float64 `yaml:"alert_threshold_20"`   // Alert threshold for 20% diffs
	AlertCooldownMinutes int    `yaml:"alert_cooldown_minutes"` // Minutes to wait before sending duplicate alerts for same diff (default: 60)
	AlertMinIncrease    float64 `yaml:"alert_min_increase"`   // Minimum diff_percent increase to send alert again (default: 5.0)
	TelegramBotToken    string  `yaml:"telegram_bot_token"`  // Telegram bot token for notifications
	TelegramChatID      int64   `yaml:"telegram_chat_id"`    // Telegram chat ID to send notifications
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
