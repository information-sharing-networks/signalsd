package signalsd

import (
	"fmt"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/rs/zerolog"
)

// Environment variables are automatically mapped using envconfig.
type ServerConfig struct {
	Environment          string        `envconfig:"ENVIRONMENT" default:"dev"`
	Host                 string        `envconfig:"HOST" default:"0.0.0.0"`
	Port                 int           `envconfig:"PORT" default:"8080"`
	SecretKey            string        `envconfig:"SECRET_KEY" required:"true"`
	LogLevel             zerolog.Level `envconfig:"LOG_LEVEL" default:"debug"`
	DatabaseURL          string        `envconfig:"DATABASE_URL" required:"true"`
	ReadTimeout          time.Duration `envconfig:"READ_TIMEOUT" default:"15s"`
	WriteTimeout         time.Duration `envconfig:"WRITE_TIMEOUT" default:"15s"`
	IdleTimeout          time.Duration `envconfig:"IDLE_TIMEOUT" default:"60s"`
	AllowedOrigins       []string      `envconfig:"ALLOWED_ORIGINS" default:"*"`
	MaxSignalPayloadSize int64         `envconfig:"MAX_SIGNAL_PAYLOAD_SIZE" default:"5242880"` // 5MB
	MaxAPIRequestSize    int64         `envconfig:"MAX_API_REQUEST_SIZE" default:"65536"`      // 64KB
	RateLimitRPS         int32         `envconfig:"RATE_LIMIT_RPS" default:"100"`
	RateLimitBurst       int32         `envconfig:"RATE_LIMIT_BURST" default:"20"`
	ServiceMode          string        `envconfig:"SERVICE_MODE"`                   // Set by CLI flag, not env var
	DBMaxConnections     int32         `envconfig:"DB_MAX_CONNECTIONS" default:"4"` // pgx pool defaults
	DBMinConnections     int32         `envconfig:"DB_MIN_CONNECTIONS" default:"0"`
	DBMaxConnLifetime    time.Duration `envconfig:"DB_MAX_CONN_LIFETIME" default:"60m"`
	DBMaxConnIdleTime    time.Duration `envconfig:"DB_MAX_CONN_IDLE_TIME" default:"30m"`
	DBConnectTimeout     time.Duration `envconfig:"DB_CONNECT_TIMEOUT" default:"5s"`
}

const (
	RefreshTokenCookieName = "refresh_token"
	TokenIssuerName        = "Signalsd"

	// Security & Auth constants
	BcryptCost            = 12                  // bcrypt.DefaultCost = 10
	AccessTokenExpiry     = 30 * time.Minute    // JWT access token lifetime
	RefreshTokenExpiry    = 30 * 24 * time.Hour // Refresh token lifetime (30 days)
	OneTimeSecretExpiry   = 48 * time.Hour
	ClientSecretExpiry    = 365 * 24 * time.Hour // Client secret expiration (1 year)
	MinimumPasswordLength = 11

	// Operational timeouts
	ServerShutdownTimeout = 10 * time.Second // Server graceful shutdown timeout
	DatabasePingTimeout   = 10 * time.Second
	ReadinessTimeout      = 2 * time.Second // Health check timeout

	// JSON validation
	SkipValidationURL = "https://github.com/skip/validation/main/schema.json" // URL used to indicate JSON schema validation should be skipped
)

// common maps - used to validate enum values

var validEnvs = map[string]bool{
	"dev":     true,
	"test":    true,
	"perf":    true,
	"prod":    true,
	"staging": true,
}

var ValidVisibilities = map[string]bool{ // isn.visibility
	"public":  true,
	"private": true,
}

var ValidRoles = map[string]bool{ // users.user_role
	"owner":  true,
	"admin":  true,
	"member": true,
}

var ValidAccountTypes = map[string]bool{ // accounts.account_type
	"user":            true,
	"service_account": true,
}

var ValidISNPermissions = map[string]bool{ // isn_accounts.permission
	"read":  true,
	"write": true,
}

var ValidServiceModes = map[string]bool{ // service modes for CLI
	"all":           true,
	"admin":         true,
	"signals":       true, // both read and write
	"signals-read":  true, // read-only signal operations
	"signals-write": true, // write-only signal operations
}

// NewServerConfig loads environment variables using envconfig and returns a ServerConfig struct
func NewServerConfig(logger *zerolog.Logger) (*ServerConfig, error) {
	var cfg ServerConfig

	// load environment variables with defaults
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("failed to process environment variables: %w", err)
	}

	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	logger.Info().
		Str("ENVIRONMENT", cfg.Environment).
		Str("HOST", cfg.Host).
		Int("PORT", cfg.Port).
		Str("LOG_LEVEL", cfg.LogLevel.String()).
		Str("READ_TIMEOUT", cfg.ReadTimeout.String()).
		Str("WRITE_TIMEOUT", cfg.WriteTimeout.String()).
		Str("IDLE_TIMEOUT", cfg.IdleTimeout.String()).
		Int64("MAX_SIGNAL_PAYLOAD_SIZE", cfg.MaxSignalPayloadSize).
		Int32("RATE_LIMIT_RPS", cfg.RateLimitRPS).
		Int32("RATE_LIMIT_BURST", cfg.RateLimitBurst).
		Int32("DB_MAX_CONNECTIONS", cfg.DBMaxConnections).
		Int32("DB_MIN_CONNECTIONS", cfg.DBMinConnections).
		Str("DB_MAX_CONN_LIFETIME", cfg.DBMaxConnLifetime.String()).
		Str("DB_MAX_CONN_IDLE_TIME", cfg.DBMaxConnIdleTime.String()).
		Str("DB_CONNECT_TIMEOUT", cfg.DBConnectTimeout.String()).
		Msg("Configuration loaded")

	return &cfg, nil
}

// validateConfig checks for required env variables
func validateConfig(cfg *ServerConfig) error {
	if cfg.Environment == "prod" {
		if cfg.DatabaseURL == "" {
			return fmt.Errorf("DATABASE_URL is required in %s environment", cfg.Environment)
		}
		if cfg.SecretKey == "" {
			return fmt.Errorf("SECRET_KEY is required in %s environment", cfg.Environment)
		}

		// Additional production safety checks
		if len(cfg.SecretKey) < 32 {
			return fmt.Errorf("SECRET_KEY must be at least 32 characters in %s environment", cfg.Environment)
		}
		if !strings.Contains(cfg.DatabaseURL, "sslmode=require") && !strings.Contains(cfg.DatabaseURL, "sslmode=verify") {
			return fmt.Errorf("DATABASE_URL must use SSL in %s environment (add sslmode=require)", cfg.Environment)
		}
	}

	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535")
	}
	if !validEnvs[cfg.Environment] {
		return fmt.Errorf("invalid ENVIRONMENT: %s", cfg.Environment)
	}

	// Validate database pool configuration
	if cfg.DBMaxConnections < 1 {
		return fmt.Errorf("DB_MAX_CONNECTIONS must be at least 1")
	}
	if cfg.DBMinConnections < 0 {
		return fmt.Errorf("DB_MIN_CONNECTIONS must be 0 or greater")
	}
	if cfg.DBMinConnections > cfg.DBMaxConnections {
		return fmt.Errorf("DB_MIN_CONNECTIONS (%d) cannot be greater than DB_MAX_CONNECTIONS (%d)", cfg.DBMinConnections, cfg.DBMaxConnections)
	}

	return nil
}
