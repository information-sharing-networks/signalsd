package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/handlers"
	internalMiddleware "github.com/nickabs/signals/internal/middleware"

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

	// User Management & Authentication
	r.Post("/api/users", usersHandler.CreateUserHandler)
	r.Put("/api/users", usersHandler.UpdateUserHandler)
	r.Get("/api/users", usersHandler.GetUsersHandler)
	r.Get("/api/users/{id}", usersHandler.GetUserByIDHandler)
	r.Post("/api/login", loginHandler.LoginHandler)
	r.Post("/api/refresh", authHandler.RefreshAccessTokenHandler)
	r.Post("/api/revoke", authHandler.RevokeRefreshTokenHandler)

	// signal defs
	r.Post("/api/signal_defs", signalDefsHandler.CreateSignalDefHandler)
	r.With(internalMiddleware.GetWithSignalDefID()).Put("/api/signal_defs/{signal_def_id}", signalDefsHandler.UpdateSignalDefHandler)
	r.With(internalMiddleware.GetWithSignalDefSlug(cfg)).Put("/api/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.UpdateSignalDefHandler)
	r.With(internalMiddleware.GetWithSignalDefID()).Delete("/api/signal_defs/{signal_def_id}", signalDefsHandler.DeleteSignalDefHandler)
	r.With(internalMiddleware.GetWithSignalDefSlug(cfg)).Delete("/api/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.DeleteSignalDefHandler)
	r.Get("/api/signal_defs", signalDefsHandler.GetSignalDefsHandler)
	r.With(internalMiddleware.GetWithSignalDefID()).Get("/api/signal_defs/{signal_def_id}", signalDefsHandler.GetSignalDefHandler)
	r.With(internalMiddleware.GetWithSignalDefSlug(cfg)).Get("/api/signal_defs/{slug}/v{sem_ver}", signalDefsHandler.GetSignalDefHandler)

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
