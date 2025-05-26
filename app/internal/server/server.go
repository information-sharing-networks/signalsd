package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	signalsd "github.com/nickabs/signalsd/app"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/logger"
	"github.com/nickabs/signalsd/app/internal/server/handlers"
	"github.com/rs/zerolog"
)

type Server struct {
	db           *sql.DB
	queries      *database.Queries
	authService  *auth.AuthService
	serverConfig *signalsd.ServerConfig
	serverLogger *zerolog.Logger
	httpLogger   *zerolog.Logger
	router       *chi.Mux
}

func NewServer(db *sql.DB, queries *database.Queries, authService *auth.AuthService, serviceConfig *signalsd.ServerConfig, serverLogger *zerolog.Logger, httpLogger *zerolog.Logger, router *chi.Mux) *Server {
	s := &Server{
		db:           db,
		queries:      queries,
		authService:  authService,
		serverConfig: serviceConfig,
		serverLogger: serverLogger,
		httpLogger:   httpLogger,
		router:       router,
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {

	// user registration and authentication handlers
	users := handlers.NewUserHandler(s.queries, s.authService, s.db)
	login := handlers.NewLoginHandler(s.queries, s.authService, s.serverConfig.Environment)
	tokens := handlers.NewTokenHandler(s.queries, s.authService, s.serverConfig.Environment)

	// site admin handlers
	admin := handlers.NewAdminHandler(s.queries)

	// middleware handles auth on the remaing services

	// isn definition handlers
	isn := handlers.NewIsnHandler(s.queries)
	signalTypes := handlers.NewSignalTypeHandler(s.queries)

	isnReceivers := handlers.NewIsnReceiverHandler(s.queries)
	isnRetrievers := handlers.NewIsnRetrieverHandler(s.queries)

	// signald runtime handlers
	webhooks := handlers.NewWebhookHandler(s.queries)
	signalBatches := handlers.NewSignalsBatchHandler(s.queries)

	s.router.Use(middleware.RequestID)
	s.router.Use(logger.LoggingMiddleware(s.httpLogger))

	// auth
	s.router.Route("/auth", func(r chi.Router) {
		r.Post("/register", users.RegisterUserHandler)
		r.Post("/login", login.LoginHandler)

		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireValidAccessToken)

			r.Put("/password/reset", users.UpdatePasswordHandler)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.authService.RequireValidRefreshToken)

			r.Post("/token", tokens.RefreshAccessTokenHandler)
			r.Post("/revoke", tokens.RevokeRefreshTokenHandler)
		})

		r.Get("/users", users.GetUsersHandler)
	})

	// api
	s.router.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {

			// validate access token and put claims and user in context
			r.Use(s.authService.RequireValidAccessToken)

			// isn config
			r.Group(func(r chi.Router) {
				r.Use(s.authService.RequireRole("owner", "admin"))

				// ISN management
				r.Post("/isn", isn.CreateIsnHandler)
				r.Put("/isn/{isn_slug}", isn.UpdateIsnHandler)
				r.Post("/isn/{isn_slug}/signals/receiver", isnReceivers.CreateIsnReceiverHandler)
				r.Put("/isn/{isn_slug}/signals/receiver", isnReceivers.UpdateIsnReceiverHandler)
				r.Post("/isn/{isn_slug}/signals/retriever", isnRetrievers.CreateIsnRetrieverHandler)
				r.Put("/isn/{isn_slug}/signals/retriever", isnRetrievers.UpdateIsnRetrieverHandler)

				// signal defs
				r.Post("/isn/{isn_slug}/signal_types", signalTypes.CreateSignalTypeHandler)
				r.Put("/isn/{isn_slug}/signal_types/{slug}/v{sem_ver}", signalTypes.UpdateSignalTypeHandler)
				r.Delete("/isn/{isn_slug}/signal_types/{slug}/v{sem_ver}", signalTypes.DeleteSignalTypeHandler)

			})
			// signals runtime
			r.Group(func(r chi.Router) {

				// batches
				r.Use(s.authService.RequireIsnWritePermission())
				r.Post("/isn/{isn_slug}/signals/batch", signalBatches.CreateSignalsBatchHandler)
				// webhooks
				r.Post("/api/webhooks", webhooks.HandlerWebhook)
			})
		})

		// unrestricted
		r.Get("/isn", isn.GetIsnsHandler)
		r.Get("/isn/{isn_slug}", isn.GetIsnHandler)
		r.Get("/isn/{isn_slug}/signals/receiver", isnReceivers.GetIsnReceiverHandler)
		r.Get("/isn/{isn_slug}/signals/retriever", isnRetrievers.GetIsnRetrieverHandler)
		r.Get("/isn/{isn_slug}/signal_types", signalTypes.GetSignalTypesHandler)
		r.Get("/isn/{isn_slug}/signal_types/{slug}/v{sem_ver}", signalTypes.GetSignalTypeHandler)
	})

	// Admin
	s.router.Route("/admin", func(r chi.Router) {
		r.Use(s.authService.RequireDevEnv)
		r.Post("/reset", admin.ResetHandler) // delete all users and content  (dev env only)

		// pending implementation of admin role
		r.Get("/users/{id}", users.GetUserHandler)
	})

	//health
	s.router.Route("/health", func(r chi.Router) {
		r.Get("/ready", admin.ReadinessHandler) // health check on database
		r.Get("/live", admin.LivenessHandler)
	})

	// documentation
	s.router.Route("/assets", func(r chi.Router) {
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))).ServeHTTP(w, r)
		})
	})
	s.router.Get("/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./assets/home.html") })
	s.router.Get("/docs", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/redoc.html") })
	s.router.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/swagger.json") })
}
func (s *Server) Run() {

	serverAddr := fmt.Sprintf("%s:%d", s.serverConfig.Host, s.serverConfig.Port)
	httpServer := &http.Server{
		Addr:         serverAddr,
		Handler:      s.router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// close the db connections when exiting
	defer func() {
		err := s.db.Close()
		if err != nil {
			s.serverLogger.Warn().Msgf("error closing database connections: %v", err)
		} else {
			s.serverLogger.Info().Msg("database connect closed")
		}
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
