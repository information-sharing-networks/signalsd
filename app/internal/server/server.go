package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	pool          *pgxpool.Pool
	queries       *database.Queries
	authService   *auth.AuthService
	serviceConfig *signalsd.ServerConfig
	serverLogger  *zerolog.Logger
	httpLogger    *zerolog.Logger
	router        *chi.Mux
}

func NewServer(pool *pgxpool.Pool, queries *database.Queries, authService *auth.AuthService, serviceConfig *signalsd.ServerConfig, serverLogger *zerolog.Logger, httpLogger *zerolog.Logger, router *chi.Mux) *Server {
	s := &Server{
		pool:          pool,
		queries:       queries,
		authService:   authService,
		serviceConfig: serviceConfig,
		serverLogger:  serverLogger,
		httpLogger:    httpLogger,
		router:        router,
	}
	s.setupMiddleware()
	s.registerRoutes()
	return s
}

func (s *Server) Run() {
	serverAddr := fmt.Sprintf("%s:%d", s.serviceConfig.Host, s.serviceConfig.Port)

	httpServer := &http.Server{
		Addr:         serverAddr,
		Handler:      s.router,
		ReadTimeout:  s.serviceConfig.ReadTimeout,
		WriteTimeout: s.serviceConfig.WriteTimeout,
		IdleTimeout:  s.serviceConfig.WriteTimeout,
	}

	defer func() {
		s.serverLogger.Info().Msg("closing database connections")
		s.pool.Close()
		s.serverLogger.Info().Msg("database connection closed")
	}()

	go func() {
		s.serverLogger.Info().Msgf("%s service listening on %s \n", s.serviceConfig.Environment, serverAddr)

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

	// force an exit if server does not shutdown within 10 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	// if the server shutsdown in under 10 seconds, exit immediately
	defer cancel()

	err := httpServer.Shutdown(ctx)
	if err != nil {
		s.serverLogger.Warn().Msgf("shutdown error: %v", err)
	}

	close(idleConnsClosed)

	<-idleConnsClosed
}

func (s *Server) setupMiddleware() {
	s.router.Use(chimiddleware.RequestID)
	s.router.Use(logger.RequestLogging(s.httpLogger))
	s.router.Use(CORS(s.serviceConfig.AllowedOrigins))
	s.router.Use(SecurityHeaders(s.serviceConfig.Environment))
}

func (s *Server) registerRoutes() {

	// user registration and authentication handlers
	users := handlers.NewUserHandler(s.queries, s.authService, s.pool) // handlers that use database transactions need the DB pool struct
	serviceAccounts := handlers.NewServiceAccountHandler(s.queries, s.authService, s.pool)
	login := handlers.NewLoginHandler(s.queries, s.authService, s.serviceConfig.Environment)
	tokens := handlers.NewTokenHandler(s.queries, s.authService, s.serviceConfig.Environment)

	// site admin handlers
	admin := handlers.NewAdminHandler(s.queries, s.pool, s.authService)

	// middleware handles auth on the remaing services

	// isn definition handlers
	isn := handlers.NewIsnHandler(s.queries, s.pool)
	signalTypes := handlers.NewSignalTypeHandler(s.queries)

	// isn permissions
	isnAccount := handlers.NewIsnAccountHandler(s.queries)

	// signald runtime handlers
	webhooks := handlers.NewWebhookHandler(s.queries)
	signalBatches := handlers.NewSignalsBatchHandler(s.queries)
	signals := handlers.NewSignalsHandler(s.queries, s.pool)

	// auth
	s.router.Route("/auth", func(r chi.Router) {

		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireValidAccessToken(false))

			r.Put("/password/reset", users.UpdatePasswordHandler)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireValidAccessToken(false))
			r.Use(s.authService.RequireRole("owner"))

			r.Put("/admins/account/{account_id}", users.GrantUserAdminRoleHandler)
			r.Delete("/admins/account/{account_id}", users.RevokeUserAdminRoleHandler)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireValidAccessToken(false))
			r.Use(s.authService.RequireRole("owner", "admin"))

			r.Post("/register/service-accounts", serviceAccounts.RegisterServiceAccountHandler)
		})

		r.Post("/register", users.RegisterUserHandler)
		r.Post("/login", login.LoginHandler)
		r.Get("/service-accounts/setup/{setup_id}", serviceAccounts.SetupServiceAccountHandler)
	})

	//oauth2.0 token handling
	s.router.Route("/oauth", func(r chi.Router) {

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

	// api routes aused to adminster the ISNs
	s.router.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {

			// request using the routes below must have a valid access token.
			// this middleware adds the access token claims and user in the Context supplied to the handlers)
			r.Use(s.authService.RequireValidAccessToken(false))

			// ISN configuration
			r.Group(func(r chi.Router) {

				// Accounts must be eiter owner or admin to use these endponts
				r.Use(s.authService.RequireRole("owner", "admin"))

				// ISN management
				r.Post("/isn", isn.CreateIsnHandler)
				r.Put("/isn/{isn_slug}", isn.UpdateIsnHandler)

				// signal types managment
				r.Post("/isn/{isn_slug}/signal_types", signalTypes.CreateSignalTypeHandler)
				r.Put("/isn/{isn_slug}/signal_types/{slug}/v{sem_ver}", signalTypes.UpdateSignalTypeHandler)

				// ISN account permissions
				r.Put("/isn/{isn_slug}/accounts/{account_id}", isnAccount.GrantIsnAccountHandler)
				r.Delete("/isn/{isn_slug}/accounts/{account_id}", isnAccount.RevokeIsnAccountHandler)
			})

			// signals exchange
			r.Group(func(r chi.Router) {

				// routes below can only be used by accounts with write permissions to the specified ISN
				r.Use(s.authService.RequireIsnPermission("write"))

				// signals batches
				r.Post("/isn/{isn_slug}/batches", signalBatches.CreateSignalsBatchHandler)

				// signals post
				r.Post("/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals", signals.CreateSignalsHandler)

				// webhooks
				r.Post("/webhooks", webhooks.HandlerWebhooks)
			})

			// search signals
			r.Group(func(r chi.Router) {

				// routes below can only be used by accounts with read or write permissions to the specified ISN
				r.Use(s.authService.RequireIsnPermission("read", "write"))

				r.Get("/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals/search", signals.SearchSignalsHandler)
			})
		})

		// unrestricted
		r.Get("/isn", isn.GetIsnsHandler)
		r.Get("/isn/{isn_slug}", isn.GetIsnHandler)
		r.Get("/isn/{isn_slug}/signal_types", signalTypes.GetSignalTypesHandler)
		r.Get("/isn/{isn_slug}/signal_types/{slug}/v{sem_ver}", signalTypes.GetSignalTypeHandler)
	})

	// Site Admin
	s.router.Route("/admin", func(r chi.Router) {
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

	s.router.Route("/health", func(r chi.Router) {

		// check the site is up and the database is accepting requests
		r.Get("/ready", admin.ReadinessHandler)

		// check the site is up
		r.Get("/live", admin.LivenessHandler)
	})

	s.router.Route("/assets", func(r chi.Router) {
		fs := http.FileServer(http.Dir("assets"))
		r.Get("/*", http.StripPrefix("/assets/", fs).ServeHTTP)
	})

	s.router.Get("/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./assets/index.html") })
	s.router.Get("/docs", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/redoc.html") })
	s.router.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/swagger.json") })
}
