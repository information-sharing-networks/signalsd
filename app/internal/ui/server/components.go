package server

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// renderErrorAlert is a helper to render error alerts with consistent logging
func (s *Server) renderErrorAlert(w http.ResponseWriter, r *http.Request, message string, logError string) {
	reqLogger := logger.ContextRequestLogger(r.Context())
	reqLogger.Error(logError)
	templ.Handler(templates.ErrorAlert(message)).ServeHTTP(w, r)
}
