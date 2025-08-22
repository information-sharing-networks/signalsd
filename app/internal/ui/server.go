package ui

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	signalsd "github.com/information-sharing-networks/signalsd/app"
	"github.com/rs/zerolog"
)

type Server struct {
	router       *chi.Mux
	serverConfig *signalsd.ServerConfig
	corsConfigs  *signalsd.CORSConfigs
	logger       *zerolog.Logger
	authService  *AuthService
}

func NewServer(cfg *signalsd.ServerConfig, corsConfigs *signalsd.CORSConfigs, logger *zerolog.Logger) *Server {
	// Determine API base URL for authentication
	apiBaseURL := "http://localhost:8080"
	if cfg.Environment == "production" {
		// TODO: Set production API URL from environment variable
		apiBaseURL = "https://api.yourdomain.com"
	}

	s := &Server{
		router:       chi.NewRouter(),
		serverConfig: cfg,
		corsConfigs:  corsConfigs,
		logger:       logger,
		authService:  NewAuthService(apiBaseURL),
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// Basic middleware
	s.router.Use(chimiddleware.RequestID)
	s.router.Use(chimiddleware.RealIP)
	s.router.Use(chimiddleware.Recoverer)
	s.router.Use(chimiddleware.Timeout(60 * time.Second))

	// Static assets
	s.router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static/"))))

	// UI routes
	s.router.Get("/", s.handleHome)
	s.router.Get("/login", s.handleLogin)
	s.router.Post("/login", s.handleLoginPost)
	s.router.Post("/logout", s.handleLogout)
	s.router.Get("/dashboard", s.handleDashboard)

	// Redirect to existing swagger docs
	s.router.Get("/docs", s.handleDocs)
	s.router.Get("/docs/*", s.handleDocs)
}

func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.serverConfig.Host, s.serverConfig.Port)

	server := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  s.serverConfig.ReadTimeout,
		WriteTimeout: s.serverConfig.WriteTimeout,
		IdleTimeout:  s.serverConfig.IdleTimeout,
	}

	// Start server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		s.logger.Info().Msgf("UI server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case err := <-serverErr:
		return fmt.Errorf("server failed to start: %w", err)
	case <-ctx.Done():
		s.logger.Info().Msg("Shutting down UI server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), signalsd.ServerShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			s.logger.Error().Err(err).Msg("Server forced to shutdown")
			return err
		}
	}

	return nil
}
