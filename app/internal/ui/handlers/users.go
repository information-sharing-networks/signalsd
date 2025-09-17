package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

func (h *HandlerService) ManageUsersPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	if err := templates.ManageUsersPage().Render(r.Context(), w); err != nil {
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

	res, err := h.ApiClient.GeneratePasswordResetLink(accessToken, email)
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

// ReissueButtonState returns the reissue button in the correct enabled/disabled state following a dropdown change
func (h *HandlerService) GeneratePasswordResetButtonState(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	user := r.FormValue("user-dropdown")
	isEnabled := user != ""

	if err := templates.GeneratePasswordResetButton(isEnabled).Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render generate password reset button", slog.String("error", err.Error()))
	}
}
