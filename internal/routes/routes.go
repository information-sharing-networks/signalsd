package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/auth"
	"github.com/nickabs/signals/internal/handlers"

	_ "github.com/nickabs/signals/docs"
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

		})
		r.Get("/signal_defs", signalDefsHandler.GetSignalDefsHandler)
		r.Get("/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.GetSignalDefHandler)
	})

	// auth
	r.Post("/auth/register", usersHandler.CreateUserHandler)
	r.Put("/auth/pasword/reset", usersHandler.UpdatePasswordHandler)
	r.Post("/auth/login", loginHandler.LoginHandler)
	r.Post("/auth/refresh-token", authHandler.RefreshAccessTokenHandler)
	r.Post("/auth/revoke-refresh-token", authHandler.RevokeRefreshTokenHandler)
	// todo protect get user endpoint so as not to expose email addresses (server admin account only)
	r.Get("/auth/users/{id}", usersHandler.GetUserByIDHandler)

	// Admin endpoints
	r.Post("/admin/reset", adminHandler.ResetHandler)     // delete all users and content (dev only)
	r.Get("/admin/health", adminHandler.ReadinessHandler) // health check
	r.Post("/api/webhooks", webhookHandler.HandlerWebhook)

	// documentation
	r.Route("/assets", func(r chi.Router) {
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))).ServeHTTP(w, r)
		})
	})

	// Route for the home page
	r.Get("/", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./assets/home.html") })

	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/redoc.html") })

	r.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, "./docs/swagger.json") })

}
