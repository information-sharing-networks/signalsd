package config

import (
	"fmt"
	"time"

	env "github.com/caarlos0/env/v11"
)

// UI server config - used when the ui is run in standalone mode
type Config struct {
	Environment  string        `env:"ENVIRONMENT" envDefault:"dev"`
	Host         string        `env:"HOST"         envDefault:"0.0.0.0"`
	Port         int           `env:"PORT"         envDefault:"3000"`
	LogLevel     string        `env:"LOG_LEVEL"    envDefault:"debug"`
	ReadTimeout  time.Duration `env:"READ_TIMEOUT"  envDefault:"15s"`
	WriteTimeout time.Duration `env:"WRITE_TIMEOUT" envDefault:"15s"`
	IdleTimeout  time.Duration `env:"IDLE_TIMEOUT"  envDefault:"60s"`
	APIBaseURL   string        `env:"API_BASE_URL"  envDefault:"http://localhost:8080"`
}

var validEnvs = map[string]bool{
	"dev":     true,
	"test":    true,
	"perf":    true,
	"prod":    true,
	"staging": true,
}

const (
	AccessTokenDetailsCookieName = "access_details_token"
	RefreshTokenCookieName       = "refresh_token"
)

func NewConfig() (*Config, error) {
	var cfg Config

	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %v", err)
	}

	if err := validateUIConfig(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %v", err)
	}

	return &cfg, nil
}

func validateUIConfig(cfg *Config) error {
	if !validEnvs[cfg.Environment] {
		return fmt.Errorf("invalid environment '%s'. Valid environments: dev, test, perf, staging, prod", cfg.Environment)
	}

	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", cfg.Port)
	}

	if cfg.ReadTimeout <= 0 {
		return fmt.Errorf("read timeout must be positive, got %v", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout <= 0 {
		return fmt.Errorf("write timeout must be positive, got %v", cfg.WriteTimeout)
	}
	if cfg.IdleTimeout <= 0 {
		return fmt.Errorf("idle timeout must be positive, got %v", cfg.IdleTimeout)
	}

	if cfg.APIBaseURL == "" {
		return fmt.Errorf("API_BASE_URL cannot be empty")
	}

	return nil
}
