package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// renderError displays an error message inline to the user
func (s *Server) renderError(w http.ResponseWriter, message string) {
	component := AccessDeniedAlert(message)
	if err := component.Render(context.Background(), w); err != nil {
		s.logger.Error("Failed to render error", slog.String("error", err.Error()))
	}
}

// handleHome handles the root path and redirects to the dashboard if authenticated, login if not
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
		s.logger.Error("Failed to render login page", slog.String("error", err.Error()))
	}
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// Render registration page
	component := RegisterPage()
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error("Failed to render registration page", slog.String("error", err.Error()))
	}
}

// handleLoginPost authenticates the user and adds authentication cookies to the response
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		s.renderError(w, "Email and password are required")
		return
	}
	// Authenticate with signalsd API
	loginResp, refreshTokenCookie, err := s.authService.AuthenticateUser(email, password)
	if err != nil {
		s.logger.Error("Authentication failed", slog.String("error", err.Error()))
		s.renderError(w, err.Error())
		return
	}

	// Set all authentication cookies using shared method
	if err := s.authService.SetAuthCookies(w, loginResp, refreshTokenCookie, s.config.Environment); err != nil {
		s.logger.Error("Failed to set authentication cookies", slog.String("error", err.Error()))
		s.renderError(w, "System error: authentication failed")
		return
	}

	// Login successful - redirect to dashboard
	w.Header().Set("HX-Redirect", "/dashboard")
	w.WriteHeader(http.StatusOK)
}

// handleRegisterPost processes user registration
func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if email == "" || password == "" || confirmPassword == "" {
		s.renderError(w, "All fields are required")
		return
	}

	if password != confirmPassword {
		s.renderError(w, "Passwords do not match")
		return
	}

	// Register user with signalsd API
	err := s.apiClient.RegisterUser(email, password)
	if err != nil {
		s.logger.Error("Registration failed", slog.String("error", err.Error()))
		s.renderError(w, err.Error())
		return
	}

	// Registration successful - show success message and redirect to login after delay
	w.Header().Set("HX-Trigger-After-Settle", "registrationSuccess")
	component := RegistrationSuccess()
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error("Failed to render registration success", slog.String("error", err.Error()))
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
	component := DashboardPage()
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error("Failed to render dashboard page", slog.String("error", err.Error()))
	}
}

// handleSignalSearch renders the signal search page
// ISN access is validated by RequireIsnAccess middleware
func (s *Server) handleSignalSearch(w http.ResponseWriter, r *http.Request) {
	// Get ISN permissions from cookie - middleware ensures this exists
	perms := s.getIsnPermsFromCookie(r)

	s.logger.Debug("Handling signals search page")
	// Convert permissions to ISN list for dropdown
	isns := make([]IsnDropdown, 0, len(perms))
	for isnSlug := range perms {
		isns = append(isns, IsnDropdown{
			Slug:    isnSlug,
			IsInUse: true,
		})
	}

	// Render search page
	component := SignalSearchPage(isns, perms, nil)
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error("Failed to render signal search page", slog.String("error", err.Error()))
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
		s.logger.Error("Failed to decode permissions cookie in signal types handler",
			slog.String("error", err.Error()),
			slog.String("cookie_value", permsCookie.Value))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var perms map[string]IsnPerms
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		s.logger.Error("Failed to parse permissions JSON in signal types handler",
			slog.String("error", err.Error()),
			slog.String("json_data", string(decodedPerms)))
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
		s.logger.Error("Failed to render signal type options", slog.String("error", err.Error()))
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
		s.logger.Error("Failed to decode permissions cookie in versions handler",
			slog.String("error", err.Error()),
			slog.String("cookie_value", permsCookie.Value))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var perms map[string]IsnPerms
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		s.logger.Error("Failed to parse permissions JSON in versions handler",
			slog.String("error", err.Error()),
			slog.String("json_data", string(decodedPerms)))
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
		s.logger.Error("Failed to render version options", slog.String("error", err.Error()))
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
		s.renderError(w, "ISN, Signal Type, and Version are required")
		return
	}

	// Get user permissions to validate ISN access and determine visibility
	isnPerm, err := s.getIsnPermissions(r, params.ISNSlug)
	if err != nil {
		s.renderError(w, err.Error())
		return
	}

	// Get access token from cookie
	accessTokenCookie, err := r.Cookie(accessTokenCookieName)
	if err != nil {
		s.logger.Error("Access token not found", slog.String("error", err.Error()))
		s.renderError(w, "Session expired - please refresh the page and try again")
		return
	}
	accessToken := accessTokenCookie.Value

	// Perform search using ISN visibility to determine endpoint
	searchResp, err := s.apiClient.SearchSignals(accessToken, params, isnPerm.Visibility)
	if err != nil {
		s.logger.Error("Signal search failed", slog.String("error", err.Error()))
		s.renderError(w, err.Error())
		return
	}

	// Render search results
	component := SearchResults(*searchResp)
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error("Failed to render search results", slog.String("error", err.Error()))
	}
}

// handleAdminDashboard renders the main admin dashboard page
// Access control is handled by RequireAdminAccess middleware
func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	// Render admin dashboard - access is validated by middleware
	component := AdminDashboardPage()
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error("Failed to render admin dashboard", slog.String("error", err.Error()))
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
		s.logger.Error("Failed to render ISN accounts admin page", slog.String("error", err.Error()))
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
		s.renderError(w, "All fields are required")
		return
	}

	// Get access token from cookie
	accessTokenCookie, err := r.Cookie(accessTokenCookieName)
	if err != nil {
		s.renderError(w, "Authentication required")
		return
	}
	accessToken := accessTokenCookie.Value

	// Call the API to add the account to the ISN
	err = s.apiClient.AddAccountToIsn(accessToken, isnSlug, accountEmail, permission)
	if err != nil {
		s.logger.Info("Failed to add account to ISN", slog.String("error", err.Error()))
		s.renderError(w, err.Error())
		return
	}

	// Success response
	component := SuccessAlert("Account successfully added to ISN")
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error("Failed to render success message", slog.String("error", err.Error()))
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

// handleAccessDenied handles access denied for both HTMX and direct requests
// Always redirects to an access denied page for consistent UX
func (s *Server) handleAccessDenied(w http.ResponseWriter, r *http.Request, pageTitle, message string) {
	if r.Header.Get("HX-Request") == "true" {
		// HTMX request - redirect to access denied page
		w.Header().Set("HX-Redirect", "/access-denied?title="+pageTitle+"&message="+message)
	} else {
		// Direct navigation - render access denied page
		component := AccessDeniedPage(pageTitle, message)
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error("Failed to render access denied page", slog.String("error", err.Error()))
		}
	}
}
