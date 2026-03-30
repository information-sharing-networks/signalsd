package server

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// DashboardPage godoc
//
//	@Summary		User dashboard
//	@Description	Entry point after login. Requires authentication.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/dashboard [get]
func (s *Server) DashboardPage(w http.ResponseWriter, r *http.Request) {
	component := templates.DashboardPage(s.config.Environment)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger := logger.ContextRequestLogger(r.Context())
		reqLogger.Error("Failed to render dashboard page", slog.String("error", err.Error()))
	}
}

// IsnAdminDashboardPage godoc
//
//	@Summary		Admin dashboard
//	@Description	Admin landing page. Requires isnadmin or siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin [get]
func (s *Server) IsnAdminDashboardPage(w http.ResponseWriter, r *http.Request) {
	// Render admin dashboard - access is validated by middleware
	component := templates.AdminDashboardPage(s.config.Environment)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger := logger.ContextRequestLogger(r.Context())
		reqLogger.Error("Failed to render admin dashboard", slog.String("error", err.Error()))
	}
}
