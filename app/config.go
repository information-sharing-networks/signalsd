package signalsd

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// service configuration - these can be set as environment variables (sensible defaults are used where possible)
type ServerConfig struct {
	Environment          string
	Host                 string
	Port                 int
	SecretKey            string
	LogLevel             zerolog.Level
	DatabaseURL          string
	ReadTimeout          time.Duration
	WriteTimeout         time.Duration
	IdleTimeout          time.Duration
	AllowedOrigins       []string
	MaxSignalPayloadSize int64
	RateLimitRPS         int
	RateLimitBurst       int
	ServiceMode          string
}

// common constants
const (
	AccessTokenExpiry        = 30 * time.Minute
	RefreshTokenExpiry       = 30 * 24 * time.Hour
	MinimumPasswordLength    = 11
	RefreshTokenCookieName   = "refresh_token"
	TokenIssuerName          = "Signalsd"
	OneTimeSecretExpiry      = 48 * time.Hour
	ClientSecretExpiry       = 365 * 24 * time.Hour
	DefaultMaxAPIRequestSize = 64 * 1024 // 64KB for admin/auth/management API

)

// common maps - used to validate enum values

var validEnvs = map[string]bool{
	"dev":     true,
	"test":    true,
	"perf":    true,
	"prod":    true,
	"staging": true,
}

var ValidVisibilities = map[string]bool{ //stored in the isn.visibility column
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
	"signals":       true, // both read and write (backward compatibility)
	"signals-read":  true, // read-only signal operations
	"signals-write": true, // write-only signal operations
}

// NewServerConfig loads environment variables and returns a ServerConfig struct
func NewServerConfig(logger *zerolog.Logger) *ServerConfig {
	const (
		defaultHost                 = "0.0.0.0"
		defaultPort                 = 8080
		defaultEnviromnent          = "dev"
		defaultLogLevelStr          = "debug"
		defaultReadTimeout          = 15 * time.Second
		defaultWriteTimeout         = 15 * time.Second
		defaultIdleTimeout          = 60 * time.Second
		defaultMaxSignalPayloadSize = 5 * 1024 * 1024 // 5MB default
		defaultRateLimitRPS         = 100
		defaultRateLimitBurst       = 20 // burst of 20 requests
	)

	// log level
	var logLevel zerolog.Level

	logLevelStr := os.Getenv("LOG_LEVEL")
	if logLevelStr == "" {
		logger.Warn().Msgf("LOG_LEVEL not set, defaulting to %s", defaultLogLevelStr)
		logLevelStr = defaultLogLevelStr
	}
	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil {
		logLevel = zerolog.DebugLevel
		logger.Warn().Msg("LOG_LEVEL not valid, defaulting to debug")
	}

	logger.Info().Msgf("log level set to {%v} \n", logLevel)
	zerolog.SetGlobalLevel(logLevel)

	// environment
	environment := os.Getenv("ENVIRONMENT")
	if environment == "" {
		logger.Warn().Msgf("ENVIRONMENT environment variable is not set, defaulting to '%s'", defaultEnviromnent)
		environment = defaultEnviromnent
	}

	_, ok := validEnvs[environment]
	if !ok {
		logger.Fatal().Msgf("invalid ENVIRONMENT environment variable (expects %v)", validEnvs)
	}

	// database
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Fatal().Msg("DATABASE_URL environment variable is not set")
	}

	// http
	host := os.Getenv("HOST")

	if host == "" {
		logger.Warn().Msgf("HOST environment variable is not set, defaulting to '%s'", defaultHost)
		host = defaultHost
	}
	portString := os.Getenv("PORT")
	var port int

	if portString == "" {
		logger.Warn().Msgf("PORT environment variable is not set, defaulting to '%d'", defaultPort)
		port = defaultPort
	} else {
		port, err = strconv.Atoi(portString)
		if err != nil {
			logger.Fatal().Msg("invalid PORT environment variable")
		}
	}

	// secrets
	secretKey := os.Getenv("SECRET_KEY")
	if secretKey == "" {
		logger.Fatal().Msg("SECRET_KEY environment variable is not set")
	}

	//db timeouts
	writeTimeout := getEnvDuration("WRITE_TIMEOUT", defaultWriteTimeout)
	readTimeout := getEnvDuration("READ_TIMEOUT", defaultReadTimeout)
	idleTimeout := getEnvDuration("IDLE_TIMEOUT", defaultIdleTimeout)

	// CORS allowed origins
	allowedOrigins := getOrigins("ALLOWED_ORIGINS")

	// Signal payload size
	maxSignalPayloadSize := getEnvInt64("MAX_SIGNAL_PAYLOAD_SIZE", defaultMaxSignalPayloadSize)

	// Rate limiting
	rateLimitRPS := getEnvInt("RATE_LIMIT_RPS", defaultRateLimitRPS)
	rateLimitBurst := getEnvInt("RATE_LIMIT_BURST", defaultRateLimitBurst)

	return &ServerConfig{
		Environment:          environment,
		Host:                 host,
		Port:                 port,
		SecretKey:            secretKey,
		LogLevel:             logLevel,
		DatabaseURL:          databaseURL,
		ReadTimeout:          readTimeout,
		WriteTimeout:         writeTimeout,
		IdleTimeout:          idleTimeout,
		AllowedOrigins:       allowedOrigins,
		MaxSignalPayloadSize: maxSignalPayloadSize,
		RateLimitRPS:         rateLimitRPS,
		RateLimitBurst:       rateLimitBurst,
		ServiceMode:          "", // Will be set by CLI flag
	}
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// return origins from ALLOWED_ORIGINS (default to *)
func getOrigins(key string) []string {
	val := os.Getenv(key)
	if val == "" {
		return []string{"*"}
	}
	origins := strings.Split(val, ",")
	for i, origin := range origins {
		origins[i] = strings.TrimSpace(origin)
	}

	return origins
}
