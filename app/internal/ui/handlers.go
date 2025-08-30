package ui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	status := s.authService.CheckTokenStatus(r)

	switch status {
	case TokenValid:
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	case TokenExpired, TokenInvalid, TokenMissing:
		s.redirectToLogin(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Render login page
	component := LoginPage()
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render login page")
	}
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Render registration page
	component := RegisterPage()
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render registration page")
	}
}

// handleLoginPost authenticates the user and adds three cookies to the response:
// - the server generated refresh token cookie
// - a cookie containing the access token provided by the server,
// - a cookie containg the isn permissions as JSON.
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		// Return error fragment for HTMX
		component := AuthError("Email and password are required")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render login error")
		}
		return
	}

	// Authenticate with signalsd API
	loginResp, refreshTokenCookie, err := s.authService.AuthenticateUser(email, password)
	if err != nil {
		s.logger.Error().Err(err).Msg("Authentication failed")
		component := AuthError("Invalid email or password")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render login error")
		}
		return
	}

	// Set all authentication cookies using shared method
	if err := s.authService.SetAuthCookies(w, loginResp, refreshTokenCookie, s.config.Environment); err != nil {
		s.logger.Error().Err(err).Msg("Failed to set authentication cookies")
		component := AuthError("System error: authentication failed")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render login error")
		}
		return
	}

	w.Header().Set("HX-Redirect", "/dashboard")
	w.WriteHeader(http.StatusOK)
}

// handleRegisterPost processes user registration
func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if email == "" || password == "" || confirmPassword == "" {
		// Return error fragment for HTMX
		component := AuthError("All fields are required")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render registration error")
		}
		return
	}

	if password != confirmPassword {
		component := AuthError("Passwords do not match")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render registration error")
		}
		return
	}

	// Register user with signalsd API
	err := s.apiClient.RegisterUser(email, password)
	if err != nil {
		s.logger.Error().Err(err).Msg("Registration failed")

		component := AuthError(err.Error())
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render registration error")
		}
		return
	}

	// Registration successful - show success message and redirect to login after delay
	w.Header().Set("HX-Trigger-After-Settle", "registrationSuccess")
	component := RegistrationSuccess()
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render registration success")
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.authService.ClearAuthCookies(w, s.config.Environment)

	// Redirect to login page
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	var errorMsg string

	// Check for error messages from redirects
	errorParam := r.URL.Query().Get("error")
	switch errorParam {
	case "admin_access_denied":
		errorMsg = "You do not have permission to access the admin dashboard"
	}

	component := DashboardPage(errorMsg)
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render dashboard page")
	}
}

func (s *Server) handleSignalSearch(w http.ResponseWriter, r *http.Request) {
	// Initialize empty permissions and ISN list
	var perms map[string]IsnPerms = make(map[string]IsnPerms)
	var isns []IsnDropdown

	// Try to get permissions cookie - if it doesn't exist or is invalid,
	// we'll show the search page with no ISNs (triggering the notification)
	permsCookie, err := r.Cookie(isnPermsCookieName)
	if err != nil {
		s.logger.Info().Msg("No permissions cookie found - user has no ISN access")
	} else {
		// Decode base64 cookie value
		decodedPerms, err := base64.StdEncoding.DecodeString(permsCookie.Value)
		if err != nil {
			s.logger.Error().Err(err).Msgf("Failed to decode permissions cookie: %s", permsCookie.Value)
		} else {
			// Parse permissions JSON
			if err := json.Unmarshal(decodedPerms, &perms); err != nil {
				s.logger.Error().Err(err).Msgf("Failed to parse permissions JSON: %s", string(decodedPerms))
				perms = make(map[string]IsnPerms) // Reset to empty map on error
			}
		}
	}

	// Convert permissions to ISN list for dropdown
	isns = make([]IsnDropdown, 0, len(perms))
	for isnSlug := range perms {
		isns = append(isns, IsnDropdown{
			Slug:    isnSlug,
			IsInUse: true,
		})
	}

	// Render search page (will show notification if len(isns) == 0)
	component := SignalSearchPage(isns, perms, nil, "")
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render signal search page")
	}
}

func (s *Server) handleGetSignalTypes(w http.ResponseWriter, r *http.Request) {
	isnSlug := r.FormValue("isn_slug")
	if isnSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get permissions data from cookie
	permsCookie, err := r.Cookie(isnPermsCookieName)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Decode base64 cookie value
	decodedPerms, err := base64.StdEncoding.DecodeString(permsCookie.Value)
	if err != nil {
		s.logger.Error().Err(err).Msgf("Failed to decode permissions cookie in signal types handler: %s", permsCookie.Value)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var perms map[string]IsnPerms
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		s.logger.Error().Err(err).Msgf("Failed to parse permissions JSON in signal types handler: %s", string(decodedPerms))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Get signal types for the selected ISN
	isnPerm, exists := perms[isnSlug]
	if !exists {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Parse signal type paths to extract unique signal types
	signalTypeMap := make(map[string]bool)
	for _, path := range isnPerm.SignalTypePaths {
		// Path format: "signal-type-slug/v1.0.0"
		parts := strings.Split(path, "/v")
		if len(parts) == 2 {
			signalTypeMap[parts[0]] = true
		}
	}

	// Convert to slice of SignalTypeDropdown
	signalTypes := make([]SignalTypeDropdown, 0, len(signalTypeMap))
	for signalType := range signalTypeMap {
		signalTypes = append(signalTypes, SignalTypeDropdown{
			Slug: signalType,
		})
	}

	// Render signal types dropdown options
	component := SignalTypeOptions(signalTypes)
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render signal type options")
	}
}

func (s *Server) handleGetSignalVersions(w http.ResponseWriter, r *http.Request) {
	isnSlug := r.FormValue("isn_slug")
	signalTypeSlug := r.FormValue("signal_type_slug")
	if isnSlug == "" || signalTypeSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get permissions data from cookie
	permsCookie, err := r.Cookie(isnPermsCookieName)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Decode base64 cookie value
	decodedPerms, err := base64.StdEncoding.DecodeString(permsCookie.Value)
	if err != nil {
		s.logger.Error().Err(err).Msgf("Failed to decode permissions cookie in versions handler: %s", permsCookie.Value)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var perms map[string]IsnPerms
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		s.logger.Error().Err(err).Msgf("Failed to parse permissions JSON in versions handler: %s", string(decodedPerms))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Get signal types for the selected ISN
	isnPerm, exists := perms[isnSlug]
	if !exists {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Find versions for the specific signal type
	versions := make([]VersionDropdown, 0)
	for _, path := range isnPerm.SignalTypePaths {
		// Path format: "signal-type-slug/v1.0.0"
		parts := strings.Split(path, "/v")
		if len(parts) == 2 && parts[0] == signalTypeSlug {
			versions = append(versions, VersionDropdown{
				Version: parts[1],
			})
		}
	}

	// Render version dropdown options
	component := VersionOptions(versions)
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render version options")
	}
}

func (s *Server) handleSearchSignals(w http.ResponseWriter, r *http.Request) {
	// Parse search parameters
	params := SignalSearchParams{
		ISNSlug:                 r.FormValue("isn_slug"),
		SignalTypeSlug:          r.FormValue("signal_type_slug"),
		SemVer:                  r.FormValue("sem_ver"),
		StartDate:               r.FormValue("start_date"),
		EndDate:                 r.FormValue("end_date"),
		AccountID:               r.FormValue("account_id"),
		SignalID:                r.FormValue("signal_id"),
		LocalRef:                r.FormValue("local_ref"),
		IncludeWithdrawn:        r.FormValue("include_withdrawn") == "true",
		IncludeCorrelated:       r.FormValue("include_correlated") == "true",
		IncludePreviousVersions: r.FormValue("include_previous_versions") == "true",
	}

	// Validate required parameters
	if params.ISNSlug == "" || params.SignalTypeSlug == "" || params.SemVer == "" {
		component := ErrorAlert("ISN, Signal Type, and Version are required")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render error")
		}
		return
	}

	// Get user permissions to validate ISN access and determine visibility
	isnPerm, err := s.getIsnPermissions(r, params.ISNSlug)
	if err != nil {
		component := ErrorAlert(err.Error())
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render error")
		}
		return
	}

	// Get access token from cookie
	accessTokenCookie, err := r.Cookie(accessTokenCookieName)
	if err != nil {
		component := ErrorAlert("Internal error - access token not found, please login again")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render error")
		}
		return
	}
	accessToken := accessTokenCookie.Value

	// Perform search using ISN visibility to determine endpoint
	searchResp, err := s.apiClient.SearchSignals(accessToken, params, isnPerm.Visibility)
	if err != nil {
		s.logger.Error().Err(err).Msg("Signal search failed")
		component := ErrorAlert(fmt.Sprintf("Search failed: %v", err))
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render error")
		}
		return
	}

	// Render search results
	component := SearchResults(*searchResp)
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render search results")
	}
}

// handleAdminDashboard renders the main admin dashboard page
func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	// Get user account info from cookie
	accountInfo := s.getAccountInfoFromCookie(r)
	if accountInfo == nil {
		component := ErrorAlert("Internal error - account info not found, please login again")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render error")
		}
		return
	}

	if accountInfo.Role != "owner" && accountInfo.Role != "admin" {
		// Redirect back to main dashboard with error message
		http.Redirect(w, r, "/dashboard?error=admin_access_denied", http.StatusSeeOther)
		return
	}

	// Render admin dashboard (no error message needed - access is validated above)
	component := AdminDashboardPage("")
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render admin dashboard")
	}
}

// handleIsnAccountsAdmin renders the ISN accounts administration page
func (s *Server) handleIsnAccountsAdmin(w http.ResponseWriter, r *http.Request) {
	// Get user permissions from cookie
	perms := s.getIsnPermsFromCookie(r)
	var isns []IsnDropdown

	// Convert permissions to ISN list for dropdown (only ISNs where user has admin rights)
	isns = make([]IsnDropdown, 0, len(perms))
	for isnSlug, perm := range perms {
		// Only show ISNs where user has write permission (admins/owners)
		if perm.Permission == "write" {
			isns = append(isns, IsnDropdown{
				Slug:    isnSlug,
				IsInUse: true,
			})
		}
	}

	// Render admin page
	component := IsnAccountsAdminPage(isns)
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render ISN accounts admin page")
	}
}

// handleAddIsnAccount handles the form submission to add an account to an ISN
func (s *Server) handleAddIsnAccount(w http.ResponseWriter, r *http.Request) {
	// Parse form data
	isnSlug := r.FormValue("isn_slug")
	accountEmail := r.FormValue("account_email")
	permission := r.FormValue("permission")

	// Validate required fields
	if isnSlug == "" || accountEmail == "" || permission == "" {
		component := ErrorAlert("All fields are required")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render error")
		}
		return
	}

	// Get access token from cookie
	accessTokenCookie, err := r.Cookie(accessTokenCookieName)
	if err != nil {
		component := ErrorAlert("Authentication required")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Info().Err(err).Msg("Failed to render error")
		}
		return
	}
	accessToken := accessTokenCookie.Value

	// Call the API to add the account to the ISN
	err = s.apiClient.AddAccountToIsn(accessToken, isnSlug, accountEmail, permission)
	if err != nil {
		s.logger.Info().Err(err).Msg("Failed to add account to ISN")
		component := ErrorAlert(fmt.Sprintf("Failed to add account to ISN: %v", err))
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render error")
		}
		return
	}

	// Success response
	component := SuccessAlert("Account successfully added to ISN")
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render success message")
	}
}

// Helper method for redirecting to login
func (s *Server) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

// getIsnPermissions validates user has access to the ISN and returns the ISN permissions
func (s *Server) getIsnPermissions(r *http.Request, isnSlug string) (*IsnPerms, error) {
	// Get permissions from cookie
	perms := s.getIsnPermsFromCookie(r)
	if len(perms) == 0 {
		return nil, fmt.Errorf("authentication required")
	}

	// Validate user has access to this ISN
	isnPerm, exists := perms[isnSlug]
	if !exists {
		return nil, fmt.Errorf("no permission for this ISN")
	}

	return &isnPerm, nil
}
