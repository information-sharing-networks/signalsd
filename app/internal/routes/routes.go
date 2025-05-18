package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	signals "github.com/nickabs/signalsd/app"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/handlers"

	_ "github.com/nickabs/signalsd/app/docs"
)

func RegisterRoutes(r *chi.Mux, cfg *signals.ServiceConfig) {

	// see middleware for authentication
	adminHandler := handlers.NewAdminHandler(cfg)
	usersHandler := handlers.NewUserHandler(cfg)
	loginHandler := handlers.NewLoginHandler(cfg)
	authHandler := handlers.NewAuthHandler(cfg)
	webhookHandler := handlers.NewWebhookHandler(cfg)
	signalDefsHandler := handlers.NewSignalDefHandler(cfg)
	isnHandler := handlers.NewIsnHandler(cfg)
	isnReceiverHandler := handlers.NewIsnReceiverHandler(cfg)
	isnRetrieverHandler := handlers.NewIsnRetrieverHandler(cfg)

	authService := auth.NewAuthService(cfg)

	// api
	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			// signal defs
			r.Use(authService.ValidateAccessToken)
			r.Put("/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.UpdateSignalDefHandler)
			r.Post("/signal_defs", signalDefsHandler.CreateSignalDefHandler)
			r.Delete("/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.DeleteSignalDefHandler)

			// ISN management
			r.Post("/isn", isnHandler.CreateIsnHandler)
			r.Put("/isn/{isn_slug}", isnHandler.UpdateIsnHandler)
			r.Post("/isn/receiver", isnReceiverHandler.CreateIsnReceiverHandler)
			r.Put("/isn/receiver/{isn_receivers_slug}", isnReceiverHandler.UpdateIsnReceiverHandler)
			r.Post("/isn/retriever", isnRetrieverHandler.CreateIsnRetrieverHandler)
			r.Put("/isn/retriever/{isn_retrievers_slug}", isnRetrieverHandler.UpdateIsnRetrieverHandler)

			// webhooks
			r.Post("/api/webhooks", webhookHandler.HandlerWebhook)
		})
		// note do not show emails in the public apis (use users.id instead)
		r.Get("/signal_defs", signalDefsHandler.GetSignalDefsHandler)
		r.Get("/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.GetSignalDefHandler)
		r.Get("/isn", isnHandler.GetIsnsHandler)
		r.Get("/isn/{isn_slug}", isnHandler.GetIsnHandler)
		r.Get("/isn/receiver/{slug}", isnReceiverHandler.GetIsnReceiverHandler)
		r.Get("/isn/retriever/{slug}", isnRetrieverHandler.GetIsnRetrieverHandler)
	})

	// auth
	r.Route("/auth", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(authService.ValidateAccessToken)
			r.Put("/password/reset", usersHandler.UpdatePasswordHandler)
		})
		r.Group(func(r chi.Router) {
			r.Use(authService.ValidateRefreshToken)
			r.Post("/refresh-token", authHandler.RefreshAccessTokenHandler)
		})
		r.Post("/register", usersHandler.CreateUserHandler)
		r.Post("/login", loginHandler.LoginHandler)
		r.Post("/revoke-refresh-token", authHandler.RevokeRefreshTokenHandler)
	})

	// todo protect get user endpoint so as not to expose email addresses (server admin account + isn participants only)

	// Admin
	r.Route("/admin", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(authService.ValidateDevEnv)
			r.Post("/reset", adminHandler.ResetHandler) // delete all users and content  (dev env only)

			// pending implementation of admin role
			r.Get("/users/{id}", usersHandler.GetUserHandler)
			r.Get("/users", usersHandler.GetUsersHandler)
		})
		r.Get("/health", adminHandler.ReadinessHandler) // health check
	})

	// documentation
	r.Route("/assets", func(r chi.Router) {
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))).ServeHTTP(w, r)
		})
	})
	r.Get("/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./assets/home.html") })
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/redoc.html") })
	r.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/swagger.json") })
}
