package server

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// SettingsPage godoc
//
//	@Summary		Account settings page
//	@Description	Renders the user account settings page. Requires authentication.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/settings [get]
func (s *Server) SettingsPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	component := templates.SettingsPage(s.config.Environment)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render settings page", slog.String("error", err.Error()))
	}
}

// UpdatePassword godoc
//
//	@Summary		Update password
//	@Description	HTMX endpoint. Changes the authenticated user's password.
//	@Tags			HTMX Actions
//	@Param			current-password	formData	string	true	"Current password"
//	@Param			new-password		formData	string	true	"New password"
//	@Param			confirm-password	formData	string	true	"Confirm new password"
//	@Success		200					"HTML partial"
//	@Router			/ui-api/account/password [put]
func (s *Server) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	currentPassword := r.FormValue("current-password")
	newPassword := r.FormValue("new-password")
	confirmPassword := r.FormValue("confirm-password")

	if currentPassword == "" || newPassword == "" || confirmPassword == "" {
		component := templates.ErrorAlert("Please fill in all fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Validate passwords match
	if newPassword != confirmPassword {
		component := templates.ErrorAlert("New passwords do not match.")
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

	// Call the API to update password
	err := s.apiClient.UpdatePassword(accessTokenDetails.AccessToken, currentPassword, newPassword)
	if err != nil {
		reqLogger.Error("Failed to update password", slog.String("error", err.Error()))

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
	component := templates.SuccessAlert("Password updated successfully. Your new password is now active.")
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}

// GeneratePasswordResetLinkPage godoc
//
//	@Summary		Generate password reset link page
//	@Description	Renders the password reset link generation form. Requires isnadmin or siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/users/generate-password-reset-link [get]
func (s *Server) GeneratePasswordResetLinkPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		s.renderErrorAlert(w, r, "Authentication required. Please log in again.", "Failed to get accessTokenDetails from context in GeneratePasswordResetLinkPage")
		return
	}

	// Get users from API
	rawUsers, err := s.apiClient.GetUsers(accessTokenDetails.AccessToken)
	if err != nil {
		s.renderErrorAlert(w, r, "Failed to load users. Please try again.", "Failed to get users: "+err.Error())
		return
	}
	users := make([]types.UserOption, len(rawUsers))
	for i, u := range rawUsers {
		users[i] = types.UserOption{Email: u.Email, UserRole: u.UserRole}
	}

	if err := templates.GeneratePasswordResetLinkPage(s.config.Environment, users).Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render manage users page", slog.String("error", err.Error()))
	}
}

// GeneratePasswordResetLink godoc
//
//	@Summary		Generate password reset link
//	@Description	HTMX endpoint. Generates a one-time password reset URL for a user. Requires isnadmin or siteadmin role.
//	@Tags			HTMX Actions
//	@Param			user-dropdown	formData	string	true	"Selected user in 'email|role' format"
//	@Success		200				"HTML partial"
//	@Router			/ui-api/users/generate-password-reset-link [put]
func (s *Server) GeneratePasswordResetLink(w http.ResponseWriter, r *http.Request) {
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

	res, err := s.apiClient.GeneratePasswordResetLink(accessTokenDetails.AccessToken, email)
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

// ManageIsnAdminRolesPage godoc
//
//	@Summary		Manage ISN admin roles page
//	@Description	Renders the ISN admin role management form. Requires siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/accounts/isn-admins/manage [get]
func (s *Server) ManageIsnAdminRolesPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Fetch users for dropdown
	rawUsers, err := s.apiClient.GetUsers(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get users list", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load users. Please try again.")
		if renderErr := component.Render(r.Context(), w); renderErr != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", renderErr.Error()))
		}
		return
	}
	users := make([]types.UserOption, len(rawUsers))
	for i, u := range rawUsers {
		users[i] = types.UserOption{Email: u.Email, UserRole: u.UserRole}
	}

	// Render isn admin role management page
	component := templates.ManageIsnAdminRolesPage(s.config.Environment, users)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render isn admin role management page", slog.String("error", err.Error()))
	}
}

// ManageAccountStatusPage godoc
//
//	@Summary		Manage account status page
//	@Description	Renders the account enable/disable form for users and service accounts. Requires isnadmin or siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/accounts/manage [get]
func (s *Server) ManageAccountStatusPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Fetch users for dropdown
	rawUsers, err := s.apiClient.GetUsers(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get users list", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load users. Please try again.")
		if renderErr := component.Render(r.Context(), w); renderErr != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", renderErr.Error()))
		}
		return
	}
	users := make([]types.UserOption, len(rawUsers))
	for i, u := range rawUsers {
		users[i] = types.UserOption{Email: u.Email, UserRole: u.UserRole}
	}

	// Fetch service accounts for dropdown
	rawServiceAccounts, err := s.apiClient.GetServiceAccounts(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get service accounts list", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load service accounts. Please try again.")
		if renderErr := component.Render(r.Context(), w); renderErr != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", renderErr.Error()))
		}
		return
	}
	serviceAccounts := make([]types.ServiceAccountOption, len(rawServiceAccounts))
	for i, sa := range rawServiceAccounts {
		serviceAccounts[i] = types.ServiceAccountOption{
			ClientOrganization: sa.ClientOrganization,
			ClientContactEmail: sa.ClientContactEmail,
			ClientID:           sa.ClientID,
		}
	}

	// Render account status management page
	component := templates.ManageAccountStatusPage(s.config.Environment, users, serviceAccounts)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render account status management page", slog.String("error", err.Error()))
	}
}

// ManageAccountStatus godoc
//
//	@Summary		Enable or disable an account
//	@Description	HTMX endpoint. Enables or disables a user or service account. Requires isnadmin or siteadmin role.
//	@Tags			HTMX Actions
//	@Param			account-type				formData	string	true	"'user' or 'service-account'"
//	@Param			action						formData	string	true	"'enable' or 'disable'"
//	@Param			user-identifier				formData	string	false	"User email (when account-type is 'user')"
//	@Param			service-account-identifier	formData	string	false	"Client ID (when account-type is 'service-account')"
//	@Success		200							"HTML partial"
//	@Router			/ui-api/accounts/manage [put]
func (s *Server) ManageAccountStatus(w http.ResponseWriter, r *http.Request) {
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
		user, err := s.apiClient.LookupUserByEmail(accessTokenDetails.AccessToken, accountIdentifier)
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
		serviceAccount, err := s.apiClient.LookupServiceAccountByClientID(accessTokenDetails.AccessToken, accountIdentifier)
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
		err = s.apiClient.DisableAccount(accessTokenDetails.AccessToken, accountID)
		successMsg = "Account disabled successfully"
	case "enable":
		err = s.apiClient.EnableAccount(accessTokenDetails.AccessToken, accountID)
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

// MangeSiteAdminRolesPage godoc
//
//	@Summary		Manage site admin roles page
//	@Description	Renders the site admin role management form. Requires siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/accounts/site-admins/manage [get]
func (s *Server) MangeSiteAdminRolesPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Fetch users for dropdown
	rawUsers, err := s.apiClient.GetUsers(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get users list", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load users. Please try again.")
		if renderErr := component.Render(r.Context(), w); renderErr != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", renderErr.Error()))
		}
		return
	}
	users := make([]types.UserOption, len(rawUsers))
	for i, u := range rawUsers {
		users[i] = types.UserOption{Email: u.Email, UserRole: u.UserRole}
	}

	// Render site admin role management page
	component := templates.MangeSiteAdminRolesPage(s.config.Environment, users)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render site admin role management page", slog.String("error", err.Error()))
	}
}

// ManageAdminRoles godoc
//
//	@Summary		Grant or revoke ISN admin role
//	@Description	HTMX endpoint. Grants or revokes the isnadmin role for a user. Requires siteadmin role.
//	@Tags			HTMX Actions
//	@Param			user-email	formData	string	true	"User email"
//	@Param			action		formData	string	true	"'grant' or 'revoke'"
//	@Success		200			"HTML partial"
//	@Router			/ui-api/accounts/isn-admins/manage [put]
func (s *Server) ManageAdminRoles(w http.ResponseWriter, r *http.Request) {
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
		err = s.apiClient.GrantAdminRole(accessTokenDetails.AccessToken, userEmail)
		successMsg = "Admin role granted successfully to " + userEmail
	} else {
		err = s.apiClient.RevokeAdminRole(accessTokenDetails.AccessToken, userEmail)
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

// ManageSiteAdminRoles godoc
//
//	@Summary		Grant or revoke site admin role
//	@Description	HTMX endpoint. Grants or revokes the siteadmin role for a user. Requires siteadmin role.
//	@Tags			HTMX Actions
//	@Param			user-email	formData	string	true	"User email"
//	@Param			action		formData	string	true	"'grant' or 'revoke'"
//	@Success		200			"HTML partial"
//	@Router			/ui-api/accounts/site-admins/manage [put]
func (s *Server) ManageSiteAdminRoles(w http.ResponseWriter, r *http.Request) {
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

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		component := templates.ErrorAlert("An error occurred. Please try again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Call the appropriate API method
	var err error
	var successMsg string

	if action == "grant" {
		err = s.apiClient.GrantSiteAdminRole(accessTokenDetails.AccessToken, userEmail)
		successMsg = "Site admin role granted successfully to " + userEmail
	} else {
		err = s.apiClient.RevokeSiteAdminRole(accessTokenDetails.AccessToken, userEmail)
		successMsg = "Site admin role revoked successfully from " + userEmail
	}

	if err != nil {
		reqLogger.Error("Failed to update site admin role", slog.String("error", err.Error()), slog.String("action", action), slog.String("user_email", userEmail))

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
