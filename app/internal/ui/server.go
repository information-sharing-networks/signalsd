package ui

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
)

const (
	// ServerShutdownTimeout is the timeout for graceful server shutdown
	ServerShutdownTimeout = 10 * time.Second
)

type Server struct {
	router      *chi.Mux
	config      *Config
	logger      *slog.Logger
	authService *AuthService
	apiClient   *Client
}

// NewStandaloneServer creates a new UI server that can run independently of the API
func NewStandaloneServer(cfg *Config, logger *slog.Logger) *Server {
	s := &Server{
		router:      chi.NewRouter(),
		config:      cfg,
		logger:      logger,
		authService: NewAuthService(cfg.APIBaseURL),
		apiClient:   NewClient(cfg.APIBaseURL),
	}

	s.setupMiddleware()
	s.RegisterRoutes(s.router)
	return s
}

// NewIntegratedServer creates a new UI server that runs as part of the API server
// The UI routes are registered on the signalsd router (passed as a parameter)
func NewIntegratedServer(router *chi.Mux, cfg *Config, logger *slog.Logger) *Server {
	s := &Server{
		router:      router,
		config:      cfg,
		logger:      logger,
		authService: NewAuthService(cfg.APIBaseURL),
		apiClient:   NewClient(cfg.APIBaseURL),
	}

	s.RegisterRoutes(s.router)
	return s
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
		r.Group(func(r chi.Router) {
			r.Use(s.RequireAdminAccess)
			r.Get("/admin/isn-accounts", s.handleIsnAccountsAdmin)
		})
		r.Get("/signal-types", s.handleSignalTypeManagement)

		// UI API endpoints (used when rendering ui components)
		r.Post("/ui-api/signal-types", s.handleGetSignalTypes)
		r.Post("/ui-api/signal-versions", s.handleGetSignalVersions)
		r.Post("/ui-api/search-signals", s.handleSearchSignals)
		r.Post("/ui-api/add-isn-account", s.handleAddIsnAccount)
		r.Post("/ui-api/create-signal-type", s.handleCreateSignalType)
	})

	// ISN routes (require ISN access)
	router.Group(func(r chi.Router) {
		r.Use(s.RequireAuth)
		r.Use(s.RequireIsnAccess)

		r.Get("/search", s.handleSignalSearch)
	})

	// Admin routes (require admin/owner role)
	router.Group(func(r chi.Router) {
		r.Use(s.RequireAuth)
		r.Use(s.RequireAdminAccess)

		r.Get("/admin", s.handleAdminDashboard)
	})
}

// setupMiddleware creates the routes when running the ui in standalone mode.
func (s *Server) setupMiddleware() {
	// middleware
	s.router.Use(chimiddleware.RequestID)
	s.router.Use(chimiddleware.RealIP)
	s.router.Use(logger.RequestLogging(s.logger)) // use the signalsd request logger
	s.router.Use(chimiddleware.Recoverer)
	s.router.Use(chimiddleware.Timeout(60 * time.Second))

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
		s.logger.Info("UI server listening", slog.String("address", addr))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case err := <-serverErr:
		return fmt.Errorf("server failed to start: %w", err)
	case <-ctx.Done():
		s.logger.Info("Shutting down UI server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), ServerShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("Server forced to shutdown", slog.String("error", err.Error()))
			return err
		}
	}

	return nil
}
