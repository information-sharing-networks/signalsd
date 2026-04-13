package server

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// ClearAlerts godoc
//
//	@Summary		Clear alert messages
//	@Description	HTMX endpoint. Returns empty content to clear any displayed alert partial.
//	@Tags			HTMX Actions
//	@Success		200	"empty"
//	@Router			/ui-api/alerts/clear [get]
func (s *Server) ClearAlerts(w http.ResponseWriter, r *http.Request) {
	templ.Handler(templates.ClearAlerts()).ServeHTTP(w, r)
}

// renderErrorAlert is a helper to render error alerts with consistent logging
func (s *Server) renderErrorAlert(w http.ResponseWriter, r *http.Request, message string, logError string) {
	reqLogger := logger.ContextRequestLogger(r.Context())
	reqLogger.Error(logError)
	templ.Handler(templates.ErrorAlert(message)).ServeHTTP(w, r)
}
