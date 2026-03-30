package server

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// renderAccessDeniedPage is a helper function that renders access denied pages with different messages
func (s *Server) renderAccessDeniedPage(w http.ResponseWriter, r *http.Request, message string) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	component := templates.AccessDeniedPage(s.config.Environment, "Access Denied", message)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render access denied page", slog.String("error", err.Error()))
	}
}

// AccessDeniedPage renders the general access denied page.
func (s *Server) AccessDeniedPage(w http.ResponseWriter, r *http.Request) {
	// Check if roles parameter is provided for more specific messaging
	msg := r.URL.Query().Get("msg")
	if msg != "" {
		s.renderAccessDeniedPage(w, r, msg)
	} else {
		s.renderAccessDeniedPage(w, r, "You do not have permission to use this feature")
	}
}
