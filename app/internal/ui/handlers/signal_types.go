package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// SignalTypeManagementHandler renders the signal type management page.
//
// Use with RequireAdminOrOwnerRole and RequireIsnAdmin middleware
func (h *HandlerService) SignalTypeManagementHandler(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Get user permissions from cookie
	isnPerms, err := h.AuthService.GetIsnPermsFromCookie(r)
	if err != nil {
		reqLogger.Error("failed to read IsnPerms from cookie", slog.String("error", err.Error()))
		return
	}

	// populate the isn dropdown list with ISNs where the user is an admin
	isns := h.getIsnDropDownList(isnPerms, true, false)

	// Render signal type management page
	component := templates.SignalTypeManagementPage(isns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal type management page", slog.String("error", err.Error()))
	}
}

// CreateSignalTypeHandler handles the form submission to create a new signal type
// Use with RequireAdminOrOwnerRole and RequireIsnAdmin middleware
func (h *HandlerService) CreateSignalTypeHandler(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn_slug")
	title := r.FormValue("title")
	schemaURL := r.FormValue("schema_url")
	bumpType := r.FormValue("bump_type")
	readmeURL := r.FormValue("readme_url")
	detail := r.FormValue("detail")

	// Validate user has admin or owner permission
	isnPerm, err := h.AuthService.CheckIsnPermission(r, isnSlug)
	if err != nil {
		component := templates.ErrorAlert("You don't have permission to access this ISN.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	if isnPerm.Permission != "write" {
		component := templates.ErrorAlert("You don't have permission to create signal types in this ISN.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Validate required fields
	if isnSlug == "" || title == "" || schemaURL == "" || bumpType == "" {
		component := templates.ErrorAlert("Please fill in all required fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
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

	// Prepare request
	createReq := client.CreateSignalTypeRequest{
		SchemaURL: schemaURL,
		Title:     title,
		BumpType:  bumpType,
	}

	// Add optional fields if provided
	if readmeURL != "" {
		createReq.ReadmeURL = &readmeURL
	}
	if detail != "" {
		createReq.Detail = &detail
	}

	// Call the API to create the signal type
	response, err := h.ApiClient.CreateSignalType(accessToken, isnSlug, createReq)
	if err != nil {
		reqLogger.Error("Failed to create signal type", slog.String("error", err.Error()))

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
	component := templates.SignalTypeCreationSuccess(*response)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}
