package signals

import (
	"database/sql"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
	"github.com/nickabs/signals/internal/database"
	"github.com/nickabs/signals/internal/logger"
	"github.com/rs/zerolog"
)

// service configuration
type ServiceConfig struct {
	DB          *database.Queries
	Environment string
	Port        int
	SecretKey   string
}

// shared constants
const (
	AccessTokenExpiry  = time.Hour
	RefreshTokenExpiry = 60 * 24 * time.Hour
)

// Common context keys
type ContextKey struct {
	Name string
}

var (
	RequestLoggerKey = ContextKey{"request-logger"}
	UserIDKey        = ContextKey{"user-id"}
)

// InitConfig loads environment variables, establishes database connection,
// and returns an initialized ServiceConfig struct.
func InitConfig() *ServiceConfig {
	const (
		defaultPort        = 8080
		defaultEnviromnent = "dev"
		defaultLogLevelStr = "debug"
	)
	validEnvs := map[string]bool{
		"dev":     true,
		"prod":    true,
		"test":    true,
		"staging": true,
	}

	// environment
	environment := os.Getenv("SIGNALS_ENVIRONMENT")
	if environment == "" {
		logger.ServerLogger.Warn().Msgf("SIGNALS_ENVIRONMENT environment variable is not set, defaulting to '%s'", defaultEnviromnent)
		environment = defaultEnviromnent
	}

	_, ok := validEnvs[environment]
	if !ok {
		logger.ServerLogger.Fatal().Msgf("invalid SIGNALS_ENVIRONMENT environment variable (expects %v)", validEnvs)
	}

	// database
	dbURL := os.Getenv("SIGNALS_DB_URL")
	if dbURL == "" {
		logger.ServerLogger.Fatal().Msg("SIGNALS_DB_URL environment variable is not set")
	}

	dbConn, err := sql.Open("postgres", dbURL)
	if err != nil {
		logger.ServerLogger.Fatal().Err(err).Msg("Error opening database connection")
	}

	if err = dbConn.Ping(); err != nil {
		logger.ServerLogger.Fatal().Err(err).Msg("Error pinging database")
	}

	dbQueries := database.New(dbConn)

	// http
	portString := os.Getenv("SIGNALS_PORT")
	var port int

	if portString == "" {
		logger.ServerLogger.Warn().Msgf("SIGNALS_PORT environment variable is not set, defaulting to '%d'", defaultPort)
		port = defaultPort
	} else {
		port, err = strconv.Atoi(portString)
		if err != nil {
			logger.ServerLogger.Fatal().Msg("invalid SIGNALS_PORT environment variable")
		}
	}

	// secrets
	secretKey := os.Getenv("SIGNALS_SECRET_KEY")
	if secretKey == "" {
		logger.ServerLogger.Fatal().Msg("SIGNALS_SECRET_KEY environment variable is not set")
	}

	// log level
	var logLevel zerolog.Level

	logLevelStr := os.Getenv("SIGNALS_LOG_LEVEL")
	if logLevelStr == "" {
		logger.ServerLogger.Warn().Msgf("SIGNALS_LOG_LEVEL not set, defaulting to %s", defaultLogLevelStr)
		logLevelStr = defaultLogLevelStr
	}
	logLevel, err = zerolog.ParseLevel(logLevelStr)
	if err != nil {
		logLevel = zerolog.DebugLevel
		logger.ServerLogger.Warn().Msg("SIGNALS_LOG_LEVEL not valid, defaulting to debug")
	}

	logger.ServerLogger.Info().Msgf("log level set to {%v} \n", logLevel)
	zerolog.SetGlobalLevel(logLevel)

	return &ServiceConfig{
		DB:          dbQueries,
		Environment: environment,
		Port:        port,
		SecretKey:   secretKey,
	}
}
