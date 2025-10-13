package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// UpdateIsnAccountsPage renders the ISN accounts administration page
func (h *HandlerService) UpdateIsnAccountsPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	isnPerms := accessTokenDetails.IsnPerms

	if len(isnPerms) == 0 {
		component := templates.ErrorAlert("You don't have permission to access any ISNs.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}
	// Convert permissions to ISN list for dropdown (only ISNs where user has admin rights)
	isns := h.getIsnOptions(isnPerms, true, false)

	// Fetch users for dropdown
	users, err := h.ApiClient.GetUserOptionsList(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get users list", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load users. Please try again.")
		if renderErr := component.Render(r.Context(), w); renderErr != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", renderErr.Error()))
		}
		return
	}

	// Fetch service accounts for dropdown
	serviceAccounts, err := h.ApiClient.GetServiceAccountOptionsList(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get service accounts list", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load service accounts. Please try again.")
		if renderErr := component.Render(r.Context(), w); renderErr != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", renderErr.Error()))
		}
		return
	}

	// Render admin page
	component := templates.IsnAccountManagementPage(isns, users, serviceAccounts)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render ISN accounts admin page", slog.String("error", err.Error()))
	}
}

// UpdateIsnAccounts handles the form submission to add an account to an ISN
func (h *HandlerService) UpdateIsnAccounts(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	accountType := r.FormValue("account-type")
	permission := r.FormValue("permission")

	// Get account identifier based on account type
	var accountIdentifier string
	switch accountType {
	case "user":
		accountIdentifier = r.FormValue("user-identifier")
	case "service-account":
		accountIdentifier = r.FormValue("service-account-identifier")
	}

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
		component := templates.ErrorAlert(fmt.Sprintf("Invalid account type selected: %v", accountType))
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		// unexpected - this should be caught by middleware check on ISN access
		reqLogger.Error("Failed to read access token from context", slog.String("component", "templates.handleAddIsnAccount"))

		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Call the API to add the account to the ISN
	err := h.ApiClient.UpdateIsnAccounts(accessTokenDetails.AccessToken, isnSlug, accountType, accountIdentifier, permission)
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
