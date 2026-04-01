package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// CreateIsnPage renders the Create ISN page
func (h *HandlerService) CreateIsnPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	component := templates.CreateIsnPage(h.Environment)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render create ISN page", slog.String("error", err.Error()))
	}
}

// CreateIsn handles the form submission to create a new ISN
// use with RequireAdminOrOwnerRole middleware
func (h *HandlerService) CreateIsn(w http.ResponseWriter, r *http.Request) {
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

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Call the API to create the ISN
	req := client.CreateIsnRequest{
		Title:      title,
		Detail:     detail,
		IsInUse:    true,
		Visibility: visibility,
	}

	res, err := h.ApiClient.CreateIsn(accessTokenDetails.AccessToken, req)
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
	component := templates.IsnCreationSuccess(*res)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}

// ManageIsnStatusPage renders the admin ISN enable/disable page.
// note this page only shows ISNs that the user has admin rights for (i.e. they created it or they a site admin)
func (h *HandlerService) ManageIsnStatusPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Inactive ISNs are included in the token claims (IsnPerm.InUse flags the current status).
	// CanAdminister is true for ISNs the user owns and for all ISNs when the user is a siteadmin,
	// so getIsnOptions with filterByIsnAdmin=true gives exactly the right set for this page.
	adminIsns := getIsnOptions(accessTokenDetails.IsnPerms, true, false)

	// Render ISN status management page
	component := templates.ManageIsnStatusPage(h.Environment, adminIsns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render ISN status management page", slog.String("error", err.Error()))
	}
}

// ManageIsnStatus handles the form submission to enable or disable ISNs
func (h *HandlerService) ManageIsnStatus(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	action := r.FormValue("action")

	// Validate required fields
	if isnSlug == "" || action == "" {
		component := templates.ErrorAlert("Please fill in all fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
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

	// Determine the is_in_use value based on action
	var isInUse bool
	switch action {
	case "enable":
		isInUse = true
	case "disable":
		isInUse = false
	default:
		component := templates.ErrorAlert("Invalid action. Please select a valid action.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Call the API to update ISN status
	err := h.ApiClient.UpdateIsnStatus(accessTokenDetails.AccessToken, isnSlug, isInUse)
	if err != nil {
		reqLogger.Error("Failed to update ISN status", slog.String("error", err.Error()))

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
	var successMsg string
	if isInUse {
		successMsg = "ISN enabled successfully"
	} else {
		successMsg = "ISN disabled successfully"
	}

	component := templates.SuccessAlert(successMsg)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}
