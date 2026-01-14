package config

import (
	"fmt"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
	"os"
)

type Config struct {
	YDB            YDBConfig            `yaml:"ydb"`
	Postgres       PostgresConfig       `yaml:"postgres"`
	Parser         ParserConfig         `yaml:"parser"`
	ValueCalculator ValueCalculatorConfig `yaml:"value_calculator"`
}

type YDBConfig struct {
	Endpoint              string        `yaml:"endpoint"`
	Database              string        `yaml:"database"`
	ServiceAccountKeyFile string        `yaml:"service_account_key_file"`
	TTL                   TTLConfig     `yaml:"ttl"`
}

type TTLConfig struct {
	Enabled       bool          `yaml:"enabled"`
	ExpireAfter   time.Duration `yaml:"expire_after"`
	AutoSetup     bool          `yaml:"auto_setup"`
}


type PostgresConfig struct {
	DSN string `yaml:"dsn"`
}

type ParserConfig struct {
	Type      string            `yaml:"type"`
	Interval  time.Duration     `yaml:"interval"`
	UserAgent string            `yaml:"user_agent"`
	Timeout   time.Duration     `yaml:"timeout"`
	Headers   map[string]string `yaml:"headers"`
	Fonbet    FonbetConfig      `yaml:"fonbet"`
}

type FonbetConfig struct {
	BaseURL   string `yaml:"base_url"`
	Lang      string `yaml:"lang"`
	Version   string `yaml:"version"`
	TestLimit int    `yaml:"test_limit"`
}

type ValueCalculatorConfig struct {
	ReferenceMethod      string    `yaml:"reference_method"`
	MinValuePercent      float64   `yaml:"min_value_percent"`
	MaxRiskPercent       float64   `yaml:"max_risk_percent"`
	MinStake             int       `yaml:"min_stake"`
	MaxStake             int       `yaml:"max_stake"`
	CheckInterval        string    `yaml:"check_interval"`
	TestInterval         string    `yaml:"test_interval"`
	Sports               []string  `yaml:"sports"`
	Markets              []string  `yaml:"markets"`
	ReferenceBookmakers  []string  `yaml:"reference_bookmakers"`
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

	// Resolve relative paths inside config against the config file directory,
	// not against the current working directory.
	if config.YDB.ServiceAccountKeyFile != "" && !filepath.IsAbs(config.YDB.ServiceAccountKeyFile) {
		baseDir := filepath.Dir(configPath)
		config.YDB.ServiceAccountKeyFile = filepath.Clean(filepath.Join(baseDir, config.YDB.ServiceAccountKeyFile))
	}

	return &config, nil
}
