package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
	"github.com/information-sharing-networks/signalsd/app/internal/server/isns"
	"github.com/information-sharing-networks/signalsd/app/internal/server/middleware"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
	"github.com/information-sharing-networks/signalsd/app/internal/ui"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	pool           *pgxpool.Pool
	queries        *database.Queries
	authService    *auth.AuthService
	serverConfig   *signalsd.ServerConfig
	corsConfigs    *signalsd.CORSConfigs
	logger         *slog.Logger
	router         *chi.Mux
	schemaCache    *schemas.SchemaCache
	publicIsnCache *isns.PublicIsnCache
}

func NewServer(
	pool *pgxpool.Pool,
	queries *database.Queries,
	authService *auth.AuthService,
	cfg *signalsd.ServerConfig,
	corsConfigs *signalsd.CORSConfigs,
	logger *slog.Logger,
	schemaCache *schemas.SchemaCache,
	publicIsnCache *isns.PublicIsnCache,
) *Server {
	server := &Server{
		pool:           pool,
		queries:        queries,
		authService:    authService,
		serverConfig:   cfg,
		corsConfigs:    corsConfigs,
		logger:         logger,
		router:         chi.NewRouter(),
		schemaCache:    schemaCache,
		publicIsnCache: publicIsnCache,
	}

	server.setupMiddleware()
	server.registerCommonRoutes()

	switch server.serverConfig.ServiceMode {
	case "all":
		server.registerAdminRoutes()
		server.registerSignalReadRoutes()
		server.registerSignalWriteRoutes()
		server.registerApiDocoRoutes()
		server.setupUIServer()
	case "api":
		server.registerAdminRoutes()
		server.registerSignalReadRoutes()
		server.registerSignalWriteRoutes()
		server.registerApiDocoRoutes()
	case "admin":
		server.registerAdminRoutes()
		server.registerApiDocoRoutes()
	case "signals":
		server.registerSignalReadRoutes()
		server.registerSignalWriteRoutes()
	case "signals-read":
		server.registerSignalReadRoutes()
	case "signals-write":
		server.registerSignalWriteRoutes()
	}
	return server
}

func (s *Server) Start(ctx context.Context) error {
	serverAddr := fmt.Sprintf("%s:%d", s.serverConfig.Host, s.serverConfig.Port)

	httpServer := &http.Server{
		Addr:         serverAddr,
		Handler:      s.router,
		ReadTimeout:  s.serverConfig.ReadTimeout,
		WriteTimeout: s.serverConfig.WriteTimeout,
		IdleTimeout:  s.serverConfig.IdleTimeout,
	}

	serverErrors := make(chan error, 1)

	// Start HTTP server
	go func() {
		s.logger.Info("service listening",
			slog.String("environment", s.serverConfig.Environment),
			slog.String("address", serverAddr))

		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			serverErrors <- fmt.Errorf("server failed to start: %w", err)
		}
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErrors:
		return err
	case <-ctx.Done():
		s.logger.Info("shutdown signal received")
	}

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), signalsd.ServerShutdownTimeout)
	defer shutdownCancel()

	s.logger.Info("shutting down HTTP server")

	err := httpServer.Shutdown(shutdownCtx)
	if err != nil {
		s.logger.Warn("HTTP server shutdown error",
			slog.String("error", err.Error()))
		return fmt.Errorf("HTTP server shutdown failed: %w", err)
	}

	s.logger.Info("HTTP server shutdown complete")
	return nil
}

// DatabaseShutdown gracefully shutsdown the database
func (s *Server) DatabaseShutdown() {
	s.logger.Info("closing database connections")
	s.pool.Close()
	s.logger.Info("database connections closed")
}

// setupMiddleware sets up the middleware that applies to all server requests
// note that the payload size limit is set on a per-route basis (see registerRoutes)
func (s *Server) setupMiddleware() {
	s.router.Use(chimiddleware.Recoverer)
	s.router.Use(chimiddleware.RequestID)
	s.router.Use(logger.RequestLogging(s.logger))
	s.router.Use(chimiddleware.StripSlashes)
	s.router.Use(middleware.SecurityHeaders(s.serverConfig.Environment))
	s.router.Use(middleware.RateLimit(s.serverConfig.RateLimitRPS, s.serverConfig.RateLimitBurst))
}

func (s *Server) registerAdminRoutes() {
	// user registration and authentication handlers
	users := handlers.NewUserHandler(s.queries, s.authService, s.pool)
	serviceAccounts := handlers.NewServiceAccountHandler(s.queries, s.authService, s.pool)
	login := handlers.NewLoginHandler(s.queries, s.authService, s.serverConfig.Environment)
	tokens := handlers.NewTokenHandler(s.queries, s.authService, s.pool, s.serverConfig.Environment)

	// site admin handlers
	admin := handlers.NewAdminHandler(s.queries, s.pool, s.authService)

	// isn definition handlers
	isn := handlers.NewIsnHandler(s.queries, s.pool)
	signalTypes := handlers.NewSignalTypeHandler(s.queries)

	// isn permissions
	isnAccount := handlers.NewIsnAccountHandler(s.queries)

	// signal batches
	signalBatches := handlers.NewSignalsBatchHandler(s.queries)

	// protected routes
	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Protected))
		r.Use(middleware.RequestSizeLimit(s.serverConfig.MaxAPIRequestSize))

		//oauth2.0 token handling
		r.Route("/oauth", func(r chi.Router) {

			r.Group(func(r chi.Router) {
				// select the authentication method based on the suppled the grant_type URL param (client_credentials or refresh_token)
				r.Use(s.authService.AuthenticateByGrantType)

				// get new access tokens
				r.Post("/token", tokens.RefreshAccessTokenHandler)
			})

			r.Group(func(r chi.Router) {

				// select the appropriate authentication method based on the user account type (user or service_account)
				r.Use(s.authService.AuthenticateByCredentalType)

				// revoke a client secret (service accounts) or refresh token (web users)
				r.Post("/revoke", tokens.RevokeTokenHandler)
			})
		})

		// api routes used to administer the ISNs (excluding signal ingestion)
		r.Route("/api", func(r chi.Router) {

			// auth endpoints
			r.Route("/auth", func(r chi.Router) {

				r.Group(func(r chi.Router) {
					r.Use(s.authService.RequireValidAccessToken)

					r.Put("/password/reset", users.UpdatePasswordHandler)
				})

				r.Group(func(r chi.Router) {
					r.Use(s.authService.RequireValidAccessToken)
					r.Use(s.authService.RequireRole("owner", "admin"))

					r.Post("/register/service-accounts", serviceAccounts.RegisterServiceAccountHandler)
				})

				r.Group(func(r chi.Router) {
					r.Use(s.authService.RequireValidClientCredentials)

					r.Post("/service-accounts/rotate-secret", tokens.RotateServiceAccountSecretHandler)
				})

				r.Post("/register", users.RegisterUserHandler)
				r.Post("/login", login.LoginHandler)
				r.Get("/service-accounts/setup/{setup_id}", serviceAccounts.SetupServiceAccountHandler)
			})

			// isn admin endpoints
			r.Route("/isn", func(r chi.Router) {

				r.Group(func(r chi.Router) {

					r.Use(s.authService.RequireValidAccessToken)

					// ISN configuration
					r.Group(func(r chi.Router) {

						// Accounts must be eiter owner or admin to use these endponts
						r.Use(s.authService.RequireRole("owner", "admin"))

						// ISN management
						r.Post("/", isn.CreateIsnHandler)
						r.Put("/{isn_slug}", isn.UpdateIsnHandler)

						// signal types managment
						r.Post("/{isn_slug}/signal_types", signalTypes.CreateSignalTypeHandler)
						r.Put("/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}", signalTypes.UpdateSignalTypeHandler)
						r.Delete("/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}", signalTypes.DeleteSignalTypeHandler)

						// ISN account permissions
						r.Put("/{isn_slug}/accounts/{account_id}", isnAccount.GrantIsnAccountHandler)
						r.Delete("/{isn_slug}/accounts/{account_id}", isnAccount.RevokeIsnAccountHandler)
						r.Get("/{isn_slug}/accounts", isnAccount.GetIsnAccountsHandler)

					})

					// create new signal batches
					r.Group(func(r chi.Router) {
						// accounts must have write permission to the isn to create or read batches
						r.Use(s.authService.RequireIsnPermission("write"))

						r.Post("/{isn_slug}/batches", signalBatches.CreateSignalsBatchHandler)
						r.Get("/{isn_slug}/batches/{batch_id}/status", signalBatches.GetSignalBatchStatusHandler)
						r.Get("/{isn_slug}/batches/search", signalBatches.SearchBatchesHandler)
					})

					// view ISN and signal type details
					r.Get("/", isn.GetIsnsHandler)
					r.Get("/{isn_slug}", isn.GetIsnHandler)
					r.Get("/{isn_slug}/signal_types", signalTypes.GetSignalTypesHandler)
					r.Get("/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}", signalTypes.GetSignalTypeHandler)
				})

			})
		})

		// Site Admin
		r.Route("/api/admin", func(r chi.Router) {
			r.Group(func(r chi.Router) {

				// route below only works in dev - take care! this endpoint deletes all the content in the database
				r.Use(s.authService.RequireDevEnv)

				// delete all users and content
				r.Post("/reset", admin.ResetHandler)
			})

			// Owner-only operations
			r.Group(func(r chi.Router) {

				r.Use(s.authService.RequireValidAccessToken)
				r.Use(s.authService.RequireRole("owner"))

				// Admin role management
				r.Put("/accounts/{account_id}/admin-role", users.GrantUserAdminRoleHandler)
				r.Delete("/accounts/{account_id}/admin-role", users.RevokeUserAdminRoleHandler)

				// ISN ownership transfer (owner only)
				r.Put("/isn/{isn_slug}/transfer-ownership", isn.TransferIsnOwnershipHandler)
			})

			// Admin and owner operations
			r.Group(func(r chi.Router) {

				r.Use(s.authService.RequireValidAccessToken)
				r.Use(s.authService.RequireRole("owner", "admin"))

				// User management
				r.Get("/users", admin.GetUsersHandler)

				// Account management
				r.Post("/accounts/{account_id}/disable", admin.DisableAccountHandler)
				r.Post("/accounts/{account_id}/enable", admin.EnableAccountHandler)
				r.Get("/service-accounts", admin.GetServiceAccountsHandler)
				r.Get("/service-accounts/{id}", admin.GetServiceAccountHandler)
				r.Put("/users/{user_id}/reset-password", admin.ResetUserPasswordHandler)
			})
		})
	})
}

// registerSignalWriteRoutes registers signal write routes
func (s *Server) registerSignalWriteRoutes() {
	webhooks := handlers.NewWebhookHandler(s.queries)
	signals := handlers.NewSignalsHandler(s.queries, s.pool, s.schemaCache, s.publicIsnCache)

	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Protected))
		r.Use(middleware.RequestSizeLimit(s.serverConfig.MaxSignalPayloadSize))
		r.Use(s.authService.RequireValidAccessToken)
		r.Use(s.authService.RequireIsnPermission("write"))

		// signals post
		r.Post("/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals", signals.CreateSignalsHandler)

		// signal withdrawal
		r.Put("/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/withdraw", signals.WithdrawSignalHandler)

		// webhooks - todo
		r.Post("/api/webhooks", webhooks.HandlerWebhooks)
	})
}

// registerSignalReadRoutes registers signal read routes
func (s *Server) registerSignalReadRoutes() {
	signals := handlers.NewSignalsHandler(s.queries, s.pool, s.schemaCache, s.publicIsnCache)

	// Public ISN signal search - no authentication required
	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Public))
		r.Get("/api/public/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search", signals.SearchPublicSignalsHandler)
	})

	// Private ISN signal search - authentication required
	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Protected))
		r.Use(s.authService.RequireValidAccessToken)
		r.Use(s.authService.RequireIsnPermission("read", "write"))
		r.Get("/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search", signals.SearchPrivateSignalsHandler)
	})

}

// registerCommonRoutes registers routes that are always available regardless of service mode
// These routes include health checks and version information
func (s *Server) registerCommonRoutes() {
	admin := handlers.NewAdminHandler(s.queries, s.pool, s.authService)

	// Health check endpoints
	s.router.Route("/health", func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Public))

		// Readiness - checks if the service is ready to accept traffic (includes database connectivity)
		r.Get("/ready", admin.ReadinessHandler)

		// Liveness - checks if the service is running (basic health check)
		r.Get("/live", admin.LivenessHandler)
	})

	// Version information
	s.router.Route("/version", func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Public))
		r.Get("/", admin.VersionHandler)
	})
}

// registerApiDocoRoutes serves public static assets and API documentation
func (s *Server) registerApiDocoRoutes() {
	// Static file server for assets (CSS, JS, images, etc.)
	s.router.Route("/assets", func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Public))

		fs := http.FileServer(http.Dir("assets"))
		r.Get("/*", http.StripPrefix("/assets/", fs).ServeHTTP)
	})

	// API documentation routes
	s.router.Route("/", func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Public))

		// When UI is integrated (mode "all"), the UI handles the root route
		if s.serverConfig.ServiceMode != "all" {
			// Redirect root to API documentation for API-only modes
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/docs", http.StatusMovedPermanently)
			})
		}

		// API documentation
		r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./docs/redoc.html")
		})

		// OpenAPI specification for API clients and tools
		r.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./docs/swagger.json")
		})
	})
}

// setupUIServer configures the UI server and registers web UI routes directly on the signalsd router
func (s *Server) setupUIServer() {
	// Create UI configuration based on signalsd configuration
	uiConfig := &ui.Config{
		Environment:  s.serverConfig.Environment,
		Host:         s.serverConfig.Host,
		Port:         s.serverConfig.Port, // Same port as API
		LogLevel:     s.serverConfig.LogLevel,
		ReadTimeout:  s.serverConfig.ReadTimeout,
		WriteTimeout: s.serverConfig.WriteTimeout,
		IdleTimeout:  s.serverConfig.IdleTimeout,
		APIBaseURL:   fmt.Sprintf("http://localhost:%d", s.serverConfig.Port), // use the API port
	}

	// Create UI server and register its routes on the signalsd router
	uiServer := ui.NewIntegratedServer(s.router, uiConfig, s.logger)
	uiServer.RegisterRoutes(s.router)

	s.logger.Info("UI routes registered")
}
