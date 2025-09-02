package ui

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// UI server config - used when the ui is run in standalone mode
type Config struct {
	Environment  string        `envconfig:"ENVIRONMENT" default:"dev"`
	Host         string        `envconfig:"HOST" default:"0.0.0.0"`
	Port         int           `envconfig:"PORT" default:"3000"`
	LogLevel     string        `envconfig:"LOG_LEVEL" default:"debug"`
	ReadTimeout  time.Duration `envconfig:"READ_TIMEOUT" default:"15s"`
	WriteTimeout time.Duration `envconfig:"WRITE_TIMEOUT" default:"15s"`
	IdleTimeout  time.Duration `envconfig:"IDLE_TIMEOUT" default:"60s"`
	APIBaseURL   string        `envconfig:"API_BASE_URL" default:"http://localhost:8080"`
}

var validEnvs = map[string]bool{
	"dev":     true,
	"test":    true,
	"perf":    true,
	"prod":    true,
	"staging": true,
}

const accessTokenCookieName = "access_token"
const isnPermsCookieName = "isn_perms"
const accountInfoCookieName = "account_info"

func NewConfig() (*Config, error) {
	var cfg Config

	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to process environment variables: %w", err)
	}

	if err := validateUIConfig(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
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
