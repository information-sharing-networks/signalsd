package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/handlers"
)

const (
	// ServerShutdownTimeout is the timeout for graceful server shutdown
	ServerShutdownTimeout = 10 * time.Second
)

type Server struct {
	router      *chi.Mux
	config      *config.Config
	logger      *slog.Logger
	authService *auth.AuthService
}

// NewStandaloneServer creates a new UI server that can run independently of the API
func NewStandaloneServer(cfg *config.Config, logger *slog.Logger) *Server {
	s := &Server{
		router:      chi.NewRouter(),
		config:      cfg,
		logger:      logger,
		authService: auth.NewAuthService(cfg.APIBaseURL, cfg.Environment),
	}

	s.setupMiddleware()
	s.RegisterRoutes(s.router)
	return s
}

// NewIntegratedServer creates a new UI server that runs as part of the API server
// The UI routes are registered on the signalsd router (passed as a parameter)
func NewIntegratedServer(router *chi.Mux, cfg *config.Config, logger *slog.Logger) *Server {

	s := &Server{
		router:      router,
		config:      cfg,
		logger:      logger,
		authService: auth.NewAuthService(cfg.APIBaseURL, cfg.Environment),
	}

	s.RegisterRoutes(s.router)
	return s
}

// RegisterRoutes registers UI routes on the signalsd router - use when running the integrated ui.
func (s *Server) RegisterRoutes(router *chi.Mux) {

	handlerService := &handlers.HandlerService{
		AuthService: auth.NewAuthService(s.config.APIBaseURL, s.config.Environment),
		ApiClient:   client.NewClient(s.config.APIBaseURL),
		Environment: s.config.Environment,
	}
	// Static assets (no auth required) - only UI's own CSS
	router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static/"))))

	// Public routes (no auth required)
	router.Get("/login", handlerService.LoginHandler)
	router.Post("/login", handlerService.LoginPostHandler)
	router.Get("/register", handlerService.RegisterLogin)
	router.Post("/register", handlerService.RegisterPostHandler)

	// redirects to dashboard if authenticated, login if not
	router.Get("/", handlerService.HomeHandler)

	// Protected routes (require authentication)
	router.Group(func(r chi.Router) {
		r.Use(s.authService.RequireAuth)

		// entry point after login
		r.Get("/dashboard", handlerService.DashboardHandler)

		// auth
		r.Post("/logout", handlerService.LogoutHandler)
		r.Get("/access-denied", handlerService.AccessDeniedHandler)
		r.Get("/need-isn-admin", handlerService.NeedIsnAdminHandler)

		// render drop down options
		r.Post("/ui-api/signal-type-options", handlerService.SignalTypeOptionsHandler)
		r.Post("/ui-api/signal-type-version-options", handlerService.SignalTypeVersionOptionsHandler)

		// render individual fields
		r.Post("/ui-api/account-identifier-field", handlerService.AccountIdentifierFieldHandler)

		// authenticated user forms
		r.Get("/search", handlerService.SignalSearchHandler)

		// execute backend api calls and render the results
		r.Post("/ui-api/search-signals", handlerService.SearchSignalsHandler)
	})

	// ISN Admin routes (require admin/owner role)
	router.Group(func(r chi.Router) {
		r.Use(s.authService.RequireAuth)
		r.Use(s.authService.RequireAdminOrOwnerRole)

		//dashboard
		r.Get("/admin", handlerService.IsnAdminDashboardHandler)

		//isn creation form
		r.Get("/admin/isns", handlerService.IsnManagementHandler)
		r.Post("/ui-api/create-isn", handlerService.CreateIsnHandler)

		// isn management forms - only relevant to admins that have created one or more ISN
		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireIsnAdmin)

			r.Get("/admin/signal-types", handlerService.SignalTypeManagementHandler)
			r.Get("/admin/isn-accounts", handlerService.IsnAccountManagementHandler)

			// execute backend api calls and render the results
			r.Post("/ui-api/create-signal-type", handlerService.CreateSignalTypeHandler)
			r.Post("/ui-api/update-isn-account-access", handlerService.UpdateIsnAccountHandler)
		})
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
