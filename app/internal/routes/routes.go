package routes

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	_ "github.com/nickabs/signalsd/app/docs"
	"github.com/nickabs/signalsd/app/internal/services"
)

func RegisterRoutes(r *chi.Mux, services services.Services) {

	// api
	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {

			r.Use(services.AuthService.ValidateAccessToken)

			// ISN management
			r.Post("/isn", services.Isn.CreateIsnHandler)
			r.Put("/isn/{isn_slug}", services.Isn.UpdateIsnHandler)
			r.Post("/isn/{isn_slug}/signals/receiver", services.IsnReceiver.CreateIsnReceiverHandler)
			r.Put("/isn/{isn_slug}/signals/receiver", services.IsnReceiver.UpdateIsnReceiverHandler)
			r.Post("/isn/{isn_slug}/signals/retriever", services.IsnRetriever.CreateIsnRetrieverHandler)
			r.Put("/isn/{isn_slug}/signals/retriever", services.IsnRetriever.UpdateIsnRetrieverHandler)

			// signal defs
			r.Post("/isn/{isn_slug}/signal_types", services.SignalType.CreateSignalTypeHandler)
			r.Put("/isn/{isn_slug}/signal_types/{slug}/v{sem_ver}", services.SignalType.UpdateSignalTypeHandler)
			r.Delete("/isn/{isn_slug}/signal_types/{slug}/v{sem_ver}", services.SignalType.DeleteSignalTypeHandler)

			// webhooks
			r.Post("/api/webhooks", services.Webhook.HandlerWebhook)
		})
		r.Get("/isn", services.Isn.GetIsnsHandler)
		r.Get("/isn/{isn_slug}", services.Isn.GetIsnHandler)
		r.Get("/isn/{isn_slug}/signals/receiver", services.IsnReceiver.GetIsnReceiverHandler)
		r.Get("/isn/{isn_slug}/signals/retriever", services.IsnRetriever.GetIsnRetrieverHandler)
		r.Get("/isn/{isn_slug}/signal_types", services.SignalType.GetSignalTypesHandler)
		r.Get("/isn/{isn_slug}/signal_types/{slug}/v{sem_ver}", services.SignalType.GetSignalTypeHandler)
	})

	// auth
	r.Route("/auth", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(services.AuthService.ValidateAccessToken)
			r.Put("/password/reset", services.Users.UpdatePasswordHandler)
		})
		r.Group(func(r chi.Router) {
			r.Use(services.AuthService.ValidateRefreshToken)
			r.Post("/refresh-token", services.Token.RefreshAccessTokenHandler)
		})
		r.Post("/register", services.Users.CreateUserHandler)
		r.Post("/login", services.Login.LoginHandler)
		r.Post("/revoke-refresh-token", services.Token.RevokeRefreshTokenHandler)
		r.Get("/users", services.Users.GetUsersHandler)
	})

	// todo protect get user endpoint so as not to expose email addresses (server admin account + isn participants only)

	// Admin
	r.Route("/admin", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(services.AuthService.ValidateDevEnv)
			r.Post("/reset", services.Admin.ResetHandler) // delete all users and content  (dev env only)

			// pending implementation of admin role
			r.Get("/users/{id}", services.Users.GetUserHandler)
		})
	})
	r.Route("/health", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Get("/ready", services.Admin.ReadinessHandler) // health check on database
			r.Get("/live", services.Admin.LivenessHandler)
		})
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
