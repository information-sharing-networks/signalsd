package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
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
		templ.Handler(templates.ErrorAlert("You don't have permission to access any ISNs.")).ServeHTTP(w, r)
		return
	}
	// Convert permissions to ISN list for dropdown (only ISNs where user has admin rights)
	isns := getIsnOptions(isnPerms, true, false)

	// Fetch users for dropdown
	rawUsers, err := s.apiClient.GetUsers(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get users list", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("Failed to load users. Please try again.")).ServeHTTP(w, r)
		return
	}

	// Fetch service accounts for dropdown
	rawServiceAccounts, err := s.apiClient.GetServiceAccounts(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get service accounts list", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("Failed to load service accounts. Please try again.")).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.ManageIsnAccountsPage(s.config.Environment, isns, getUserOptions(rawUsers, accessTokenDetails.Email), getServiceAccountOptions(rawServiceAccounts))).ServeHTTP(w, r)
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
//	@Failure		400							"HTML error partial"
//	@Failure		401							"HTML error partial"
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
		templ.Handler(templates.ErrorAlert("Please fill in all fields.")).ServeHTTP(w, r)
		return
	}

	// Validate account type
	if !signalsd.ValidAccountTypes[accountType] {
		templ.Handler(templates.ErrorAlert(fmt.Sprintf("Invalid account type selected: %v", accountType))).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		// unexpected - this should be caught by middleware check on ISN access
		reqLogger.Error("Failed to read access token from context", slog.String("component", "templates.handleAddIsnAccount"))
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
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

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	var msg string
	if permission == "none" {
		msg = "Account successfully removed from ISN"
	} else {
		msg = "Account successfully added to ISN"
	}

	templ.Handler(templates.SuccessAlert(msg)).ServeHTTP(w, r)
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
		templ.Handler(templates.ErrorAlert("You don't have permission to access any ISNs.")).ServeHTTP(w, r)
		return
	}

	// Convert permissions to ISN list for dropdown (only ISNs where user has admin rights)
	isns := getIsnOptions(isnPerms, true, false)

	// Fetch users for dropdown
	rawUsers, err := s.apiClient.GetUsers(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get users list", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("Failed to load users. Please try again.")).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.TransferOwnershipPage(s.config.Environment, isns, getUserOptions(rawUsers, accessTokenDetails.Email))).ServeHTTP(w, r)
}

// TransferOwnership godoc
//
//	@Summary		Transfer ISN ownership
//	@Description	HTMX endpoint. Transfers ownership of an ISN to another user. Requires siteadmin role.
//	@Tags			HTMX Actions
//	@Param			isn-slug		formData	string	true	"ISN slug"
//	@Param			new-owner-email	formData	string	true	"Email of the new owner (must be an existing user)"
//	@Success		200				"HTML partial"
//	@Failure		400				"HTML error partial"
//	@Failure		401				"HTML error partial"
//	@Router			/ui-api/isn/transfer-ownership [put]
func (s *Server) TransferOwnership(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	newOwnerEmail := r.FormValue("new-owner-email")

	// Validate required fields
	if isnSlug == "" || newOwnerEmail == "" {
		templ.Handler(templates.ErrorAlert("Please fill in all fields.")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
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

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.SuccessAlert(fmt.Sprintf("ISN ownership successfully transferred to %s", newOwnerEmail))).ServeHTTP(w, r)
}
