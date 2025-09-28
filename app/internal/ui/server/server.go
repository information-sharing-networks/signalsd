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

// NewStandaloneServer creates a UI server that can run independently of the API
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
// note: dynamic HMTX handlers are registered with routes starting /ui-api
func (s *Server) RegisterRoutes(router *chi.Mux) {

	handlerService := &handlers.HandlerService{
		AuthService: auth.NewAuthService(s.config.APIBaseURL, s.config.Environment),
		ApiClient:   client.NewClient(s.config.APIBaseURL),
		Environment: s.config.Environment,
	}
	// Public routes (no auth required)
	router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static/"))))
	router.Get("/login", handlerService.LoginPage)
	router.Post("/login", handlerService.Login)
	router.Post("/logout", handlerService.Logout)
	router.Get("/register", handlerService.RegisterPage)
	router.Post("/register", handlerService.Register)

	// redirects to dashboard if authenticated, login if not
	router.Get("/", handlerService.HomePage)

	// Protected routes
	router.Group(func(r chi.Router) {
		r.Use(s.authService.RequireAuth)
		r.Use(s.authService.AddAccountIDToLogContext)

		// entry point after login
		r.Get("/dashboard", handlerService.DashboardPage)

		// auth
		r.Get("/access-denied", handlerService.AccessDeniedPage)
		r.Get("/need-isn-admin", handlerService.AccessDeniedNeedIsnAdminPage)
		r.Get("/need-isn-access", handlerService.AccessDeniedNeedIsnAccessPage)

		//alerts
		r.Get("/ui-api/clear-alerts", handlerService.ClearAlerts)

		// render dropdown list options
		r.Post("/ui-api/signal-type-options", handlerService.RenderSignalTypeOptions)
		r.Post("/ui-api/signal-type-version-options", handlerService.RenderSignalTypeVersionOptions)
		r.Get("/ui-api/service-account-options", handlerService.RenderServiceAccountOptions)
		r.Get("/ui-api/user-options", handlerService.RenderUserOptionsGeneratePasswordLink)

		// render individual fields
		r.Get("/ui-api/account-identifier-field", handlerService.RenderAccountIdentifierField)

		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireIsnAccess)

			// search signals
			r.Get("/search", handlerService.SearchSignalsPage)
			r.Post("/ui-api/search-signals", handlerService.SearchSignals)
		})

	})

	// ISN Admin routes (require admin/owner role)
	router.Group(func(r chi.Router) {
		r.Use(s.authService.RequireAuth)
		r.Use(s.authService.RequireAdminOrOwnerRole)
		r.Use(s.authService.AddAccountIDToLogContext)

		//dashboard
		r.Get("/admin", handlerService.IsnAdminDashboardPage)

		// user management
		r.Get("/admin/users", handlerService.ManageUsersPage)

		//isn creation
		r.Get("/admin/isns", handlerService.CreateIsnPage)
		r.Post("/ui-api/create-isn", handlerService.CreateIsn)

		// service accounts
		r.Get("/admin/service-accounts", handlerService.ManageServiceAccountsPage)
		r.Post("/ui-api/create-service-account", handlerService.CreateServiceAccount)
		r.Post("/ui-api/reissue-service-account", handlerService.ReissueServiceAccount)
		r.Get("/ui-api/reissue-btn-state", handlerService.ReissueButtonState)

		// user accounts
		r.Post("/ui-api/generate-password-reset-link", handlerService.GeneratePasswordResetLink)
		r.Get("/ui-api/generate-password-reset-btn-state", handlerService.GeneratePasswordResetButtonState)

		// isn management forms
		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireIsnAdmin) //  the below features are only relevant to admins that have created one or more ISN

			r.Get("/admin/signal-types", handlerService.CreateSignalTypePage)
			r.Post("/ui-api/create-signal-type", handlerService.CreateSignalType)

			r.Get("/admin/isn-accounts", handlerService.UpdateIsnAccountPage)
			r.Post("/ui-api/update-isn-account", handlerService.UpdateIsnAccount)
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

	// Start server
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
