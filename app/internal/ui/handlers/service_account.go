package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

func (h *HandlerService) CreateServiceAccountPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	component := templates.CreateServiceAccount()

	if err := component.Render(r.Context(), w); err != nil {
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

	// Get access token from cookie
	accessTokenCookie, err := r.Cookie(config.AccessTokenCookieName)
	if err != nil {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	accessToken := accessTokenCookie.Value

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
