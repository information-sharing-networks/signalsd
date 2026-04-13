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

// CreateServiceAccountsPage godoc
//
//	@Summary		Create service account page
//	@Description	Renders the create service account form. Requires isnadmin or siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/service-accounts/create [get]
func (s *Server) CreateServiceAccountsPage(w http.ResponseWriter, r *http.Request) {
	templ.Handler(templates.CreateServiceAccountsPage(s.config.Environment)).ServeHTTP(w, r)
}

// ReissueServiceAccountCredentialsPage godoc
//
//	@Summary		Reissue service account credentials page
//	@Description	Renders the reissue credentials form. Requires isnadmin or siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/service-accounts/reissue-credentials [get]
func (s *Server) ReissueServiceAccountCredentialsPage(w http.ResponseWriter, r *http.Request) {
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		s.renderErrorAlert(w, r, "Authentication required. Please log in again.", "Failed to get accessTokenDetails from context in ReissueServiceAccountCredentialsPage")
		return
	}

	// Get service accounts from API
	serviceAccounts, err := s.apiClient.GetServiceAccounts(accessTokenDetails.AccessToken)
	if err != nil {
		s.renderErrorAlert(w, r, "Failed to load service accounts. Please try again.", "Failed to get service accounts: "+err.Error())
		return
	}

	templ.Handler(templates.ReissueServiceAccountCredentialsPage(s.config.Environment, getServiceAccountOptions(serviceAccounts))).ServeHTTP(w, r)
}

// CreateServiceAccount godoc
//
//	@Summary		Create service account
//	@Description	HTMX endpoint. Creates a new service account. Requires isnadmin or siteadmin role.
//	@Tags			HTMX Actions
//	@Param			email			formData	string	true	"Contact email"
//	@Param			organization	formData	string	true	"Organization name"
//	@Success		200				"HTML partial"
//	@Failure		400				"HTML error partial"
//	@Failure		401				"HTML error partial"
//	@Router			/ui-api/service-accounts/create [post]
func (s *Server) CreateServiceAccount(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	email := r.FormValue("email")
	organization := r.FormValue("organization")

	if email == "" || organization == "" {
		templ.Handler(templates.ErrorAlert("you must supply values for both email and organization")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	req := client.CreateServiceAccountRequest{
		ClientOrganization: organization,
		ClientContactEmail: email,
	}

	res, err := s.apiClient.CreateServiceAccount(accessTokenDetails.AccessToken, req)
	if err != nil {
		reqLogger.Error("Failed to create service account", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.ServiceAccountCreationSuccess(*res)).ServeHTTP(w, r)
}

// ReissueServiceAccountCredentials godoc
//
//	@Summary		Reissue service account credentials
//	@Description	HTMX endpoint. Issues a new client secret for the selected service account. Requires isnadmin or siteadmin role.
//	@Tags			HTMX Actions
//	@Param			service-account-dropdown	formData	string	true	"Client ID of the service account"
//	@Success		200							"HTML partial"
//	@Failure		400							"HTML error partial"
//	@Failure		401							"HTML error partial"
//	@Router			/ui-api/service-accounts/reissue-credentials [put]
func (s *Server) ReissueServiceAccountCredentials(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	clientID := r.FormValue("service-account-dropdown")
	if clientID == "" {
		templ.Handler(templates.ErrorAlert("you must select a service account to reissue credentials")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	req := client.ReissueServiceAccountRequest{
		ClientID: clientID,
	}

	res, err := s.apiClient.ReissueServiceAccountCredentials(accessTokenDetails.AccessToken, req)
	if err != nil {
		reqLogger.Error("Failed to reissue service account credentials", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	// Success response - reuse the same template as creation since the response structure is the same
	successResponse := client.ReissueServiceAccountResponse{
		ClientID:           res.ClientID,
		ClientContactEmail: res.ClientContactEmail,
		AccountID:          res.AccountID,
		SetupURL:           res.SetupURL,
		ExpiresAt:          res.ExpiresAt,
		ExpiresIn:          res.ExpiresIn,
	}
	templ.Handler(templates.ServiceAccountReissueSuccess(successResponse)).ServeHTTP(w, r)
}
