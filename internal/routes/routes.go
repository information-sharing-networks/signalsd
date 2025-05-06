package routes

import (
	"github.com/go-chi/chi/v5"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/handlers"
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
	r.Get("/api/signal_defs/{SignalDefID}", signalDefsHandler.GetSignalDefHandler)
	r.Delete("/api/signal_defs/{SignalDefID}", signalDefsHandler.DeleteSignalDefsHandler)
	r.Put("/api/signal_defs/{SignalDefID}", signalDefsHandler.UpdateSignalDefHandler)

	// Health
	r.Get("/api/health", adminHandler.ReadinessHandler) // health check

	// Admin endpoints
	r.Post("/admin/reset", adminHandler.ResetHandler) // delete all users and content (dev only)
}
