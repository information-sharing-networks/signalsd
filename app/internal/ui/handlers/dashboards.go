package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

func (h *HandlerService) DashboardHandler(w http.ResponseWriter, r *http.Request) {
	component := templates.DashboardPage()
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger := logger.ContextRequestLogger(r.Context())
		reqLogger.Error("Failed to render dashboard page", slog.String("error", err.Error()))
	}
}

// IsnAdminDashboardHandler renders the main admin dashboard page
// Access control is handled by RequireAdminAccess middleware
func (h *HandlerService) IsnAdminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	// Render admin dashboard - access is validated by middleware
	component := templates.AdminDashboardPage()
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger := logger.ContextRequestLogger(r.Context())
		reqLogger.Error("Failed to render admin dashboard", slog.String("error", err.Error()))
	}
}
