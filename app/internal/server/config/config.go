package signalsd

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jub0bs/cors"
	"github.com/kelseyhightower/envconfig"
)

// Environment variables are automatically mapped using envconfig.
type ServerConfig struct {
	Environment          string        `envconfig:"ENVIRONMENT" default:"dev"`
	Host                 string        `envconfig:"HOST" default:"0.0.0.0"`
	Port                 int           `envconfig:"PORT" default:"8080"`
	SecretKey            string        `envconfig:"SECRET_KEY" required:"true"`
	LogLevel             string        `envconfig:"LOG_LEVEL" default:"debug"`
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

// CORSConfigs holds the CORS middleware instances for different endpoint types
type CORSConfigs struct {
	Public    *cors.Middleware
	Protected *cors.Middleware
}

const (
	RefreshTokenCookieName = "refresh_token"
	TokenIssuerName        = "Signalsd"

	// Security & Auth constants
	BcryptCost            = 10                  // bcrypt.DefaultCost = 10
	AccessTokenExpiry     = 30 * time.Minute    // JWT access token lifetime
	RefreshTokenExpiry    = 30 * 24 * time.Hour // Refresh token lifetime (30 days)
	OneTimeSecretExpiry   = 48 * time.Hour
	ClientSecretExpiry    = 365 * 24 * time.Hour // Client secret expiration (1 year)
	MinimumPasswordLength = 11

	// Operational timeouts
	ServerShutdownTimeout = 10 * time.Second // Server graceful shutdown timeout
	DatabasePingTimeout   = 10 * time.Second
	ReadinessTimeout      = 2 * time.Second // Health check timeout

	// CORS settings
	CORSMaxAgeInSeconds = 86400 // 24 hours

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
	"all":           true, // signalsd backend + embeded UI
	"api":           true, // all the api endpoints (no UI)
	"admin":         true, // admin endpoints w/o signals ops
	"signals":       true, // both read and write
	"signals-read":  true, // read-only signal operations
	"signals-write": true, // write-only signal operations
}

// NewServerConfig loads environment variables using envconfig and returns a ServerConfig struct and CORSConfigs
func NewServerConfig() (*ServerConfig, *CORSConfigs, error) {
	var cfg ServerConfig

	// load environment variables with defaults
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, nil, fmt.Errorf("failed to process environment variables: %w", err)
	}

	if err := validateConfig(&cfg); err != nil {
		return nil, nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Initialize CORS configurations
	corsConfigs, err := createCORSConfigs(&cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("CORS configuration failed: %w", err)
	}

	return &cfg, corsConfigs, nil
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

// createCORSConfigs creates the CORS configurations based on the server config
func createCORSConfigs(cfg *ServerConfig) (*CORSConfigs, error) {
	var origins []string
	if len(cfg.AllowedOrigins) == 0 || (len(cfg.AllowedOrigins) == 1 && strings.TrimSpace(cfg.AllowedOrigins[0]) == "*") {
		origins = []string{"*"}
	} else {
		// Trim whitespace from all origins
		origins = make([]string, len(cfg.AllowedOrigins))
		for i, origin := range cfg.AllowedOrigins {
			origins[i] = strings.TrimSpace(origin)
		}
	}

	publicConfig := cors.Config{
		Origins: []string{"*"},
		Methods: []string{
			http.MethodGet,
			http.MethodHead,
			http.MethodOptions,
		},
		RequestHeaders: []string{
			"Accept",
			"Content-Type",
			"X-Requested-With",
		},
		MaxAgeInSeconds: CORSMaxAgeInSeconds,
	}

	publicMiddleware, err := cors.NewMiddleware(publicConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create public CORS middleware: %w", err)
	}

	protectedConfig := cors.Config{
		Origins: origins,
		Methods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},
		RequestHeaders: []string{
			"Content-Type",
			"Authorization",
			"X-Requested-With",
		},
		MaxAgeInSeconds: CORSMaxAgeInSeconds,
	}

	protectedMiddleware, err := cors.NewMiddleware(protectedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create protected CORS middleware: %w", err)
	}

	corsConfigs := &CORSConfigs{
		Public:    publicMiddleware,
		Protected: protectedMiddleware,
	}

	return corsConfigs, nil
}
