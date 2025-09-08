package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// HandleIsnAccountAccess handles the form submission to add an account to an ISN
func (h *HandlerService) HandleIsnAccountAccess(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn_slug")
	accountEmail := r.FormValue("account_email")
	permission := r.FormValue("permission")

	// Validate required fields
	if isnSlug == "" || accountEmail == "" || permission == "" {
		h.RenderError(w, r, "Please fill in all fields.")
		return
	}

	// Get access token from cookie
	accessTokenCookie, err := r.Cookie(config.AccessTokenCookieName)
	if err != nil {
		reqLogger.Error("Failed to read access token cookie", slog.String("component", "templates.handleAddIsnAccount"), slog.String("error", err.Error()))
		h.RenderError(w, r, "Authentication required. Please log in again.")
		return
	}
	accessToken := accessTokenCookie.Value

	// Call the API to add the account to the ISN
	err = h.ApiClient.IsnAccountAccess(accessToken, isnSlug, accountEmail, permission)
	if err != nil {
		reqLogger.Error("Failed to add account to ISN", slog.String("component", "templates.handleAddIsnAccount"), slog.String("error", err.Error()))

		if ce, ok := err.(*client.ClientError); ok {
			h.RenderError(w, r, ce.UserError())
		} else {
			h.RenderError(w, r, "An error occurred. Please try again.")
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

// HandleIsnAccountsAdmin renders the ISN accounts administration page
func (h *HandlerService) HandleIsnAccountsAdmin(w http.ResponseWriter, r *http.Request) {
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
	component := templates.IsnAccountsAdminPage(isns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render ISN accounts admin page", slog.String("error", err.Error()))
	}
}
