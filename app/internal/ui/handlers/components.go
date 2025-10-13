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

// renderErrorAlert is a helper to render error alerts with consistent logging
func (h *HandlerService) renderErrorAlert(w http.ResponseWriter, r *http.Request, message string, logError string) {
	reqLogger := logger.ContextRequestLogger(r.Context())
	reqLogger.Error(logError)
	component := templates.ErrorAlert(message)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
	}
}
