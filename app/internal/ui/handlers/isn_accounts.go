package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// UpdateIsnAccountHandler handles the form submission to add an account to an ISN
func (h *HandlerService) UpdateIsnAccountHandler(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn_slug")
	accountType := r.FormValue("account_type")
	accountIdentifier := r.FormValue("account_identifier")
	permission := r.FormValue("permission")

	// Validate required fields
	if isnSlug == "" || accountType == "" || accountIdentifier == "" || permission == "" {
		component := templates.ErrorAlert("Please fill in all fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Validate account type
	if !signalsd.ValidAccountTypes[accountType] {
		component := templates.ErrorAlert("Invalid account type selected.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from cookie
	accessTokenCookie, err := r.Cookie(config.AccessTokenCookieName)
	if err != nil {
		// unexpected - this should be caught by middlware check on ISN access
		reqLogger.Error("Failed to read access token cookie", slog.String("component", "templates.handleAddIsnAccount"), slog.String("error", err.Error()))

		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}
	accessToken := accessTokenCookie.Value

	// Call the API to add the account to the ISN
	err = h.ApiClient.UpdateIsnAccountAccess(accessToken, isnSlug, accountType, accountIdentifier, permission)
	if err != nil {
		reqLogger.Error("Failed to add account to ISN", slog.String("component", "templates.handleAddIsnAccount"), slog.String("error", err.Error()))

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
	msg := ""
	if permission == "none" {
		msg = "Account successfully removed from ISN"
	} else {
		msg = "Account successfully added to ISN"
	}

	component := templates.SuccessAlert(msg)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}

// IsnAccountManagementHandler renders the ISN accounts administration page
func (h *HandlerService) IsnAccountManagementHandler(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Get user permissions from cookie
	isnPerms, err := h.AuthService.GetIsnPermsFromCookie(r)
	if err != nil {
		reqLogger.Error("failed to read IsnPerms from cookie", slog.String("error", err.Error()))
		return
	}

	// Convert permissions to ISN list for dropdown (only ISNs where user has write permission)
	var isns []types.IsnDropdown
	isns = make([]types.IsnDropdown, 0, len(isnPerms))
	for isnSlug, perm := range isnPerms {
		// Only show ISNs where user has write permission (admins/owners)
		if perm.IsnAdmin {
			isns = append(isns, types.IsnDropdown{
				Slug:    isnSlug,
				IsInUse: true,
			})
		}
	}

	// Render admin page
	component := templates.IsnAccountManagementPage(isns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render ISN accounts admin page", slog.String("error", err.Error()))
	}
}

// AccountIdentifierFieldHandler renders the appropriate input field based on account type
func (h *HandlerService) AccountIdentifierFieldHandler(w http.ResponseWriter, r *http.Request) {
	accountType := r.FormValue("account_type")

	component := templates.AccountIdentifierField(accountType)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger := logger.ContextRequestLogger(r.Context())
		reqLogger.Error("Failed to render account identifier field", slog.String("error", err.Error()))
	}
}
