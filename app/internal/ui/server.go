package ui

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
)

const (
	// ServerShutdownTimeout is the timeout for graceful server shutdown
	ServerShutdownTimeout = 10 * time.Second
)

type Server struct {
	router      *chi.Mux
	config      *UIConfig
	logger      *zerolog.Logger
	authService *AuthService
	apiClient   *APIClient
}

func NewServer(cfg *UIConfig, logger *zerolog.Logger) *Server {
	s := &Server{
		router:      chi.NewRouter(),
		config:      cfg,
		logger:      logger,
		authService: NewAuthService(cfg.APIBaseURL),
		apiClient:   NewAPIClient(cfg.APIBaseURL),
	}

	s.setupStandaloneRoutes()
	return s
}

// Router returns the UI server's router for mounting in other servers
func (s *Server) Router() *chi.Mux {
	return s.router
}

// RegisterRoutes registers UI routes on the signalsd router - use when running the integrated ui.
func (s *Server) RegisterRoutes(router *chi.Mux) {
	// Static assets (no auth required) - only UI's own CSS
	router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static/"))))

	// Public routes (no auth required)
	router.Get("/login", s.handleLogin)
	router.Post("/login", s.handleLoginPost)
	router.Get("/register", s.handleRegister)
	router.Post("/register", s.handleRegisterPost)

	// redirects to dashboard if authenticated, login if not
	router.Get("/", s.handleHome)

	// Protected routes (require authentication)
	router.Group(func(r chi.Router) {
		r.Use(s.RequireAuth)

		r.Post("/logout", s.handleLogout)
		r.Get("/dashboard", s.handleDashboard)
		r.Get("/search", s.handleSignalSearch)

		// UI API endpoints (used when rendering ui components)
		r.Post("/ui-api/signal-types", s.handleGetSignalTypes)
		r.Post("/ui-api/signal-versions", s.handleGetSignalVersions)
		r.Post("/ui-api/search-signals", s.handleSearchSignals)
	})
}

// setupStandaloneRoutes creates the routes when running the ui in standalone mode.
func (s *Server) setupStandaloneRoutes() {
	// middleware
	s.router.Use(chimiddleware.RequestID)
	s.router.Use(chimiddleware.RealIP)
	s.router.Use(chimiddleware.Recoverer)
	s.router.Use(chimiddleware.Timeout(60 * time.Second))

	// Static assets
	s.router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static/"))))

	// Public routes (no auth required)
	s.router.Get("/login", s.handleLogin)
	s.router.Post("/login", s.handleLoginPost)
	s.router.Get("/register", s.handleRegister)
	s.router.Post("/register", s.handleRegisterPost)

	// redirects to dashboard if authenticated, login if not
	s.router.Get("/", s.handleHome)

	// Protected routes (require authentication)
	s.router.Group(func(r chi.Router) {
		r.Use(s.RequireAuth)

		r.Post("/logout", s.handleLogout)
		r.Get("/dashboard", s.handleDashboard)
		r.Get("/search", s.handleSignalSearch)
		r.Post("/ui-api/signal-types", s.handleGetSignalTypes)
		r.Post("/ui-api/signal-versions", s.handleGetSignalVersions)
		r.Post("/ui-api/search-signals", s.handleSearchSignals)
	})
}

// Start the UI server in standalone mode
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	server := &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
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

		shutdownCtx, cancel := context.WithTimeout(context.Background(), ServerShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			s.logger.Error().Err(err).Msg("Server forced to shutdown")
			return err
		}
	}

	return nil
}
