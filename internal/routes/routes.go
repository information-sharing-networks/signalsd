package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/handlers"

	_ "github.com/nickabs/signals/docs"
)

func RegisterRoutes(r *chi.Mux, cfg *signals.ServiceConfig) {
	adminHandler := handlers.NewAdminHandler(cfg)
	usersHandler := handlers.NewUserHandler(cfg)
	loginHandler := handlers.NewLoginHandler(cfg)
	authHandler := handlers.NewAuthHandler(cfg)
	webhookHandler := handlers.NewWebhookHandler(cfg)
	signalDefsHandler := handlers.NewSignalDefHandler(cfg)

	// User Management & Authentication
	r.Post("/api/users", usersHandler.CreateUserHandler)
	r.Put("/api/users", usersHandler.UpdateUserHandler)
	r.Post("/api/login", loginHandler.LoginHandler)
	r.Post("/api/refresh", authHandler.RefreshAccessTokenHandler)
	r.Post("/api/revoke", authHandler.RevokeRefreshTokenHandler)
	r.Post("/api/webhooks", webhookHandler.HandlerWebhook)

	// signal defs
	r.Post("/api/signal_defs", signalDefsHandler.CreateSignalDefHandler)
	r.Get("/api/signal_defs", signalDefsHandler.GetSignalDefsHandler)
	r.Get("/api/signal_defs/{id}", signalDefsHandler.GetSignalDefByIDHandler)
	r.Get("/api/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.GetSignalDefBySlugHandler)
	r.Delete("/api/signal_defs/{SignalDefID}", signalDefsHandler.DeleteSignalDefsHandler) //todo
	r.Put("/api/signal_defs/{SignalDefID}", signalDefsHandler.UpdateSignalDefHandler)

	// Admin endpoints
	r.Post("/admin/reset", adminHandler.ResetHandler)     // delete all users and content (dev only)
	r.Get("/admin/health", adminHandler.ReadinessHandler) // health check

	// API doco
	r.Get("/swagger.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./docs/swagger.json")
	})
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./docs/redoc.html")
	})
}
