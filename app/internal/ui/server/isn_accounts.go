package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// ManageIsnAccountsPage godoc
//
//	@Summary		Manage ISN accounts page
//	@Description	Renders the ISN accounts management page. Requires isnadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/isn/accounts/manage [get]
func (s *Server) ManageIsnAccountsPage(w http.ResponseWriter, r *http.Request) {
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
	isns := getIsnOptions(isnPerms, true, false)

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

	// Render admin page
	component := templates.ManageIsnAccountsPage(s.config.Environment, isns, users, serviceAccounts)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render ISN accounts admin page", slog.String("error", err.Error()))
	}
}

// ManageIsnAccounts godoc
//
//	@Summary		Manage ISN accounts
//	@Description	HTMX endpoint. Adds or removes a user/service account from an ISN. Requires isnadmin role.
//	@Tags			HTMX Actions
//	@Param			isn-slug					formData	string	true	"ISN slug"
//	@Param			account-type				formData	string	true	"'user' or 'service-account'"
//	@Param			permission					formData	string	true	"'read', 'write', or 'none' (removes account)"
//	@Param			user-identifier				formData	string	false	"User email (when account-type is 'user')"
//	@Param			service-account-identifier	formData	string	false	"Client ID (when account-type is 'service-account')"
//	@Success		200							"HTML partial"
//	@Router			/ui-api/isn/accounts/manage [put]
func (s *Server) ManageIsnAccounts(w http.ResponseWriter, r *http.Request) {
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
	err := s.apiClient.UpdateIsnAccounts(accessTokenDetails.AccessToken, isnSlug, accountType, accountIdentifier, permission)
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

// TransferOwnershipPage godoc
//
//	@Summary		Transfer ISN ownership page
//	@Description	Renders the ISN ownership transfer page. Requires siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/isn/transfer-ownership [get]
func (s *Server) TransferOwnershipPage(w http.ResponseWriter, r *http.Request) {
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
	isns := getIsnOptions(isnPerms, true, false)

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

	// Render transfer ownership page
	component := templates.TransferOwnershipPage(s.config.Environment, isns, users)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render transfer ownership page", slog.String("error", err.Error()))
	}
}

// TransferOwnership godoc
//
//	@Summary		Transfer ISN ownership
//	@Description	HTMX endpoint. Transfers ownership of an ISN to another user. Requires siteadmin role.
//	@Tags			HTMX Actions
//	@Param			isn-slug		formData	string	true	"ISN slug"
//	@Param			new-owner-email	formData	string	true	"Email of the new owner (must be an existing user)"
//	@Success		200				"HTML partial"
//	@Router			/ui-api/isn/transfer-ownership [put]
func (s *Server) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	newOwnerEmail := r.FormValue("new-owner-email")

	// Validate required fields
	if isnSlug == "" || newOwnerEmail == "" {
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

	// Call the API to transfer ownership
	err := s.apiClient.TransferIsnOwnership(accessTokenDetails.AccessToken, isnSlug, newOwnerEmail)
	if err != nil {
		reqLogger.Error("Failed to transfer ISN ownership", slog.String("error", err.Error()))

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
	msg := fmt.Sprintf("ISN ownership successfully transferred to %s", newOwnerEmail)
	component := templates.SuccessAlert(msg)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
}
