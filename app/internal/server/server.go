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
	"github.com/information-sharing-networks/signalsd/app/internal/publicisns"
	"github.com/information-sharing-networks/signalsd/app/internal/router"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
	"github.com/information-sharing-networks/signalsd/app/internal/server/middleware"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	uiserver "github.com/information-sharing-networks/signalsd/app/internal/ui/server"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	// pool is the database connection pool
	pool *pgxpool.Pool

	// queries is the sqlc generated database queries
	queries *database.Queries

	// authService provides authentication and authorization services
	authService *auth.AuthService

	// config is the server environment configuration
	config *signalsd.ServerEnvironment

	// corsConfigs is the CORS configuration for the server - configured via the ALLOWED_ORIGINS environment variable
	corsConfigs *signalsd.CORSConfigs

	// logger is the server logger - used for application level logging - http request logging is handled by the RequestLogging middleware
	logger *slog.Logger

	// router is the HTTP router for the server
	router *chi.Mux

	// schemaCache is the JSON schema cache for signal types - used to validate incoming signals
	// refreshed by polling the database every CachePollInterval.
	schemaCache *schemas.Cache

	// publicIsnCache is the cache of public ISNs and their signal types - used by the public signal search endpoint
	// refreshed by polling the database every CachePollInterval.
	publicIsnCache *publicisns.Cache

	// signalRouterCache holds the compiled Signals Routing Rules. It is loaded at startup and
	// refreshed by polling the database every CachePollInterval.
	signalRouterCache *router.Cache
}

func NewServer(
	pool *pgxpool.Pool,
	queries *database.Queries,
	authService *auth.AuthService,
	config *signalsd.ServerEnvironment,
	corsConfigs *signalsd.CORSConfigs,
	logger *slog.Logger,
	schemaCache *schemas.Cache,
	publicIsnCache *publicisns.Cache,
	signalRouterCache *router.Cache,
) *Server {
	server := &Server{
		pool:              pool,
		queries:           queries,
		authService:       authService,
		config:            config,
		corsConfigs:       corsConfigs,
		logger:            logger,
		router:            chi.NewRouter(),
		schemaCache:       schemaCache,
		publicIsnCache:    publicIsnCache,
		signalRouterCache: signalRouterCache,
	}

	server.setupMiddleware()
	server.registerCommonRoutes()

	switch server.config.ServiceMode {
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
	case "isnadmin":
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
	serverAddr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)

	httpServer := &http.Server{
		Addr:         serverAddr,
		Handler:      s.router,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	// Start polling for cache changes.
	s.signalRouterCache.StartPolling(ctx, signalsd.CachePollInterval)
	s.publicIsnCache.StartPolling(ctx, signalsd.CachePollInterval)
	s.schemaCache.StartPolling(ctx, signalsd.CachePollInterval)

	serverErrors := make(chan error, 1)

	// Start HTTP server
	go func() {
		s.logger.Info("service listening",
			slog.String("environment", s.config.Environment),
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
	s.router.Use(middleware.SecurityHeaders(s.config.Environment))
	s.router.Use(middleware.RateLimit(s.config.RateLimitRPS, s.config.RateLimitBurst))
}

func (s *Server) registerAdminRoutes() {
	// user registration and authentication handlers
	users := handlers.NewUserHandler(s.queries, s.authService, s.pool)
	serviceAccounts := handlers.NewServiceAccountHandler(s.queries, s.authService, s.pool, s.config.PublicBaseURL)
	login := handlers.NewLoginHandler(s.queries, s.authService, s.config.Environment)
	tokens := handlers.NewTokenHandler(s.queries, s.authService, s.pool, s.config.Environment)

	// site admin handlers
	admin := handlers.NewAdminHandler(s.queries, s.pool, s.authService, s.config.PublicBaseURL)

	// isn definition handlers
	isn := handlers.NewIsnHandler(s.queries, s.pool, s.config.PublicBaseURL)
	signalTypes := handlers.NewSignalTypeHandler(s.queries)
	isnRouter := handlers.NewRoutingConfigHandler(s.queries, s.pool, s.signalRouterCache, s.schemaCache)

	// isn permissions
	isnAccount := handlers.NewIsnAccountHandler(s.queries)

	// protected routes
	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Protected))
		r.Use(middleware.RequestSizeLimit(s.config.MaxAPIRequestSize))

		//oauth2.0 token handling
		r.Route("/oauth", func(r chi.Router) {

			r.Group(func(r chi.Router) {
				// select the authentication method based on the suppled the grant_type URL param (client_credentials or refresh_token)
				r.Use(s.authService.AuthenticateByGrantType)

				// get new access tokens
				r.Post("/token", tokens.RefreshAccessToken)
			})

			r.Group(func(r chi.Router) {

				// select the appropriate authentication method based on the user account type (user or service_account)
				r.Use(s.authService.AuthenticateByCredentalType)

				// revoke a client secret (service accounts) or refresh token (web users)
				r.Post("/revoke", tokens.RevokeToken)
			})
		})

		// api routes used to administer the ISNs (excluding signal ingestion)
		r.Route("/api", func(r chi.Router) {

			// auth endpoints
			r.Route("/auth", func(r chi.Router) {

				r.Group(func(r chi.Router) {
					r.Use(s.authService.RequireValidAccessToken)

					r.Put("/password/reset", users.UpdatePassword)
				})

				r.Group(func(r chi.Router) {
					r.Use(s.authService.RequireValidAccessToken)
					r.Use(s.authService.RequireRole("siteadmin", "isnadmin"))

					r.Post("/service-accounts/register", serviceAccounts.RegisterServiceAccount)
					r.Post("/service-accounts/reissue-credentials", serviceAccounts.ReissueServiceAccountCredentials)
				})

				r.Group(func(r chi.Router) {
					r.Use(s.authService.RequireValidClientCredentials)

					r.Post("/service-accounts/rotate-secret", tokens.RotateServiceAccountSecret)
				})

				// no authentication required
				r.Post("/register", users.RegisterUser)
				r.Post("/login", login.Login)
				r.Get("/service-accounts/setup/{setup_id}", serviceAccounts.SetupServiceAccount)
				r.Get("/password-reset/{token_id}", users.PasswordResetTokenPage)
				r.Post("/password-reset/{token_id}", users.PasswordResetToken)
			})

			// isn admin endpoints
			r.Route("/isn", func(r chi.Router) {
				r.Use(s.authService.RequireValidAccessToken)

				r.Group(func(r chi.Router) {

					// ISN configuration
					r.Group(func(r chi.Router) {

						// Accounts must be either site admin or isn admin to use these endponts
						r.Use(s.authService.RequireRole("siteadmin", "isnadmin"))

						// ISN management
						r.Post("/", isn.CreateIsn)
						r.Put("/{isn_slug}", isn.UpdateIsn)

						// add signal types to an ISN
						r.Post("/{isn_slug}/signal-types/add", signalTypes.AddSignalTypeToISN)

						// ISN signal type status management
						r.Put("/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}", signalTypes.UpdateIsnSignalTypeStatus)

						// ISN account permissions
						r.Put("/{isn_slug}/accounts/{account_id}", isnAccount.UpdateIsnAccountPermission)
						r.Get("/{isn_slug}/accounts", isnAccount.GetIsnAccounts)

					})

					// view ISN and signal type details
					r.Get("/", isn.GetIsns)
					r.Get("/{isn_slug}", isn.GetIsn)
					r.Get("/{isn_slug}/signal-types", signalTypes.GetSignalTypes)
					r.Get("/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}", signalTypes.GetSignalType)
				})

			})
		})

		// Site Admin
		r.Route("/api/admin", func(r chi.Router) {
			r.Group(func(r chi.Router) {

				// route below only works in dev - take care! this endpoint deletes all the content in the database
				r.Use(s.authService.RequireDevEnv)

				// delete all users and content
				r.Post("/reset", admin.ResetEnv)
			})

			// siteadmin operations
			r.Group(func(r chi.Router) {

				r.Use(s.authService.RequireValidAccessToken)
				r.Use(s.authService.RequireRole("siteadmin"))

				// ISN admin role management
				r.Put("/accounts/{account_id}/isn-admin-role", users.GrantUserIsnAdminRole)
				r.Delete("/accounts/{account_id}/isn-admin-role", users.RevokeUserIsnAdminRole)

				// Site admin role management
				r.Put("/accounts/{account_id}/site-admin-role", users.GrantUserSiteAdminRole)
				r.Delete("/accounts/{account_id}/site-admin-role", users.RevokeUserSiteAdminRole)

				// ISN ownership transfer
				r.Put("/isn/{isn_slug}/transfer-ownership", isn.TransferIsnOwnership)

				// signal types management
				r.Get("/signal-types", signalTypes.GetSignalTypes)
				r.Post("/signal-types", signalTypes.CreateSignalType)
				r.Post("/signal-types/{signal_type_slug}/schemas", signalTypes.RegisterNewSignalTypeSchema)
				r.Put("/signal-types/{signal_type_slug}/v{sem_ver}", signalTypes.UpdateSignalType)
				r.Delete("/signal-types/{signal_type_slug}/v{sem_ver}", signalTypes.DeleteSignalType)

				// Signals Routing Rules management
				r.Get("/signal-types/{signal_type_slug}/v{sem_ver}/routes", isnRouter.GetSignalRoutingConfig)
				r.Put("/signal-types/{signal_type_slug}/v{sem_ver}/routes", isnRouter.UpdateSignalRoutingConfig)
				r.Delete("/signal-types/{signal_type_slug}/v{sem_ver}/routes", isnRouter.DeleteSignalRoutingConfig)
			})

			// shared site-admin and isn-admin operations
			r.Group(func(r chi.Router) {

				r.Use(s.authService.RequireValidAccessToken)
				r.Use(s.authService.RequireRole("siteadmin", "isnadmin"))

				// Account management
				r.Post("/accounts/{account_id}/disable", admin.DisableAccount)
				r.Post("/accounts/{account_id}/enable", admin.EnableAccount)
				r.Get("/users", admin.GetUsers)
				r.Get("/service-accounts", admin.GetServiceAccounts)
				r.Post("/users/{user_id}/generate-password-reset-link", admin.GeneratePasswordResetLink)
			})
		})
	})
}

// registerSignalWriteRoutes registers signal write routes
func (s *Server) registerSignalWriteRoutes() {
	signals := handlers.NewSignalsHandler(s.queries, s.pool, s.schemaCache, s.publicIsnCache)
	signalBatches := handlers.NewSignalsBatchHandler(s.queries)
	routerSignals := handlers.NewSignalRouter(s.queries, s.pool, s.schemaCache, s.signalRouterCache)

	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Protected))
		r.Use(middleware.RequestSizeLimit(s.config.MaxSignalPayloadSize))
		r.Use(s.authService.RequireValidAccessToken)
		r.Use(s.authService.RequireAccessPermission("write"))

		// direct ISN signal post and withdrawal
		r.Post("/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals", signals.CreateSignals)
		r.Put("/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/withdraw", signals.WithdrawSignal)

		// batch status endpoints
		r.Get("/api/batches/search", signalBatches.SearchBatches)
		r.Get("/api/batches/{batch_ref}/status", signalBatches.GetSignalBatchStatus)
	})

	// Router signal endpoint: ISN is resolved by routing rules, not from the URL.
	// RequireAccessPermission("write") is NOT used here - permission is checked in-handler
	// after ISN resolution, via auth.CheckIsnWritePermission.
	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Protected))
		r.Use(middleware.RequestSizeLimit(s.config.MaxSignalPayloadSize))
		r.Use(s.authService.RequireValidAccessToken)
		r.Post("/api/router/signal-types/{signal_type_slug}/v{sem_ver}/signals", routerSignals.RouteSignals)
	})

	// Router signal endpoint: ISN is resolved by routing rules, not from the URL.
	// RequireAccessPermission("write") is NOT used here - permission is checked in-handler
	// after ISN resolution, via auth.CheckIsnWritePermission.
	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Protected))
		r.Use(middleware.RequestSizeLimit(s.config.MaxSignalPayloadSize))
		r.Use(s.authService.RequireValidAccessToken)
		r.Post("/api/router/signal-types/{signal_type_slug}/v{sem_ver}/signals", routerSignals.RouteSignals)
	})
}

// registerSignalReadRoutes registers signal read routes
func (s *Server) registerSignalReadRoutes() {
	signals := handlers.NewSignalsHandler(s.queries, s.pool, s.schemaCache, s.publicIsnCache)

	// Public ISN signal search - no authentication required
	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Public))
		r.Get("/api/public/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search", signals.SearchPublicSignals)
	})

	// Private ISN signal search - any ISN member (read or write) may call this endpoint.
	// Write-only accounts receive only the signals they created; visibility filtering is applied in the handler.
	s.router.Group(func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Protected))
		r.Use(s.authService.RequireValidAccessToken)
		r.Use(s.authService.RequireIsnMembership)
		r.Get("/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search", signals.SearchPrivateSignals)
	})

}

// registerCommonRoutes registers routes that are always available regardless of service mode
// These routes include health checks and version information
func (s *Server) registerCommonRoutes() {
	admin := handlers.NewAdminHandler(s.queries, s.pool, s.authService, s.config.PublicBaseURL)

	// Health check endpoints
	s.router.Route("/health", func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Public))

		// Readiness - checks if the service is ready to accept traffic (includes database connectivity)
		r.Get("/ready", admin.Readiness)

		// Liveness - checks if the service is running (basic health check)
		r.Get("/live", admin.Liveness)
	})

	// Version information
	s.router.Route("/version", func(r chi.Router) {
		r.Use(middleware.CORS(s.corsConfigs.Public))
		r.Get("/", admin.Version)
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
		if s.config.ServiceMode != "all" {
			// Redirect root to API documentation for API-only modes
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/docs", http.StatusMovedPermanently)
			})
		}

		// API documentation
		r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./docs/rapidoc.html")
		})

		// API documentation
		r.Get("/redoc", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./docs/redoc.html")
		})
		// OpenAPI specification for API clients and tools
		r.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./docs/swagger.json")
		})

		// UI api documentation
		r.Get("/ui-docs", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./docs/ui/rapidoc.html")
		})

		// UI OpenAPI specification
		r.Get("/ui-swagger.json", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./docs/ui/swagger.json")
		})
	})
}

// setupUIServer configures the UI server and registers web UI routes directly on the signalsd router
func (s *Server) setupUIServer() {
	// Create UI configuration based on signalsd configuration
	uiConfig := &config.Config{
		Environment:  s.config.Environment,
		Host:         s.config.Host,
		Port:         s.config.Port, // Same port as API
		LogLevel:     s.config.LogLevel,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
		APIBaseURL:   fmt.Sprintf("http://localhost:%d", s.config.Port), // use the API port
	}

	// Create UI server and register its routes on the signalsd router
	uiServer := uiserver.NewIntegratedServer(s.router, uiConfig, s.logger)
	uiServer.RegisterRoutes()

	s.logger.Info("UI routes registered")
}
