package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

func (h *HandlerService) GeneratePasswordResetLinkPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		h.renderErrorAlert(w, r, "Authentication required. Please log in again.", "Failed to get accessTokenDetails from context in GeneratePasswordResetLinkPage")
		return
	}

	// Get users from API
	users, err := h.ApiClient.GetUserOptionsList(accessTokenDetails.AccessToken)
	if err != nil {
		h.renderErrorAlert(w, r, "Failed to load users. Please try again.", "Failed to get users: "+err.Error())
		return
	}

	if err := templates.GeneratePasswordResetLinkPage(users).Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render manage users page", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) GeneratePasswordResetLink(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	userDropdownValue := r.FormValue("user-dropdown")
	if userDropdownValue == "" {
		component := templates.ErrorAlert("you must select a user account")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render ErrorAlert", slog.String("error", err.Error()))
		}
		return
	}

	// Parse the combined organization|email value
	parts := strings.Split(userDropdownValue, "|")
	if len(parts) != 2 {
		component := templates.ErrorAlert("invalid user account selection")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render ErrorAlert", slog.String("error", err.Error()))
		}
		return
	}

	email := parts[0]

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	res, err := h.ApiClient.GeneratePasswordResetLink(accessTokenDetails.AccessToken, email)
	if err != nil {
		reqLogger.Error("Failed to generate password reset link for user", slog.String("error", err.Error()))

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
	successResponse := client.GeneratePasswordResetLinkResponse{
		UserEmail: res.UserEmail,
		AccountID: res.AccountID,
		ResetURL:  res.ResetURL,
		ExpiresAt: res.ExpiresAt,
		ExpiresIn: res.ExpiresIn,
	}
	component := templates.GeneratePasswordResetLinkSuccess(successResponse)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}
