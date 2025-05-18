package signals

import (
	"database/sql"
	"os"
	"strconv"
	"time"

	_ "github.com/lib/pq"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/logger"
	"github.com/rs/zerolog"
)

/*
config sets up shared variables for the service:
- ServiceConfig: main calls initConfig() and gets a pointer to the newly initialized config struct - the config is then passed to all handlers as a parameter.
- common constants - e.g token expiry times
- common maps - used to list valid values for certain fields e.g signalDefs.Stage
*/

// service configuration
type ServiceConfig struct {
	DB          *database.Queries
	Environment string
	Host        string
	Port        int
	SecretKey   string
	LogLevel    zerolog.Level
}

// common constants
const (
	AccessTokenExpiry     = time.Hour
	RefreshTokenExpiry    = 60 * 24 * time.Hour
	MinimumPasswordLength = 11
)

// common maps - used to validate enum values
var validEnvs = map[string]bool{
	"dev":     true,
	"prod":    true,
	"test":    true,
	"staging": true,
}
var ValidSignalDefStages = map[string]bool{ // stored in the signal_defs.stage column
	"dev":        true,
	"test":       true,
	"live":       true,
	"deprecated": true,
	"closed":     true,
	"shuttered":  true,
}
var ValidPayloadValidations = map[string]bool{ //stored in the isn_receivers.payload_validation column
	"always":   true,
	"never":    true,
	"optional": true,
}
var ValidVisibilities = map[string]bool{ //stored in the isn.visibility column
	"public":  true,
	"private": true,
}
var ValidRetrieverStatus = map[string]bool{ // ins_retriever.retriever_status
	"offline": true,
	"online":  true,
	"error":   true,
	"closed":  true,
}
var ValidReceiverStatus = map[string]bool{ // ins_receiver.receiver_status
	"offline": true,
	"online":  true,
	"error":   true,
	"closed":  true,
}

// InitConfig loads environment variables, establishes database connection and returns a ServiceConfig struct
func InitConfig() *ServiceConfig {
	const (
		defaultHost        = "127.0.0.1"
		defaultPort        = 8080
		defaultEnviromnent = "dev"
		defaultLogLevelStr = "debug"
	)

	// log level
	var logLevel zerolog.Level

	logLevelStr := os.Getenv("SIGNALS_LOG_LEVEL")
	if logLevelStr == "" {
		logger.ServerLogger.Warn().Msgf("SIGNALS_LOG_LEVEL not set, defaulting to %s", defaultLogLevelStr)
		logLevelStr = defaultLogLevelStr
	}
	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil {
		logLevel = zerolog.DebugLevel
		logger.ServerLogger.Warn().Msg("SIGNALS_LOG_LEVEL not valid, defaulting to debug")
	}

	logger.ServerLogger.Info().Msgf("log level set to {%v} \n", logLevel)
	zerolog.SetGlobalLevel(logLevel)
	logger.InitLogger(logLevel)

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

	host := os.Getenv("SIGNALS_HOST")

	if host == "" {
		logger.ServerLogger.Warn().Msgf("SIGNALS_HOST environment variable is not set, defaulting to '%s'", defaultHost)
		host = defaultHost
	}
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

	return &ServiceConfig{
		DB:          dbQueries,
		Environment: environment,
		Host:        host,
		Port:        port,
		SecretKey:   secretKey,
		LogLevel:    logLevel,
	}
}
