package server

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// renderAccessDeniedPage is a helper function that renders access denied pages with different messages
func (s *Server) renderAccessDeniedPage(w http.ResponseWriter, r *http.Request, message string) {
	templ.Handler(templates.AccessDeniedPage(s.config.Environment, "Access Denied", message)).ServeHTTP(w, r)
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
