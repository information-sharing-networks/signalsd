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

// handleLoginPost authenticates the user and adds three cookies to the response:
// - the server generated refresh token cookie
// - a cookie containing the access token provided by the server,
// - a cookie containing the ISN permissions
// - a cookie containg the isn permissions as JSON.
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		// Return error fragment for HTMX
		component := LoginError("Email and password are required")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render login error")
		}
		return
	}

	// Authenticate with signalsd API
	loginResp, refreshTokenCookie, err := s.authService.AuthenticateUser(email, password)
	if err != nil {
		s.logger.Error().Err(err).Msg("Authentication failed")
		component := LoginError("Invalid email or password")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render login error")
		}
		return
	}

	// Set all authentication cookies using shared method
	if err := s.authService.SetAuthCookies(w, loginResp, refreshTokenCookie, s.config.Environment); err != nil {
		s.logger.Error().Err(err).Msg("Failed to set authentication cookies")
		component := LoginError("Login successful but failed to set cookies")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render login error")
		}
		return
	}

	// Return success response for HTMX
	w.Header().Set("HX-Redirect", "/dashboard")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear all authentication cookies using shared method
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
	// Render dashboard page
	component := DashboardPage()
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render dashboard page")
	}
}

func (s *Server) handleSignalSearch(w http.ResponseWriter, r *http.Request) {
	// Get permissions data from cookie
	permsCookie, err := r.Cookie(isnPermsCookieName)
	if err != nil {
		s.logger.Error().Err(err).Msg("No permissions cookie found")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Decode base64 cookie value
	decodedPerms, err := base64.StdEncoding.DecodeString(permsCookie.Value)
	if err != nil {
		s.logger.Error().Err(err).Msgf("Failed to decode permissions cookie: %s", permsCookie.Value)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	var perms map[string]IsnPerms
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		s.logger.Error().Err(err).Msgf("Failed to parse permissions JSON: %s", string(decodedPerms))
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Convert permissions to ISN list for dropdown
	isns := make([]ISN, 0, len(perms))
	for isnSlug := range perms {
		isns = append(isns, ISN{
			Slug:    isnSlug,
			Title:   isnSlug, // Use slug as title for now
			IsInUse: true,
		})
	}

	// Render search page
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

	// Convert to slice
	signalTypes := make([]string, 0, len(signalTypeMap))
	for signalType := range signalTypeMap {
		signalTypes = append(signalTypes, signalType)
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
	versions := make([]string, 0)
	for _, path := range isnPerm.SignalTypePaths {
		// Path format: "signal-type-slug/v1.0.0"
		parts := strings.Split(path, "/v")
		if len(parts) == 2 && parts[0] == signalTypeSlug {
			versions = append(versions, parts[1])
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
		component := SearchError("ISN, Signal Type, and Version are required")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render search error")
		}
		return
	}

	// Get user permissions to validate ISN access and determine visibility
	isnPerm, err := s.getIsnPermissions(r, params.ISNSlug)
	if err != nil {
		component := SearchError(err.Error())
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render search error")
		}
		return
	}

	// Get access token for API calls
	accessTokenCookie, err := r.Cookie(accessTokenCookieName)
	if err != nil {
		component := SearchError("Authentication required")
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render search error")
		}
		return
	}

	// Perform search using ISN visibility to determine endpoint
	searchResp, err := s.apiClient.SearchSignals(accessTokenCookie.Value, params, isnPerm.Visibility)
	if err != nil {
		s.logger.Error().Err(err).Msg("Signal search failed")
		component := SearchError(fmt.Sprintf("Search failed: %v", err))
		if err := component.Render(r.Context(), w); err != nil {
			s.logger.Error().Err(err).Msg("Failed to render search error")
		}
		return
	}

	// Render search results
	component := SearchResults(*searchResp)
	if err := component.Render(r.Context(), w); err != nil {
		s.logger.Error().Err(err).Msg("Failed to render search results")
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
	// Get permissions cookie
	permsCookie, err := r.Cookie(isnPermsCookieName)
	if err != nil {
		return nil, fmt.Errorf("authentication required")
	}

	// Decode permissions
	decodedPerms, err := base64.StdEncoding.DecodeString(permsCookie.Value)
	if err != nil {
		return nil, fmt.Errorf("invalid permissions")
	}

	var perms map[string]IsnPerms
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		return nil, fmt.Errorf("invalid permissions format")
	}

	// Validate user has access to this ISN
	isnPerm, exists := perms[isnSlug]
	if !exists {
		return nil, fmt.Errorf("no permission for this ISN")
	}

	return &isnPerm, nil
}
