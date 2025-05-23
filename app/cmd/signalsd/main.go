package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/handlers"
	"github.com/nickabs/signalsd/app/internal/logger"
	internalMiddleware "github.com/nickabs/signalsd/app/internal/middleware"
	"github.com/nickabs/signalsd/app/internal/routes"
	"github.com/nickabs/signalsd/app/internal/services"
	"github.com/rs/zerolog/log"

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

//	@securityDefinitions.ApiKey	BearerRefreshToken
//	@in							header
//	@name						Authorization
//	@description				Bearer { refresh token }

//	@tag.name			auth
//	@tag.description	User and token management endpoints

//	@tag.name			signal config
//	@tag.description	signal definitions describe the format of the data being shared in an ISN

//	@tag.name			ISN config
//	@tag.description	Information sharing networks are used to exchange signalsd between participating users.

//	@tag.name			ISN view
//	@tag.description	View information about the configured ISNs

func main() {

	cfg := signalsd.InitConfig()

	// db connection
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		logger.ServerLogger.Fatal().Err(err).Msg("Error opening database connection")
	}

	if err = db.Ping(); err != nil {
		logger.ServerLogger.Fatal().Err(err).Msg("Error pinging database")
	}

	queries := database.New(db)

	// auth services
	authService := auth.NewAuthService(cfg.SecretKey, cfg.Environment, queries)

	// user definintion and authentication services
	usersHandler := handlers.NewUserHandler(queries, authService, db)
	loginHandler := handlers.NewLoginHandler(queries, authService)
	tokenHandler := handlers.NewTokenHandler(queries, authService)

	// site admin services - tood authServices with admin/owner role
	adminHandler := handlers.NewAdminHandler(queries)

	// middleware handles auth on the remaing services

	// isn definition services
	isnHandler := handlers.NewIsnHandler(queries)
	signalTypeHandler := handlers.NewSignalTypeHandler(queries)
	isnReceiverHandler := handlers.NewIsnReceiverHandler(queries)
	isnRetrieverHandler := handlers.NewIsnRetrieverHandler(queries)

	// signald runtime services
	webhookHandler := handlers.NewWebhookHandler(queries)

	services := services.Services{
		Admin:        adminHandler,
		Users:        usersHandler,
		Login:        loginHandler,
		Token:        tokenHandler,
		Webhook:      webhookHandler,
		SignalType:   signalTypeHandler,
		Isn:          isnHandler,
		IsnReceiver:  isnReceiverHandler,
		IsnRetriever: isnRetrieverHandler,
		AuthService:  authService,
	}
	r := chi.NewRouter()

	r.Use(chiMiddleware.RequestID)
	r.Use(internalMiddleware.LoggerMiddleware)

	log.Info().Msg("Starting server")

	routes.RegisterRoutes(r, services)

	serverAddr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	server := &http.Server{
		Addr:         serverAddr,
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// close the db connections when exiting
	defer func() {
		err := db.Close()
		if err != nil {
			log.Warn().Msgf("error closing database connections: %v", err)
		}
	}()

	go func() {
		log.Info().Msgf("%s service listening on %s \n", cfg.Environment, serverAddr)

		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed to start")
		}
	}()

	idleConnsClosed := make(chan struct{}, 1)

	sigint := make(chan os.Signal, 1)

	signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

	<-sigint

	log.Info().Msg("service shutting down")

	// force an exit if server does not shutdown within 10 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	// if the server shutsdown in under 10 seconds, exit immediately
	defer cancel()

	err = server.Shutdown(ctx)
	if err != nil {
		log.Warn().Msgf("shutdown error: %v", err)
	}
	//todo close DB connection

	close(idleConnsClosed)
	log.Info().Msg("database connect closed debug")

	<-idleConnsClosed

	log.Info().Msg("server shutdown complete")

}
