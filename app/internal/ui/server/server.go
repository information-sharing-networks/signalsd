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
	apiClient   *client.Client
}

// NewStandaloneServer creates a UI server that can run independently of the API
func NewStandaloneServer(cfg *config.Config, logger *slog.Logger) *Server {
	s := &Server{
		router:      chi.NewRouter(),
		config:      cfg,
		logger:      logger,
		authService: auth.NewAuthService(cfg.APIBaseURL, cfg.Environment),
		apiClient:   client.NewClient(cfg.APIBaseURL),
	}

	s.setupMiddleware()
	s.RegisterRoutes()
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
		apiClient:   client.NewClient(cfg.APIBaseURL),
	}

	s.RegisterRoutes()
	return s
}

// RegisterRoutes registers UI routes on the server's router.
// note: dynamic HMTX handlers are registered with routes starting /ui-api
func (s *Server) RegisterRoutes() {
	// Public routes (no auth required)
	s.router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./web/static/"))))
	s.router.Get("/login", s.LoginPage)
	s.router.Post("/login", s.Login)
	s.router.Post("/logout", s.Logout)
	s.router.Get("/register", s.RegisterPage)
	s.router.Post("/register", s.Register)

	// redirects to dashboard if authenticated, login if not
	s.router.Get("/", s.HomePage)

	// Protected routes - require valid access token
	s.router.Group(func(r chi.Router) {
		r.Use(s.authService.RequireAuth)
		r.Use(s.authService.AddAccountIDToLogContext)

		// entry point after login
		r.Get("/dashboard", s.DashboardPage)

		// account settings
		r.Get("/settings", s.SettingsPage)
		r.Put("/ui-api/account/password", s.UpdatePassword)

		// auth
		r.Get("/access-denied", s.AccessDeniedPage)

		// HTMX endpoints for cascading signal type dropdowns
		r.Get("/ui-api/options/signal-type-slugs", s.APISignalTypeSlugs)
		r.Get("/ui-api/options/signal-type-versions", s.APISignalTypeVersions)
		r.Get("/ui-api/options/isn/signal-type-slugs", s.TokenSignalTypeSlugs)
		r.Get("/ui-api/options/isn/signal-type-versions", s.TokenSignalTypeVersions)

		// checkbox toggles
		r.Get("/ui-api/toggles/skip-validation", s.ToggleSkipValidation)
		r.Get("/ui-api/toggles/skip-readme", s.ToggleSkipReadme)

		// these routes are only relevant where accounts have been granted access to one or more ISNs
		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireIsnAccess)

			// search signals
			r.Get("/search", s.SearchSignalsPage)
			r.Get("/ui-api/signals/search", s.SearchSignals)
			r.Get("/ui-api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/{signal_id}/correlated-count/{count}", s.GetLatestCorrelatedSignals)
		})

		// admin routes (isnadmin or siteadmin)
		r.Group(func(r chi.Router) {

			r.Use(s.authService.RequireRole("isnadmin", "siteadmin"))
			r.Use(s.authService.AddAccountIDToLogContext)

			//dashboard
			r.Get("/admin", s.IsnAdminDashboardPage)

			// user management
			r.Get("/admin/users/generate-password-reset-link", s.GeneratePasswordResetLinkPage)
			r.Put("/ui-api/users/generate-password-reset-link", s.GeneratePasswordResetLink)

			//isn creation
			r.Get("/admin/isn/create", s.CreateIsnPage)
			r.Post("/ui-api/isn/create", s.CreateIsn)

			// ISN status management (enable/disable ISNs)
			r.Get("/admin/isn/manage", s.ManageIsnStatusPage)
			r.Put("/ui-api/isn/manage", s.ManageIsnStatus)

			// service accounts
			r.Get("/admin/service-accounts/create", s.CreateServiceAccountsPage)
			r.Post("/ui-api/service-accounts/create", s.CreateServiceAccount)

			r.Get("/admin/service-accounts/reissue-credentials", s.ReissueServiceAccountCredentialsPage)
			r.Put("/ui-api/service-accounts/reissue-credentials", s.ReissueServiceAccountCredentials)

			// Account management (enable/disable accounts)
			r.Get("/admin/accounts/manage", s.ManageAccountStatusPage)
			r.Put("/ui-api/accounts/manage", s.ManageAccountStatus)
		})

		// isn admin routes
		r.Group(func(r chi.Router) {
			// the below features are only relevant to admins that have created one or more ISN
			r.Use(s.authService.RequireIsnAdmin)

			r.Get("/admin/signal-types/list", s.ListSignalTypesPage)

			// isn configuration
			r.Get("/admin/isn/accounts/manage", s.ManageIsnAccountsPage)
			r.Put("/ui-api/isn/accounts/manage", s.ManageIsnAccounts)

			r.Get("/admin/isn/signal-types/add", s.AddSignalTypeToIsnPage)
			r.Post("/ui-api/isn/signal-types/add", s.AddSignalTypeToIsn)

			r.Get("/admin/isn/signal-types/manage", s.ManageIsnSignalTypesStatusPage)
			r.Put("/ui-api/isn/signal-types/manage", s.ManageIsnSignalTypesStatus)
		})

		// site admin only
		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireRole("siteadmin"))

			// ISN ownership transfer
			r.Get("/admin/isn/transfer-ownership", s.TransferOwnershipPage)
			r.Put("/ui-api/isn/transfer-ownership", s.TransferOwnership)

			// ISN Admin role management
			r.Get("/admin/accounts/isn-admins/manage", s.ManageIsnAdminRolesPage)
			r.Put("/ui-api/accounts/isn-admins/manage", s.ManageAdminRoles)

			// Site Admin role management
			r.Get("/admin/accounts/site-admins/manage", s.MangeSiteAdminRolesPage)
			r.Put("/ui-api/accounts/site-admins/manage", s.ManageSiteAdminRoles)

			// signal types
			r.Get("/admin/signal-types/create", s.CreateSignalTypePage)
			r.Post("/ui-api/signal-types/create", s.CreateSignalType)

			r.Get("/admin/signal-types/register-new-schema", s.RegisterNewSignalTypeSchemaPage)
			r.Put("/ui-api/signal-types/register-new-schema", s.RegisterNewSignalTypeSchema)

			// Signals Routing Rules
			r.Get("/admin/signal-types/routing", s.ManageSignalRoutingPage)
			r.Get("/ui-api/signal-types/routing", s.LoadSignalRoutingForm)
			r.Put("/ui-api/signal-types/routing", s.SaveSignalRoutingConfig)
			r.Delete("/ui-api/signal-types/routing", s.DeleteSignalRoutingConfig)
			// table management in routing rules page
			r.Get("/ui-api/signal-types/routing/add-row", s.AddRoutingRow)
			r.Get("/ui-api/signal-types/routing/remove-row", s.RemoveRoutingRow)
		})

	})
}

// setupMiddleware creates the routes when running the ui in standalone mode.
func (s *Server) setupMiddleware() {
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
