package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/auth"
	"github.com/nickabs/signals/internal/logger"
	internalMiddleware "github.com/nickabs/signals/internal/middleware"
	"github.com/nickabs/signals/internal/routes"

	_ "github.com/nickabs/signals/docs"
)

// @title Signals
// @version v0.0.1
// @description Signals service API
// @license MIT
// @host localhost:8080
// @accept json
// @produce json
//
// @securityDefinitions.ApiKey  BearerAuth
// @in header
// @name Authorization
// @description Bearer {JWT access token}
//
// @externalDocs.description  OpenAPI

// TODO - will the signal defs ever need to be private? Current implementation assumes 'no'
func main() {
	logger.ServerLogger.Info().Msg("Starting server")

	cfg := signals.InitConfig()

	r := chi.NewRouter()

	authService := auth.NewAuthService(cfg)

	r.Use(chiMiddleware.RequestID)
	r.Use(internalMiddleware.LoggerMiddleware)
	r.Use(internalMiddleware.AuthorizationMiddleware(*authService))

	//TODO
	//r.Use(chiMiddleware.Recoverer)
	//r.Use(chiMiddleware.Timeout(60 * time.Second))

	routes.RegisterRoutes(r, cfg)

	serverAddr := fmt.Sprintf("localhost:%d", cfg.Port)
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	logger.ServerLogger.Info().Msgf("%s service listening on %s \n", cfg.Environment, serverAddr)

	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		logger.ServerLogger.Fatal().Err(err).Msg("Server failed to start")
	}
}
