package config

import (
	"fmt"
	"time"

	"github.com/Netflix/go-env"
)

// UI server config - used when the ui is run in standalone mode
type Config struct {
	Environment  string        `env:"ENVIRONMENT,default=dev"`
	Host         string        `env:"HOST,default=0.0.0.0"`
	Port         int           `env:"PORT,default=3000"`
	LogLevel     string        `env:"LOG_LEVEL,default=debug"`
	ReadTimeout  time.Duration `env:"READ_TIMEOUT,default=15s"`
	WriteTimeout time.Duration `env:"WRITE_TIMEOUT,default=15s"`
	IdleTimeout  time.Duration `env:"IDLE_TIMEOUT,default=60s"`
	APIBaseURL   string        `env:"API_BASE_URL,default=http://localhost:8080"`
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

	_, err := env.UnmarshalFromEnviron(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal environment variables: %w", err)
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
