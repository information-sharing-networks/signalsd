package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

func (h *HandlerService) ClearAlerts(w http.ResponseWriter, r *http.Request) {
	component := templates.ClearAlerts()
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger := logger.ContextRequestLogger(r.Context())
		reqLogger.Error("Failed to render ClearAlerts", slog.String("error", err.Error()))
	}
}
