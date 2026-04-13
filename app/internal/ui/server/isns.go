package server

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// CreateIsnPage godoc
//
//	@Summary		Create ISN page
//	@Description	Renders the create ISN form. Requires isnadmin or siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/isn/create [get]
func (s *Server) CreateIsnPage(w http.ResponseWriter, r *http.Request) {
	templ.Handler(templates.CreateIsnPage(s.config.Environment)).ServeHTTP(w, r)
}

// CreateIsn godoc
//
//	@Summary		Create ISN
//	@Description	HTMX endpoint. Creates a new ISN. Requires isnadmin or siteadmin role.
//	@Tags			HTMX Actions
//	@Param			title		formData	string	true	"ISN title"
//	@Param			detail		formData	string	true	"ISN description"
//	@Param			visibility	formData	string	true	"'public' or 'private'"
//	@Success		200			"HTML partial"
//	@Failure		400			"HTML error partial"
//	@Failure		401			"HTML error partial"
//	@Router			/ui-api/isn/create [post]
func (s *Server) CreateIsn(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	title := r.FormValue("title")
	detail := r.FormValue("detail")
	visibility := r.FormValue("visibility")

	// Validate required fields
	if title == "" || detail == "" || visibility == "" {
		templ.Handler(templates.ErrorAlert("Please fill in all fields.")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	// Call the API to create the ISN
	req := client.CreateIsnRequest{
		Title:      title,
		Detail:     detail,
		IsInUse:    true,
		Visibility: visibility,
	}

	res, err := s.apiClient.CreateIsn(accessTokenDetails.AccessToken, req)
	if err != nil {
		reqLogger.Error("Failed to create ISN", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.IsnCreationSuccess(*res)).ServeHTTP(w, r)
}

// ManageIsnStatusPage godoc
//
//	@Summary		Manage ISN status page
//	@Description	Renders the ISN enable/disable page. Only shows ISNs the user administers. Requires isnadmin or siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/isn/manage [get]
func (s *Server) ManageIsnStatusPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Inactive ISNs are included in the token claims (IsnPerm.InUse flags the current status).
	// CanAdminister is true for ISNs the user owns and for all ISNs when the user is a siteadmin,
	// so getIsnOptions with filterByIsnAdmin=true gives exactly the right set for this page.
	adminIsns := getIsnOptions(accessTokenDetails.IsnPerms, true, false)

	templ.Handler(templates.ManageIsnStatusPage(s.config.Environment, adminIsns)).ServeHTTP(w, r)
}

// ManageIsnStatus godoc
//
//	@Summary		Enable or disable an ISN
//	@Description	HTMX endpoint. Enables or disables an ISN. Requires isnadmin or siteadmin role.
//	@Tags			HTMX Actions
//	@Param			isn-slug	formData	string	true	"ISN slug"
//	@Param			action		formData	string	true	"'enable' or 'disable'"
//	@Success		200			"HTML partial"
//	@Failure		400			"HTML error partial"
//	@Failure		401			"HTML error partial"
//	@Router			/ui-api/isn/manage [put]
func (s *Server) ManageIsnStatus(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	action := r.FormValue("action")

	// Validate required fields
	if isnSlug == "" || action == "" {
		templ.Handler(templates.ErrorAlert("Please fill in all fields.")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	// Determine the is_in_use value based on action
	var isInUse bool
	switch action {
	case "enable":
		isInUse = true
	case "disable":
		isInUse = false
	default:
		templ.Handler(templates.ErrorAlert("Invalid action. Please select a valid action.")).ServeHTTP(w, r)
		return
	}

	// Call the API to update ISN status
	err := s.apiClient.UpdateIsnStatus(accessTokenDetails.AccessToken, isnSlug, isInUse)
	if err != nil {
		reqLogger.Error("Failed to update ISN status", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	var successMsg string
	if isInUse {
		successMsg = "ISN enabled successfully"
	} else {
		successMsg = "ISN disabled successfully"
	}

	templ.Handler(templates.SuccessAlert(successMsg)).ServeHTTP(w, r)
}
