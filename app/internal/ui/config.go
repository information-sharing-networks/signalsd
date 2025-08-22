package ui

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
)

// UIConfig holds configuration specific to the UI server
type UIConfig struct {
	Environment  string        `envconfig:"ENVIRONMENT" default:"dev"`
	Host         string        `envconfig:"HOST" default:"0.0.0.0"`
	Port         int           `envconfig:"PORT" default:"3000"`
	LogLevel     zerolog.Level `envconfig:"LOG_LEVEL" default:"debug"`
	ReadTimeout  time.Duration `envconfig:"READ_TIMEOUT" default:"15s"`
	WriteTimeout time.Duration `envconfig:"WRITE_TIMEOUT" default:"15s"`
	IdleTimeout  time.Duration `envconfig:"IDLE_TIMEOUT" default:"60s"`
	APIBaseURL   string        `envconfig:"API_BASE_URL" default:"http://localhost:8080"`
}

// validEnvs defines the allowed environment values
var validEnvs = map[string]bool{
	"dev":     true,
	"test":    true,
	"perf":    true,
	"prod":    true,
	"staging": true,
}

// NewUIConfig loads environment variables and returns a UIConfig struct
func NewUIConfig(logger *zerolog.Logger) (*UIConfig, error) {
	var cfg UIConfig

	// Load environment variables with defaults
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to process environment variables: %w", err)
	}

	if err := validateUIConfig(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	logger.Info().Msgf("UI configuration loaded for environment: %s", cfg.Environment)
	logger.Info().Msgf("UI will connect to API at: %s", cfg.APIBaseURL)

	return &cfg, nil
}

// validateUIConfig checks for required configuration and validates values
func validateUIConfig(cfg *UIConfig) error {
	// Validate environment
	if !validEnvs[cfg.Environment] {
		return fmt.Errorf("invalid environment '%s'. Valid environments: dev, test, perf, staging, prod", cfg.Environment)
	}

	// Validate port range
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", cfg.Port)
	}

	// Validate timeouts
	if cfg.ReadTimeout <= 0 {
		return fmt.Errorf("read timeout must be positive, got %v", cfg.ReadTimeout)
	}
	if cfg.WriteTimeout <= 0 {
		return fmt.Errorf("write timeout must be positive, got %v", cfg.WriteTimeout)
	}
	if cfg.IdleTimeout <= 0 {
		return fmt.Errorf("idle timeout must be positive, got %v", cfg.IdleTimeout)
	}

	// Validate API base URL is not empty
	if cfg.APIBaseURL == "" {
		return fmt.Errorf("API_BASE_URL cannot be empty")
	}

	return nil
}
