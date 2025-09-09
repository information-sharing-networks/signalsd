package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// IsnManagementHandler renders the ISN management page
func (h *HandlerService) IsnManagementHandler(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// currently a single function is supported - create ISN
	component := templates.CreateIsnPage()
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render create ISN page", slog.String("error", err.Error()))
	}
}

// CreateIsnHandler handles the form submission to create a new ISN
// use with RequireAdminOrOwnerRole middleware
func (h *HandlerService) CreateIsnHandler(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	title := r.FormValue("title")
	detail := r.FormValue("detail")
	visibility := r.FormValue("visibility")

	// Validate required fields
	if title == "" || detail == "" || visibility == "" {
		component := templates.ErrorAlert("Please fill in all fields.")
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

	//todo make helper
	// Call the API to create the ISN
	accessToken := accessTokenCookie.Value
	createReq := client.CreateIsnRequest{
		Title:      title,
		Detail:     detail,
		IsInUse:    true,
		Visibility: visibility,
	}

	response, err := h.ApiClient.CreateIsn(accessToken, createReq)
	if err != nil {
		reqLogger.Error("Failed to create ISN", slog.String("error", err.Error()))

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
	component := templates.IsnCreationSuccess(*response)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}
