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

	if err := templates.GeneratePasswordResetLinkPage(h.Environment, users).Render(r.Context(), w); err != nil {
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

// AdminRoleManagementPage renders the admin role management page
func (h *HandlerService) AdminRoleManagementPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

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

	// Render admin role management page
	component := templates.AdminRoleManagementPage(h.Environment, users)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render admin role management page", slog.String("error", err.Error()))
	}
}

// AccountStatusPage renders the account status management page
func (h *HandlerService) AccountStatusPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

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

	// Render account status management page
	component := templates.AccountStatusPage(h.Environment, users, serviceAccounts)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render account status management page", slog.String("error", err.Error()))
	}
}

// AdminAccountStatus handles the form submission to enable or disable accounts
func (h *HandlerService) AdminAccountStatus(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	accountType := r.FormValue("account-type")
	action := r.FormValue("action")

	// Get account identifier based on account type
	var accountIdentifier string
	switch accountType {
	case "user":
		accountIdentifier = r.FormValue("user-identifier")
	case "service-account":
		accountIdentifier = r.FormValue("service-account-identifier")
	}

	// Validate required fields
	if accountType == "" || accountIdentifier == "" || action == "" {
		component := templates.ErrorAlert("Please fill in all fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to read access token from context", slog.String("component", "handlers.AdminAccountStatus"))

		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Look up the account ID based on account type and identifier
	var accountID string
	var err error

	switch accountType {
	case "user":
		// For users, the identifier is the email
		user, err := h.ApiClient.LookupUserByEmail(accessTokenDetails.AccessToken, accountIdentifier)
		if err != nil {
			reqLogger.Error("Failed to lookup user", slog.String("error", err.Error()))
			component := templates.ErrorAlert("User not found.")
			if renderErr := component.Render(r.Context(), w); renderErr != nil {
				reqLogger.Error("Failed to render error alert", slog.String("error", renderErr.Error()))
			}
			return
		}
		accountID = user.AccountID
	case "service-account":
		serviceAccount, err := h.ApiClient.LookupServiceAccountByClientID(accessTokenDetails.AccessToken, accountIdentifier)
		if err != nil {
			reqLogger.Error("Failed to lookup service account", slog.String("error", err.Error()))
			component := templates.ErrorAlert("Service account not found.")
			if renderErr := component.Render(r.Context(), w); renderErr != nil {
				reqLogger.Error("Failed to render error alert", slog.String("error", renderErr.Error()))
			}
			return
		}
		accountID = serviceAccount.AccountID
	}

	// Call the appropriate API method
	var successMsg string

	switch action {
	case "disable":
		err = h.ApiClient.DisableAccount(accessTokenDetails.AccessToken, accountID)
		successMsg = "Account disabled successfully"
	case "enable":
		err = h.ApiClient.EnableAccount(accessTokenDetails.AccessToken, accountID)
		successMsg = "Account enabled successfully"
	default:
		component := templates.ErrorAlert("Invalid action. Please select a valid action.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	if err != nil {
		reqLogger.Error("Failed to update account status", slog.String("error", err.Error()))

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
	component := templates.SuccessAlert(successMsg)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}

// AdminRoleManagement handles the form submission to grant or revoke admin roles
func (h *HandlerService) AdminRoleManagement(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	userEmail := r.FormValue("user-email")
	action := r.FormValue("action")

	// Validate required fields
	if userEmail == "" || action == "" {
		component := templates.ErrorAlert("Please fill in all fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Validate action
	if action != "grant" && action != "revoke" {
		component := templates.ErrorAlert("Invalid action selected.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Call the appropriate API method
	var err error
	var successMsg string

	if action == "grant" {
		err = h.ApiClient.GrantAdminRole(accessTokenDetails.AccessToken, userEmail)
		successMsg = "Admin role granted successfully to " + userEmail
	} else {
		err = h.ApiClient.RevokeAdminRole(accessTokenDetails.AccessToken, userEmail)
		successMsg = "Admin role revoked successfully from " + userEmail
	}

	if err != nil {
		reqLogger.Error("Failed to update admin role", slog.String("error", err.Error()), slog.String("action", action), slog.String("user_email", userEmail))

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
	component := templates.SuccessAlert(successMsg)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success alert", slog.String("error", err.Error()))
	}
}
