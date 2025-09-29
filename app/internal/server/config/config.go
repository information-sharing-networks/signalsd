package signalsd

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Netflix/go-env"
	"github.com/jub0bs/cors"
)

// Environment variables with defaults
type ServerEnvironment struct {
	Environment          string        `env:"ENVIRONMENT,default=dev"`
	Host                 string        `env:"HOST,default=0.0.0.0"`
	Port                 int           `env:"PORT,default=8080"`
	PublicBaseURL        string        `env:"PUBLIC_BASE_URL"` // base url for user facing links (defaults to Host/Port values = see below)
	SecretKey            string        `env:"SECRET_KEY,required=true"`
	LogLevel             string        `env:"LOG_LEVEL,default=debug"`
	DatabaseURL          string        `env:"DATABASE_URL,required=true"`
	ReadTimeout          time.Duration `env:"READ_TIMEOUT,default=15s"`
	WriteTimeout         time.Duration `env:"WRITE_TIMEOUT,default=15s"`
	IdleTimeout          time.Duration `env:"IDLE_TIMEOUT,default=60s"`
	AllowedOrigins       []string      `env:"ALLOWED_ORIGINS,separator=|"`
	MaxSignalPayloadSize int64         `env:"MAX_SIGNAL_PAYLOAD_SIZE,default=5242880"` // 5MB
	MaxAPIRequestSize    int64         `env:"MAX_API_REQUEST_SIZE,default=65536"`      // 64KB
	RateLimitRPS         int32         `env:"RATE_LIMIT_RPS,default=100"`
	RateLimitBurst       int32         `env:"RATE_LIMIT_BURST,default=20"`
	ServiceMode          string        `env:"SERVICE_MODE"`                 // Set by CLI flag, not env var
	DBMaxConnections     int32         `env:"DB_MAX_CONNECTIONS,default=4"` // pgx pool defaults
	DBMinConnections     int32         `env:"DB_MIN_CONNECTIONS,default=0"`
	DBMaxConnLifetime    time.Duration `env:"DB_MAX_CONN_LIFETIME,default=60m"`
	DBMaxConnIdleTime    time.Duration `env:"DB_MAX_CONN_IDLE_TIME,default=30m"`
	DBConnectTimeout     time.Duration `env:"DB_CONNECT_TIMEOUT,default=5s"`
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
	BcryptCost            = 10                   // bcrypt.DefaultCost = 10
	AccessTokenExpiry     = 30 * time.Minute     // JWT access token lifetime
	RefreshTokenExpiry    = 30 * 24 * time.Hour  // Refresh token lifetime (30 days)
	OneTimeSecretExpiry   = 48 * time.Hour       // Service account setup tokens
	PasswordResetExpiry   = 30 * time.Minute     // Password reset tokens
	ClientSecretExpiry    = 365 * 24 * time.Hour // Client secret expiration (1 year)
	MinimumPasswordLength = 11

	// Operational timeouts
	ServerShutdownTimeout = 10 * time.Second // Server graceful shutdown timeout
	DatabasePingTimeout   = 10 * time.Second
	ReadinessTimeout      = 2 * time.Second // Health check timeout

	// CORS settings
	CORSMaxAgeInSeconds = 86400 // 24 hours

	// Signal Type file validation
	SkipValidationURL = "https://github.com/skip/validation/main/schema.json" // URL used to indicate JSON schema validation should be skipped
	SkipReadmeURL     = "https://github.com/skip/readme/main/readme.md"       // URL used to indicate there is no readme for the signal type

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
	"service-account": true,
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
func NewServerConfig() (*ServerEnvironment, *CORSConfigs, error) {
	var cfg ServerEnvironment

	_, err := env.UnmarshalFromEnviron(&cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal environment variables: %w", err)
	}

	// default to host/port if not set (the env must be set for production - checked in the validateConfig function)
	if cfg.Environment != "prod" && cfg.PublicBaseURL == "" {
		cfg.PublicBaseURL = fmt.Sprintf("http://%s:%d", cfg.Host, cfg.Port)
	}

	if err := validateConfig(&cfg); err != nil {
		return nil, nil, err
	}

	// Initialize CORS configurations
	corsConfigs, err := createCORSConfigs(&cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("CORS configuration failed: %w", err)
	}

	return &cfg, corsConfigs, nil
}

// validateConfig checks for required env variables
func validateConfig(cfg *ServerEnvironment) error {
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

	u, err := url.ParseRequestURI(cfg.PublicBaseURL)
	if err != nil {
		return fmt.Errorf("PUBLIC_BASE_URL is not a valid URL: %s", cfg.PublicBaseURL)
	}

	if u.Scheme == "" {
		return fmt.Errorf("PUBLIC_BASE_URL does not include a valid scheme (http or https): %s", cfg.PublicBaseURL)
	}

	if cfg.Environment == "prod" {

		if u.Scheme == "http" && u.Hostname() == "localhost" {
			return fmt.Errorf("PUBLIC_BASE_URL must be set in production")
		}
		if u.Scheme != "https" {
			return fmt.Errorf("PUBLIC_BASE_URL must use https in production: %s", cfg.PublicBaseURL)
		}
		if u.Port() != "" {
			return fmt.Errorf("PUBLIC_BASE_URL should not include a port in production: %s", cfg.PublicBaseURL)
		}
	}

	if u.Hostname() == "" {
		return fmt.Errorf("PUBLIC_BASE_URL does not include a host: %s", cfg.PublicBaseURL)
	}

	if u.Path != "" || u.Path == "/" {
		return fmt.Errorf("PUBLIC_BASE_URL should not include a path: %s", cfg.PublicBaseURL)
	}

	if cfg.Environment == "prod" || cfg.Environment == "staging " {
		if len(cfg.AllowedOrigins) == 0 {
			return fmt.Errorf("ALLOWED_ORIGINS must be set in %v", cfg.Environment)
		}
		if cfg.AllowedOrigins[0] == "*" {
			return fmt.Errorf("ALLOWED_ORIGINS must not be set to '*' in %v", cfg.Environment)
		}
	}

	// default to all origins when not in prod/staging
	if len(cfg.AllowedOrigins) == 0 {
		cfg.AllowedOrigins = []string{"*"}
	}
	return nil
}

// createCORSConfigs creates the CORS configurations based on the server config
func createCORSConfigs(cfg *ServerEnvironment) (*CORSConfigs, error) {
	// Trim whitespace from all origins
	origins := make([]string, len(cfg.AllowedOrigins))
	for i, origin := range cfg.AllowedOrigins {
		origins[i] = strings.TrimSpace(origin)
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
