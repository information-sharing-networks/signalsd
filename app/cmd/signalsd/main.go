package main

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

//	@tag.name			Site admin
//	@tag.description	Site adminstration tools

//	@tag.name			ISN configuration
//	@tag.description	Information sharing networks are used to exchange signalsd between participating users.

//	@tag.name			ISN Permissions
//	@tag.description	Grant accounts read or write access to an ISN

//	@tag.name			ISN view
//	@tag.description	View information about the configured ISNs

//	@tag.name			Signal definitions
//	@tag.description	Signal definitions describe the format of the data being shared in an ISN

//	@tag.name			Signal sharing
//	@tag.description	Send and recieve signals over the ISN

func main() {

	serverLogger := logger.InitServerLogger()

	cfg := signalsd.InitConfig(serverLogger)

	httpLogger := logger.InitHttpLogger(cfg.LogLevel)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		serverLogger.Fatal().Err(err).Msg("Failed to parse database URL")
	}

	if cfg.Environment == "prod" {
		poolConfig.MaxConns = 8
		poolConfig.MinConns = 4
		poolConfig.MaxConnLifetime = 30 * time.Minute
		poolConfig.MaxConnIdleTime = 5 * time.Minute
		poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		serverLogger.Fatal().Err(err).Msg("Unable to create connection pool")
	}

	if err = pool.Ping(ctx); err != nil {
		serverLogger.Fatal().Err(err).Msg("Error pinging database via pool")
	}
	serverLogger.Info().Msgf("connected to PostgreSQL at %v", cfg.DatabaseURL)

	queries := database.New(pool)

	authService := auth.NewAuthService(cfg.SecretKey, cfg.Environment, queries)
	router := chi.NewRouter()

	server := server.NewServer(pool, queries, authService, cfg, serverLogger, httpLogger, router)

	serverLogger.Info().Msg("Starting server")

	server.Run()

	serverLogger.Info().Msg("server shutdown complete")

}
