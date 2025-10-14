package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

func (h *HandlerService) CreateServiceAccountsPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	if err := templates.CreateServiceAccountsPage(h.Environment).Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render CreateServiceAccount template", slog.String("error", err.Error()))
	}

}

func (h *HandlerService) ReissueServiceAccountCredentialsPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		h.renderErrorAlert(w, r, "Authentication required. Please log in again.", "Failed to get accessTokenDetails from context in ReissueServiceAccountCredentialsPage")
		return
	}

	// Get service accounts from API
	serviceAccounts, err := h.ApiClient.GetServiceAccountOptionsList(accessTokenDetails.AccessToken)
	if err != nil {
		h.renderErrorAlert(w, r, "Failed to load service accounts. Please try again.", "Failed to get service accounts: "+err.Error())
		return
	}

	if err := templates.ReissueServiceAccountCredentialsPage(h.Environment, serviceAccounts).Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render ReissueServiceAccount template", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) CreateServiceAccount(w http.ResponseWriter, r *http.Request) {
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

	res, err := h.ApiClient.CreateServiceAccount(accessTokenDetails.AccessToken, req)
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

func (h *HandlerService) ReissueServiceAccountCredentials(w http.ResponseWriter, r *http.Request) {
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

	res, err := h.ApiClient.ReissueServiceAccountCredentials(accessTokenDetails.AccessToken, req)
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
