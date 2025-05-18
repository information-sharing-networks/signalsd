package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	internalMiddleware "github.com/nickabs/signalsd/app/internal/middleware"
	"github.com/nickabs/signalsd/app/internal/routes"
	"github.com/rs/zerolog/log"

	signals "github.com/nickabs/signalsd/app"
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

//	@securityDefinitions.ApiKey	BearerRefreshToken
//	@in							header
//	@name						Authorization
//	@description				Bearer { refresh token }

//	@tag.name			auth
//	@tag.description	User and token management endpoints

//	@tag.name			signal config
//	@tag.description	signal definitions describe the format of the data being shared in an ISN

//	@tag.name			ISN config
//	@tag.description	Information sharing networks are used to exchange signals between participating users.

//	@tag.name			ISN view
//	@tag.description	View information about the configured ISNs

func main() {
	// TODO - will the signal defs ever need to be private? Current implementation assumes 'no'

	cfg := signals.InitConfig()

	r := chi.NewRouter()

	//todoauthService := auth.NewAuthService(cfg)

	r.Use(chiMiddleware.RequestID)
	r.Use(internalMiddleware.LoggerMiddleware)

	log.Info().Msg("Starting server")

	routes.RegisterRoutes(r, cfg)

	serverAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Info().Msgf("%s service listening on %s \n", cfg.Environment, serverAddr)

	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("Server failed to start")
	}
}
