package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// AccessDeniedHandler rednders the access denied page.
func (h *HandlerService) AccessDeniedHandler(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	component := templates.AccessDeniedPage("Access Denied", "You do not have permission to use this feature")
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render access denied page", slog.String("error", err.Error()))
	}
}
