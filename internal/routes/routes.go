package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nickabs/signals"
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

	// auth
	r.Post("/auth/register", usersHandler.CreateUserHandler)
	r.Put("/auth/pasword/reset", usersHandler.UpdatePasswordHandler)
	r.Post("/auth/login", loginHandler.LoginHandler)
	r.Post("/auth/refresh-token", authHandler.RefreshAccessTokenHandler)
	r.Post("/auth/revoke-token", authHandler.RevokeRefreshTokenHandler)
	// todo protect get user endpoint so as not to expose email addresses (server admin account only)
	r.Get("/auth/users/{id}", usersHandler.GetUserByIDHandler)

	// signal defs
	r.Post("/api/signal_defs", signalDefsHandler.CreateSignalDefHandler)
	r.Put("/api/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.UpdateSignalDefHandler)
	r.Delete("/api/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.DeleteSignalDefHandler)
	r.Get("/api/signal_defs", signalDefsHandler.GetSignalDefsHandler)
	r.Get("/api/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.GetSignalDefHandler)

	// ISN management
	r.Post("/api/isn", isnHandler.CreateIsnHandler)
	r.Put("/api/isn/{isn_slug}", isnHandler.UpdateIsnHandler)
	r.Post("/api/isn/receiver", isnReceiverHandler.CreateIsnReceiverHandler)
	r.Put("/api/isn/receiver/{isn_receivers_slug}", isnReceiverHandler.UpdateIsnReceiverHandler)
	r.Post("/api/isn/retriever", isnRetrieverHandler.CreateIsnRetrieverHandler)
	r.Put("/api/isn/retriever/{isn_retrievers_slug}", isnRetrieverHandler.UpdateIsnRetrieverHandler)

	// Admin endpoints
	r.Post("/admin/reset", adminHandler.ResetHandler)     // delete all users and content (dev only)
	r.Get("/admin/health", adminHandler.ReadinessHandler) // health check
	r.Post("/api/webhooks", webhookHandler.HandlerWebhook)

	// API doco
	r.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./docs/swagger.json")
	})
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./docs/redoc.html")
	})
}
