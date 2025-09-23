package config

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
	"os"
)

type Config struct {
	YDB        YDBConfig        `yaml:"ydb"`
	Postgres   PostgresConfig   `yaml:"postgres"`
	Parser     ParserConfig     `yaml:"parser"`
	Calculator CalculatorConfig `yaml:"calculator"`
}

type YDBConfig struct {
	Endpoint              string `yaml:"endpoint"`
	Database              string `yaml:"database"`
	ServiceAccountKeyFile string `yaml:"service_account_key_file"`
}


type PostgresConfig struct {
	DSN string `yaml:"dsn"`
}

type ParserConfig struct {
	Interval  time.Duration `yaml:"interval"`
	UserAgent string        `yaml:"user_agent"`
	Timeout   time.Duration `yaml:"timeout"`
}

type CalculatorConfig struct {
	MinProfitPercent float64 `yaml:"min_profit_percent"`
	MaxCombinations  int     `yaml:"max_combinations"`
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
