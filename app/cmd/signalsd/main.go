package main

import (
	"context"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/server"
	"github.com/jackc/pgx/v5/pgxpool"

	signalsd "github.com/information-sharing-networks/signalsd/app"

	_ "github.com/information-sharing-networks/signalsd/app/docs"
)

//	@title			Signals ISN API
//	@version		1.0
//	@description	Signals ISN service API
//	@license.name	MIT

//	@servers.url		https://api.example.com
//	@servers.description	Production server
//	@servers.url		http://localhost:8080
//	@servers.description	Development server

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
//	@tag.description	Manage the Information Sharing Networks that are used to exchange signals between participating users.

//	@tag.name			ISN Permissions
//	@tag.description	Grant accounts read or write access to an ISN

//	@tag.name			ISN view
//	@tag.description	View information about the configured ISNs

//	@tag.name			Signal definitions
//	@tag.description	Define the format of the data being shared in an ISN

// @tag.name			Service accounts
// @tag.description		Manage service account end points
func main() {

	serverLogger := logger.InitServerLogger()

	cfg := signalsd.NewServerConfig(serverLogger)

	httpLogger := logger.InitHttpLogger(cfg.LogLevel, cfg.Environment)

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

	safeURL, _ := removePassword(cfg.DatabaseURL)

	serverLogger.Info().Msgf("connected to PostgreSQL at %v", safeURL)

	queries := database.New(pool)

	authService := auth.NewAuthService(cfg.SecretKey, cfg.Environment, queries)
	router := chi.NewRouter()

	server := server.NewServer(pool, queries, authService, cfg, serverLogger, httpLogger, router)

	serverLogger.Info().Msg("Starting server")

	server.Run()

	serverLogger.Info().Msg("server shutdown complete")

}

func removePassword(connStr string) (string, error) {
	u, _ := url.Parse(connStr)
	if u.User != nil {
		username := u.User.Username()
		u.User = url.User(username)
	}

	return u.String(), nil
}
