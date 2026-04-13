package server

import (
	"net/http"

	"github.com/a-h/templ"
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
	templ.Handler(templates.DashboardPage(s.config.Environment)).ServeHTTP(w, r)
}

// IsnAdminDashboardPage godoc
//
//	@Summary		Admin dashboard
//	@Description	Admin landing page. Requires isnadmin or siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin [get]
func (s *Server) IsnAdminDashboardPage(w http.ResponseWriter, r *http.Request) {
	templ.Handler(templates.AdminDashboardPage(s.config.Environment)).ServeHTTP(w, r)
}
