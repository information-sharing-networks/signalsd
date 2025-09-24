package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

func (h *HandlerService) ManageServiceAccountsPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	if err := templates.ManageServiceAccounts().Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render CreateServiceAccount template", slog.String("error", err.Error()))
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
	accessToken, ok := auth.ContextAccessToken(r.Context())
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

	res, err := h.ApiClient.CreateServiceAccount(accessToken, req)
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

func (h *HandlerService) ReissueServiceAccount(w http.ResponseWriter, r *http.Request) {
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
	accessToken, ok := auth.ContextAccessToken(r.Context())
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

	res, err := h.ApiClient.ReissueServiceAccount(accessToken, req)
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
		ClientID:  res.ClientID,
		AccountID: res.AccountID,
		SetupURL:  res.SetupURL,
		ExpiresAt: res.ExpiresAt,
		ExpiresIn: res.ExpiresIn,
	}
	component := templates.ServiceAccountReissueSuccess(successResponse)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}

// ReissueButtonState returns the reissue button in the correct enabled/disabled state
func (h *HandlerService) ReissueButtonState(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	serviceAccountValue := r.FormValue("service-account-dropdown")
	isEnabled := serviceAccountValue != ""

	component := templates.ReissueButton(isEnabled)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render reissue button", slog.String("error", err.Error()))
	}
}
