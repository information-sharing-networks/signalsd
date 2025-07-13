package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	signalsd "github.com/information-sharing-networks/signalsd/app"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"

	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type Server struct {
	Pool         *pgxpool.Pool
	Queries      *database.Queries
	AuthService  *auth.AuthService
	ServerConfig *signalsd.ServerConfig
	CORSConfigs  *signalsd.CORSConfigs
	ServerLogger *zerolog.Logger
	HTTPLogger   *zerolog.Logger
	Router       *chi.Mux
}

func NewServer(s *Server) *Server {

	s.setupMiddleware()

	s.registerCommonRoutes()

	switch s.ServerConfig.ServiceMode {
	case "all":
		s.registerAdminRoutes()
		s.registerSignalReadRoutes()
		s.registerSignalWriteRoutes()
		s.registerStaticAssetRoutes()
	case "admin":
		s.registerAdminRoutes()
		s.registerStaticAssetRoutes()
	case "signals":
		s.registerSignalReadRoutes()
		s.registerSignalWriteRoutes()
	case "signals-read":
		s.registerSignalReadRoutes()
	case "signals-write":
		s.registerSignalWriteRoutes()
	}
	return s
}

func (s *Server) Run() {
	serverAddr := fmt.Sprintf("%s:%d", s.ServerConfig.Host, s.ServerConfig.Port)

	httpServer := &http.Server{
		Addr:         serverAddr,
		Handler:      s.Router,
		ReadTimeout:  s.ServerConfig.ReadTimeout,
		WriteTimeout: s.ServerConfig.WriteTimeout,
		IdleTimeout:  s.ServerConfig.IdleTimeout,
	}

	defer func() {
		s.ServerLogger.Info().Msg("closing database connections")
		s.Pool.Close()
		s.ServerLogger.Info().Msg("database connection closed")
	}()

	go func() {
		s.ServerLogger.Info().Msgf("%s service listening on %s \n", s.ServerConfig.Environment, serverAddr)

		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			s.ServerLogger.Fatal().Err(err).Msg("Server failed to start")
		}
	}()

	idleConnsClosed := make(chan struct{}, 1)

	sigint := make(chan os.Signal, 1)

	signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

	<-sigint

	s.ServerLogger.Info().Msg("service shutting down")

	// force an exit if server does not shutdown within configured timeout
	ctx, cancel := context.WithTimeout(context.Background(), signalsd.ServerShutdownTimeout)

	// if the server shutsdown in under 10 seconds, exit immediately
	defer cancel()

	err := httpServer.Shutdown(ctx)
	if err != nil {
		s.ServerLogger.Warn().Msgf("shutdown error: %v", err)
	}

	close(idleConnsClosed)

	<-idleConnsClosed
}

// setupMiddleware sets up the middleware that applies to all server requests
// note that the payload size limit is set on a per-route basis (see registerRoutes)
func (s *Server) setupMiddleware() {
	s.Router.Use(chimiddleware.Recoverer)
	s.Router.Use(chimiddleware.RequestID)
	s.Router.Use(logger.RequestLogging(s.HTTPLogger))
	s.Router.Use(chimiddleware.StripSlashes)
	s.Router.Use(SecurityHeaders(s.ServerConfig.Environment))
	s.Router.Use(RateLimit(s.ServerConfig.RateLimitRPS, s.ServerConfig.RateLimitBurst))
}

func (s *Server) registerAdminRoutes() {
	// user registration and authentication handlers
	users := handlers.NewUserHandler(s.Queries, s.AuthService, s.Pool)
	serviceAccounts := handlers.NewServiceAccountHandler(s.Queries, s.AuthService, s.Pool)
	login := handlers.NewLoginHandler(s.Queries, s.AuthService, s.ServerConfig.Environment)
	tokens := handlers.NewTokenHandler(s.Queries, s.AuthService, s.Pool, s.ServerConfig.Environment)

	// site admin handlers
	admin := handlers.NewAdminHandler(s.Queries, s.Pool, s.AuthService)

	// isn definition handlers
	isn := handlers.NewIsnHandler(s.Queries, s.Pool)
	signalTypes := handlers.NewSignalTypeHandler(s.Queries)

	// isn permissions
	isnAccount := handlers.NewIsnAccountHandler(s.Queries)

	// signal batches
	signalBatches := handlers.NewSignalsBatchHandler(s.Queries)

	s.Router.Group(func(r chi.Router) {
		r.Use(CORS(s.CORSConfigs.Protected))
		r.Use(RequestSizeLimit(s.ServerConfig.MaxAPIRequestSize))

		//oauth2.0 token handling
		r.Route("/oauth", func(r chi.Router) {

			r.Group(func(r chi.Router) {
				// the RequireOAuthGrantType middleware calls the appropriate authentication middleware for the grant_type (client_credentials or refresh_token)
				r.Use(s.AuthService.RequireOAuthGrantType)

				// get new access tokens
				r.Post("/token", tokens.NewAccessTokenHandler)
			})

			r.Group(func(r chi.Router) {

				// the RequireValidAccountTypeCredentials middleware calls the appropriate authentication middleware for the user account type (user or service_account)
				r.Use(s.AuthService.RequireValidAccountTypeCredentials)

				// revoke a client secret (service accounts) or refresh token (web users)
				r.Post("/revoke", tokens.RevokeTokenHandler)
			})
		})

		// api routes used to administer the ISNs (excluding signal ingestion)
		r.Route("/api", func(r chi.Router) {

			// auth endpoints
			r.Route("/auth", func(r chi.Router) {

				r.Group(func(r chi.Router) {
					r.Use(s.AuthService.RequireValidAccessToken(false))

					r.Put("/password/reset", users.UpdatePasswordHandler)
				})

				r.Group(func(r chi.Router) {
					r.Use(s.AuthService.RequireValidAccessToken(false))
					r.Use(s.AuthService.RequireRole("owner", "admin"))

					r.Post("/register/service-accounts", serviceAccounts.RegisterServiceAccountHandler)
				})

				r.Group(func(r chi.Router) {
					r.Use(s.AuthService.RequireValidClientCredentials)

					r.Post("/service-accounts/rotate-secret", tokens.RotateServiceAccountSecretHandler)
				})

				r.Post("/register", users.RegisterUserHandler)
				r.Post("/login", login.LoginHandler)
				r.Get("/service-accounts/setup/{setup_id}", serviceAccounts.SetupServiceAccountHandler)
			})

			// isn admin endpoints
			r.Route("/isn", func(r chi.Router) {

				r.Group(func(r chi.Router) {

					r.Use(s.AuthService.RequireValidAccessToken(false))

					// ISN configuration
					r.Group(func(r chi.Router) {

						// Accounts must be eiter owner or admin to use these endponts
						r.Use(s.AuthService.RequireRole("owner", "admin"))

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
						r.Use(s.AuthService.RequireIsnPermission("write"))

						r.Post("/{isn_slug}/batches", signalBatches.CreateSignalsBatchHandler)
						r.Get("/{isn_slug}/batches/{batch_id}/status", signalBatches.GetSignalBatchStatusHandler)
						r.Get("/{isn_slug}/batches/search", signalBatches.SearchBatchesHandler)
					})

					// unrestricted
					r.Get("/", isn.GetIsnsHandler)
					r.Get("/{isn_slug}", isn.GetIsnHandler)
					r.Get("/{isn_slug}/signal_types", signalTypes.GetSignalTypesHandler)
					r.Get("/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}", signalTypes.GetSignalTypeHandler)
				})

			})
		})

		// Site Admin
		r.Route("/admin", func(r chi.Router) {
			r.Group(func(r chi.Router) {

				// route below only works in dev
				r.Use(s.AuthService.RequireDevEnv)

				// delete all users and content
				r.Post("/reset", admin.ResetHandler)
			})

			r.Group(func(r chi.Router) {

				r.Use(s.AuthService.RequireValidAccessToken(false))
				r.Use(s.AuthService.RequireRole("owner"))

				// routes below can only be used by the owner as they expose the email addresses of all users on the site
				r.Get("/users/{id}", admin.GetUserHandler)
				r.Get("/users", admin.GetUsersHandler)

				// Admin role management
				r.Put("/accounts/{account_id}/admin-role", users.GrantUserAdminRoleHandler)
				r.Delete("/accounts/{account_id}/admin-role", users.RevokeUserAdminRoleHandler)

				// ISN ownership transfer
				r.Put("/isn/{isn_slug}/transfer-ownership", isn.TransferIsnOwnershipHandler)
			})

			r.Group(func(r chi.Router) {

				// routes below can only be used by owners and admins
				r.Use(s.AuthService.RequireValidAccessToken(false))
				r.Use(s.AuthService.RequireRole("owner", "admin"))

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
	webhooks := handlers.NewWebhookHandler(s.Queries)
	signals := handlers.NewSignalsHandler(s.Queries, s.Pool)

	s.Router.Group(func(r chi.Router) {
		r.Use(CORS(s.CORSConfigs.Protected))
		r.Use(RequestSizeLimit(s.ServerConfig.MaxSignalPayloadSize))
		r.Use(s.AuthService.RequireValidAccessToken(false))
		r.Use(s.AuthService.RequireIsnPermission("write"))

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
	signals := handlers.NewSignalsHandler(s.Queries, s.Pool)

	// Public ISN signal search - no authentication required
	s.Router.Group(func(r chi.Router) {
		r.Use(CORS(s.CORSConfigs.Public))
		r.Get("/api/public/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search", signals.SearchPublicSignalsHandler)
	})

	// Private ISN signal search - authentication required
	s.Router.Group(func(r chi.Router) {
		r.Use(CORS(s.CORSConfigs.Protected))
		r.Use(s.AuthService.RequireValidAccessToken(false))
		r.Use(s.AuthService.RequireIsnPermission("read", "write"))
		r.Get("/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search", signals.SearchPrivateSignalsHandler)
	})

}

// registerCommonRoutes registers routes that are always available regardless of service mode
// These routes include health checks and version information for monitoring and debugging
func (s *Server) registerCommonRoutes() {
	admin := handlers.NewAdminHandler(s.Queries, s.Pool, s.AuthService)

	// Health check endpoints
	s.Router.Route("/health", func(r chi.Router) {
		r.Use(CORS(s.CORSConfigs.Public))

		// Readiness - checks if the service is ready to accept traffic (includes database connectivity)
		r.Get("/ready", admin.ReadinessHandler)

		// Liveness - checks if the service is running (basic health check)
		r.Get("/live", admin.LivenessHandler)
	})

	// Version information
	s.Router.Route("/version", func(r chi.Router) {
		r.Use(CORS(s.CORSConfigs.Public))
		r.Get("/", admin.VersionHandler)
	})
}

// registerStaticAssetRoutes serves public static assets and API documentation
func (s *Server) registerStaticAssetRoutes() {
	// Static file server for assets (CSS, JS, images, etc.)
	s.Router.Route("/assets", func(r chi.Router) {
		fs := http.FileServer(http.Dir("assets"))
		r.Get("/*", http.StripPrefix("/assets/", fs).ServeHTTP)
	})

	// API documentation and landing pages
	s.Router.Route("/", func(r chi.Router) {
		// Main landing page
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./assets/index.html")
		})

		// API documentation (ReDoc)
		r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./docs/redoc.html")
		})

		// OpenAPI specification for API clients and tools
		r.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, "./docs/swagger.json")
		})
	})
}
