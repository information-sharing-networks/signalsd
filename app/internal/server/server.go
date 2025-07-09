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
	pool         *pgxpool.Pool
	queries      *database.Queries
	authService  *auth.AuthService
	serverConfig *signalsd.ServerConfig
	corsConfigs  *signalsd.CORSConfigs
	serverLogger *zerolog.Logger
	httpLogger   *zerolog.Logger
	router       *chi.Mux
}

func NewServer(pool *pgxpool.Pool, queries *database.Queries, authService *auth.AuthService, serverConfig *signalsd.ServerConfig, corsConfigs *signalsd.CORSConfigs, serverLogger *zerolog.Logger, httpLogger *zerolog.Logger, router *chi.Mux) *Server {
	s := &Server{
		pool:         pool,
		queries:      queries,
		authService:  authService,
		serverConfig: serverConfig,
		corsConfigs:  corsConfigs,
		serverLogger: serverLogger,
		httpLogger:   httpLogger,
		router:       router,
	}

	s.setupMiddleware()

	s.registerCommonRoutes()

	switch s.serverConfig.ServiceMode {
	case "all":
		s.registerAdminRoutes()
		s.registerSignalRoutes()
		s.registerStaticAssetRoutes()
	case "admin":
		s.registerAdminRoutes()
		s.registerStaticAssetRoutes()
	case "signals", "signals-read", "signals-write":
		s.registerSignalRoutes()
	}
	return s
}

func (s *Server) Run() {
	serverAddr := fmt.Sprintf("%s:%d", s.serverConfig.Host, s.serverConfig.Port)

	httpServer := &http.Server{
		Addr:         serverAddr,
		Handler:      s.router,
		ReadTimeout:  s.serverConfig.ReadTimeout,
		WriteTimeout: s.serverConfig.WriteTimeout,
		IdleTimeout:  s.serverConfig.IdleTimeout,
	}

	defer func() {
		s.serverLogger.Info().Msg("closing database connections")
		s.pool.Close()
		s.serverLogger.Info().Msg("database connection closed")
	}()

	go func() {
		s.serverLogger.Info().Msgf("%s service listening on %s \n", s.serverConfig.Environment, serverAddr)

		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			s.serverLogger.Fatal().Err(err).Msg("Server failed to start")
		}
	}()

	idleConnsClosed := make(chan struct{}, 1)

	sigint := make(chan os.Signal, 1)

	signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)

	<-sigint

	s.serverLogger.Info().Msg("service shutting down")

	// force an exit if server does not shutdown within configured timeout
	ctx, cancel := context.WithTimeout(context.Background(), signalsd.ServerShutdownTimeout)

	// if the server shutsdown in under 10 seconds, exit immediately
	defer cancel()

	err := httpServer.Shutdown(ctx)
	if err != nil {
		s.serverLogger.Warn().Msgf("shutdown error: %v", err)
	}

	close(idleConnsClosed)

	<-idleConnsClosed
}

// setupMiddleware sets up the middleware that applies to all server requests
// note that the payload size limit is set on a per-route basis (see registerRoutes)
func (s *Server) setupMiddleware() {
	s.router.Use(chimiddleware.RequestID)
	s.router.Use(logger.RequestLogging(s.httpLogger))
	s.router.Use(SecurityHeaders(s.serverConfig.Environment))
	s.router.Use(RateLimit(s.serverConfig.RateLimitRPS, s.serverConfig.RateLimitBurst))
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

	s.router.Group(func(r chi.Router) {
		r.Use(CORS(s.corsConfigs.Protected))
		r.Use(RequestSizeLimit(s.serverConfig.MaxAPIRequestSize))

		//oauth2.0 token handling
		r.Route("/oauth", func(r chi.Router) {

			r.Group(func(r chi.Router) {
				// the RequireOAuthGrantType middleware calls the appropriate authentication middleware for the grant_type (client_credentials or refresh_token)
				r.Use(s.authService.RequireOAuthGrantType)

				// get new access tokens
				r.Post("/token", tokens.NewAccessTokenHandler)
			})

			r.Group(func(r chi.Router) {

				// the RequireValidAccountTypeCredentials middleware calls the appropriate authentication middleware for the user account type (user or service_account)
				r.Use(s.authService.RequireValidAccountTypeCredentials)

				// revoke a client secret (service accounts) or refresh token (web users)
				r.Post("/revoke", tokens.RevokeTokenHandler)
			})
		})

		// api routes used to administer the ISNs (excluding signal ingestion)
		r.Route("/api", func(r chi.Router) {

			// auth endpoints
			r.Route("/auth", func(r chi.Router) {

				r.Group(func(r chi.Router) {
					r.Use(s.authService.RequireValidAccessToken(false))

					r.Put("/password/reset", users.UpdatePasswordHandler)
				})

				r.Group(func(r chi.Router) {
					r.Use(s.authService.RequireValidAccessToken(false))
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

					// request using the routes below must have a valid access token.
					// this middleware adds the access token claims and user in the Context supplied to the handlers)
					r.Use(s.authService.RequireValidAccessToken(false))

					// ISN configuration
					r.Group(func(r chi.Router) {

						// Accounts must be eiter owner or admin to use these endponts
						r.Use(s.authService.RequireRole("owner", "admin"))

						// ISN management
						r.Post("/", isn.CreateIsnHandler)
						r.Put("/i/{isn_slug}", isn.UpdateIsnHandler)

						// signal types managment
						r.Post("/{isn_slug}/signal_types", signalTypes.CreateSignalTypeHandler)
						r.Put("/{isn_slug}/signal_types/{slug}/v{sem_ver}", signalTypes.UpdateSignalTypeHandler)
						r.Delete("/{isn_slug}/signal_types/{slug}/v{sem_ver}", signalTypes.DeleteSignalTypeHandler)

						// ISN account permissions
						r.Put("/{isn_slug}/accounts/{account_id}", isnAccount.GrantIsnAccountHandler)
						r.Delete("/{isn_slug}/accounts/{account_id}", isnAccount.RevokeIsnAccountHandler)
						r.Get("/{isn_slug}/accounts", isnAccount.GetIsnAccountsHandler)

					})

					// create new signal batches
					r.Group(func(r chi.Router) {
						// accounts must have write permission to the isn to create or read batches
						r.Use(s.authService.RequireValidAccessToken(false))
						r.Use(s.authService.RequireIsnPermission("write"))

						r.Post("/{isn_slug}/batches", signalBatches.CreateSignalsBatchHandler)
						r.Get("/{isn_slug}/batches/{batch_id}/status", signalBatches.GetSignalBatchStatusHandler)
						r.Get("/{isn_slug}/batches/search", signalBatches.SearchBatchesHandler)
					})

					// unrestricted
					r.Get("/", isn.GetIsnsHandler)
					r.Get("/{isn_slug}", isn.GetIsnHandler)
					r.Get("/{isn_slug}/signal_types", signalTypes.GetSignalTypesHandler)
					r.Get("/{isn_slug}/signal_types/{slug}/v{sem_ver}", signalTypes.GetSignalTypeHandler)
				})

			})
		})

		// Site Admin
		r.Route("/admin", func(r chi.Router) {
			r.Group(func(r chi.Router) {

				// route below only works in dev
				r.Use(s.authService.RequireDevEnv)

				// delete all users and content
				r.Post("/reset", admin.ResetHandler)
			})

			r.Group(func(r chi.Router) {

				// route below can only be used by the owner as it exposes the email addresses of all users on the site
				r.Use(s.authService.RequireValidAccessToken(false))
				r.Use(s.authService.RequireRole("owner"))

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
				r.Use(s.authService.RequireValidAccessToken(false))
				r.Use(s.authService.RequireRole("owner", "admin"))

				r.Post("/accounts/{account_id}/disable", admin.DisableAccountHandler)
				r.Post("/accounts/{account_id}/enable", admin.EnableAccountHandler)
				r.Get("/service-accounts", admin.GetServiceAccountsHandler)
				r.Get("/service-accounts/{id}", admin.GetServiceAccountHandler)
				r.Put("/users/{user_id}/reset-password", admin.ResetUserPasswordHandler)
			})
		})
	})
}

// registerSignalRoutes registers signal routes based on service mode
func (s *Server) registerSignalRoutes() {
	// Initialize handlers once
	webhooks := handlers.NewWebhookHandler(s.queries)
	signals := handlers.NewSignalsHandler(s.queries, s.pool)

	// Register write routes - these are always protected endpoints
	if s.serverConfig.ServiceMode == "all" || s.serverConfig.ServiceMode == "signals" || s.serverConfig.ServiceMode == "signals-write" {
		s.router.Group(func(r chi.Router) {
			r.Use(CORS(s.corsConfigs.Protected))
			r.Use(RequestSizeLimit(s.serverConfig.MaxSignalPayloadSize))
			r.Use(s.authService.RequireValidAccessToken(false))

			r.Group(func(r chi.Router) {

				// routes below can only be used by accounts with write permissions to the specified ISN
				r.Use(s.authService.RequireIsnPermission("write"))

				// signals post
				r.Post("/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals", signals.CreateSignalsHandler)

				// signal withdrawal
				r.Put("/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/withdraw", signals.WithdrawSignalHandler)

				// webhooks - todo
				r.Post("/api/webhooks", webhooks.HandlerWebhooks)
			})
		})
	}

	if s.serverConfig.ServiceMode == "all" || s.serverConfig.ServiceMode == "signals" || s.serverConfig.ServiceMode == "signals-read" {

		// Public ISN signal search - no authentication required
		s.router.Group(func(r chi.Router) {
			r.Use(CORS(s.corsConfigs.Public))
			r.Get("/api/public/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search", signals.SearchPublicSignalsHandler)
		})

		// Private ISN signal search - authentication required
		s.router.Group(func(r chi.Router) {
			r.Use(CORS(s.corsConfigs.Protected))
			r.Use(s.authService.RequireValidAccessToken(false))
			r.Use(s.authService.RequireIsnPermission("read", "write"))
			r.Get("/api/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search", signals.SearchPrivateSignalsHandler)
		})

	}
}

// registerCommonRoutes registers routes that are always available (e.g health checks)
func (s *Server) registerCommonRoutes() {
	admin := handlers.NewAdminHandler(s.queries, s.pool, s.authService)

	s.router.Route("/health", func(r chi.Router) {
		r.Use(CORS(s.corsConfigs.Public))

		// check the site is up and the database is accepting requests
		r.Get("/ready", admin.ReadinessHandler)

		// check the site is up
		r.Get("/live", admin.LivenessHandler)
	})

	s.router.Group(func(r chi.Router) {
		r.Use(CORS(s.corsConfigs.Public))
		r.Get("/version", admin.VersionHandler)
	})
}

// registerStaticAssetRoutes static assets e.g documentation for the API
func (s *Server) registerStaticAssetRoutes() {
	s.router.Route("/assets", func(r chi.Router) {
		r.Use(CORS(s.corsConfigs.Public))
		fs := http.FileServer(http.Dir("assets"))
		r.Get("/*", http.StripPrefix("/assets/", fs).ServeHTTP)
	})

	s.router.Group(func(r chi.Router) {
		r.Use(CORS(s.corsConfigs.Public))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./assets/index.html") })
		r.Get("/docs", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/redoc.html") })
		r.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/swagger.json") })
	})
}
