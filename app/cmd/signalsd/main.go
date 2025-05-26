package main

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/logger"
	"github.com/nickabs/signalsd/app/internal/server"

	signalsd "github.com/nickabs/signalsd/app"

	_ "github.com/nickabs/signalsd/app/docs"
)

//	@description	Signals ISN service API
//	@license		MIT
//	@host			localhost:8080

//	@accept		json
//	@produce	json

//	@securityDefinitions.ApiKey	BearerAccessToken
//	@in							header
//	@name						Authorization
//	@description				Bearer {JWT access token}

//	@tag.name			auth
//	@tag.description	User and token management endpoints

//	@tag.name			signal config
//	@tag.description	signal definitions describe the format of the data being shared in an ISN

//	@tag.name			ISN config
//	@tag.description	Information sharing networks are used to exchange signalsd between participating users.

//	@tag.name			ISN view
//	@tag.description	View information about the configured ISNs

func main() {

	serverLogger := logger.InitServerLogger()

	cfg := signalsd.InitConfig(serverLogger)

	httpLogger := logger.InitHttpLogger(cfg.LogLevel)

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		serverLogger.Fatal().Err(err).Msg("Error opening database connection")
	}

	if err = db.Ping(); err != nil {
		serverLogger.Fatal().Err(err).Msg("Error pinging database")
	}

	queries := database.New(db)

	authService := auth.NewAuthService(cfg.SecretKey, cfg.Environment, queries)

	router := chi.NewRouter()

	server := server.NewServer(db, queries, authService, cfg, serverLogger, httpLogger, router)

	serverLogger.Info().Msg("Starting server")

	server.Run()

	serverLogger.Info().Msg("server shutdown complete")

}
