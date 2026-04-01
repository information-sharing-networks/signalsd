package server

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// CreateServiceAccountsPage godoc
//
//	@Summary		Create service account page
//	@Description	Renders the create service account form. Requires isnadmin or siteadmin role.
//	@Tags			UI Page
//	@Success		200	"HTML page"
//	@Router			/admin/service-accounts/create [get]
func (s *Server) CreateServiceAccountsPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	if err := templates.CreateServiceAccountsPage(s.config.Environment).Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render CreateServiceAccount template", slog.String("error", err.Error()))
	}
}

// ReissueServiceAccountCredentialsPage godoc
//
//	@Summary		Reissue service account credentials page
//	@Description	Renders the reissue credentials form. Requires isnadmin or siteadmin role.
//	@Tags			UI Page
//	@Success		200	"HTML page"
//	@Router			/admin/service-accounts/reissue-credentials [get]
func (s *Server) ReissueServiceAccountCredentialsPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

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

	if err := templates.ReissueServiceAccountCredentialsPage(s.config.Environment, getServiceAccountOptions(serviceAccounts)).Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render ReissueServiceAccount template", slog.String("error", err.Error()))
	}
}

// CreateServiceAccount godoc
//
//	@Summary		Create service account
//	@Description	HTMX endpoint. Creates a new service account. Requires isnadmin or siteadmin role.
//	@Tags			HTMX Actions
//	@Param			email			formData	string	true	"Contact email"
//	@Param			organization	formData	string	true	"Organization name"
//	@Success		200				"HTML partial"
//	@Router			/ui-api/service-accounts/create [post]
func (s *Server) CreateServiceAccount(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	email := r.FormValue("email")
	organization := r.FormValue("organization")

	if email == "" || organization == "" {
		component := templates.ErrorAlert("you must supply values for both email and organization")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render ErrorAlert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
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

		component := templates.ErrorAlert(msg)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Success response
	component := templates.ServiceAccountCreationSuccess(*res)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}

// ReissueServiceAccountCredentials godoc
//
//	@Summary		Reissue service account credentials
//	@Description	HTMX endpoint. Issues a new client secret for the selected service account. Requires isnadmin or siteadmin role.
//	@Tags			HTMX Actions
//	@Param			service-account-dropdown	formData	string	true	"Client ID of the service account"
//	@Success		200							"HTML partial"
//	@Router			/ui-api/service-accounts/reissue-credentials [put]
func (s *Server) ReissueServiceAccountCredentials(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	clientID := r.FormValue("service-account-dropdown")
	if clientID == "" {
		component := templates.ErrorAlert("you must select a service account to reissue credentials")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render ErrorAlert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
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

		component := templates.ErrorAlert(msg)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
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
	component := templates.ServiceAccountReissueSuccess(successResponse)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}
