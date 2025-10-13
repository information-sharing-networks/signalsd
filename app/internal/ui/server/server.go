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

	router.Group(func(r chi.Router) {
		// Protected routes - require valid access token
		r.Use(s.authService.RequireAuth)
		r.Use(s.authService.AddAccountIDToLogContext)

		// entry point after login
		r.Get("/dashboard", handlerService.DashboardPage)

		// auth
		r.Get("/access-denied", handlerService.AccessDeniedPage)
		r.Get("/need-isn-admin", handlerService.AccessDeniedNeedIsnAdminPage)
		r.Get("/need-isn-access", handlerService.AccessDeniedNeedIsnAccessPage)

		// HTMX endpoints for cascading signal type dropdowns
		r.Get("/ui-api/options/signal-type-slugs", handlerService.RenderSignalTypeSlugOptions)
		r.Get("/ui-api/options/signal-type-versions", handlerService.RenderSignalTypeVersionOptions)

		// checkbox toggles
		r.Get("/ui-api/toggles/skip-validation", handlerService.ToggleSkipValidation)
		r.Get("/ui-api/toggles/skip-readme", handlerService.ToggleSkipReadme)

		r.Group(func(r chi.Router) {
			// these routes are only relevant where accounts have been granted access to one or more ISNs
			r.Use(s.authService.RequireIsnAccess)

			// search signals
			r.Get("/search", handlerService.SearchSignalsPage)
			r.Get("/ui-api/signals/search", handlerService.SearchSignals)
			r.Get("/ui-api/isn/{isnSlug}/signal_types/{signalTypeSlug}/v{semVer}/signals/{signalID}/correlated-count/{count}", handlerService.GetLatestCorrelatedSignals)
		})

		r.Group(func(r chi.Router) {

			// ISN Admin routes (require admin/owner role)
			r.Use(s.authService.RequireAdminOrOwnerRole)
			r.Use(s.authService.AddAccountIDToLogContext)

			//dashboard
			r.Get("/admin", handlerService.IsnAdminDashboardPage)

			// user management
			r.Get("/admin/users/generate-password-reset-link", handlerService.GeneratePasswordResetLinkPage)
			r.Put("/ui-api/users/generate-password-reset-link", handlerService.GeneratePasswordResetLink)

			//isn creation
			r.Get("/admin/isn/create", handlerService.CreateIsnPage)
			r.Post("/ui-api/isn/create", handlerService.CreateIsn)

			// service accounts
			r.Get("/admin/service-accounts/create", handlerService.CreateServiceAccountsPage)
			r.Get("/admin/service-accounts/reissue-credentials", handlerService.ReissueServiceAccountCredentialsPage)
			r.Post("/ui-api/service-accounts/create", handlerService.CreateServiceAccount)
			r.Put("/ui-api/service-accounts/reissue-credentials", handlerService.ReissueServiceAccountCredentials)

			r.Group(func(r chi.Router) {
				// the below features are only relevant to admins that have created one or more ISN
				r.Use(s.authService.RequireIsnAdmin)

				// signal types
				r.Get("/admin/signal-types/create", handlerService.CreateSignalTypePage)
				r.Get("/admin/signal-types/register-new-schema", handlerService.RegisterNewSignalTypeSchemaPage)
				r.Post("/ui-api/signal-types/create", handlerService.CreateSignalType)
				r.Put("/ui-api/signal-types/register-new-schema", handlerService.RegisterNewSignalTypeSchema)

				// isn account permissions
				r.Get("/admin/isn/accounts/update", handlerService.UpdateIsnAccountsPage)
				r.Put("/ui-api/isn/accounts/update", handlerService.UpdateIsnAccounts)
			})
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
