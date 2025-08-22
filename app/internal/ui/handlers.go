package ui

import (
	"fmt"
	"net/http"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	// Check if user is authenticated (with automatic refresh)
	if !s.isAuthenticatedWithRefresh(w, r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Redirect authenticated users to dashboard
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, redirect to dashboard
	if s.isAuthenticatedWithRefresh(w, r) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}

	// Render login page
	component := LoginPage()
	component.Render(r.Context(), w)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	if email == "" || password == "" {
		// Return error fragment for HTMX
		component := LoginError("Email and password are required")
		component.Render(r.Context(), w)
		return
	}

	// Authenticate with signalsd API
	loginResp, err := s.authService.AuthenticateUser(email, password)
	if err != nil {
		s.logger.Error().Err(err).Msg("Authentication failed")
		component := LoginError("Invalid email or password")
		component.Render(r.Context(), w)
		return
	}

	// Set access token cookie (the API automatically sets the refresh token cookie)
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    loginResp.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.Environment == "prod",
		MaxAge:   loginResp.ExpiresIn,
	})

	// Return success response for HTMX
	w.Header().Set("HX-Redirect", "/dashboard")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear access token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.config.Environment == "prod",
	})

	// Clear refresh token cookie (matches signalsd API cookie settings)
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/oauth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.config.Environment == "prod",
	})

	// Redirect to login page
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthenticatedWithRefresh(w, r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Render dashboard page
	component := DashboardPage()
	component.Render(r.Context(), w)
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	// Redirect to the existing swagger documentation on the API server
	docsURL := s.config.APIBaseURL + "/docs"
	http.Redirect(w, r, docsURL, http.StatusSeeOther)
}

func (s *Server) handleSignalSearch(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthenticatedWithRefresh(w, r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get access token for API calls
	accessTokenCookie, err := r.Cookie("access_token")
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get ISNs for the dropdown
	isns, err := s.authService.GetISNs(accessTokenCookie.Value)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to get ISNs")
		// Render search page without ISNs
		component := SignalSearchPage(nil, nil, nil, "")
		component.Render(r.Context(), w)
		return
	}

	// Render search page
	component := SignalSearchPage(isns, nil, nil, "")
	component.Render(r.Context(), w)
}

func (s *Server) handleGetSignalTypes(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthenticatedWithRefresh(w, r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	isnSlug := r.FormValue("isn_slug")
	if isnSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get access token for API calls
	accessTokenCookie, err := r.Cookie("access_token")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Get signal types for the ISN
	signalTypes, err := s.authService.GetSignalTypes(accessTokenCookie.Value, isnSlug)
	if err != nil {
		s.logger.Error().Err(err).Msg("Failed to get signal types")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Render signal types dropdown options
	component := SignalTypeOptions(signalTypes)
	component.Render(r.Context(), w)
}

func (s *Server) handleSearchSignals(w http.ResponseWriter, r *http.Request) {
	if !s.isAuthenticatedWithRefresh(w, r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Parse search parameters
	params := SignalSearchParams{
		ISNSlug:                 r.FormValue("isn_slug"),
		SignalTypeSlug:          r.FormValue("signal_type_slug"),
		SemVer:                  r.FormValue("sem_ver"),
		IsPublic:                r.FormValue("is_public") == "true",
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
		component.Render(r.Context(), w)
		return
	}

	// Get access token for API calls
	var accessToken string
	if !params.IsPublic {
		accessTokenCookie, err := r.Cookie("access_token")
		if err != nil {
			component := SearchError("Authentication required for private ISN search")
			component.Render(r.Context(), w)
			return
		}
		accessToken = accessTokenCookie.Value
	}

	// Perform search
	searchResp, err := s.authService.SearchSignals(accessToken, params)
	if err != nil {
		s.logger.Error().Err(err).Msg("Signal search failed")
		component := SearchError(fmt.Sprintf("Search failed: %v", err))
		component.Render(r.Context(), w)
		return
	}

	// Render search results
	component := SearchResults(searchResp.Signals)
	component.Render(r.Context(), w)
}

// Helper methods

// isAuthenticatedWithRefresh checks authentication and attempts token refresh if needed
func (s *Server) isAuthenticatedWithRefresh(w http.ResponseWriter, r *http.Request) bool {
	accessTokenCookie, err := r.Cookie("access_token")
	if err != nil {
		return false
	}

	// Try to validate current access token
	if err := s.authService.ValidateToken(accessTokenCookie.Value); err == nil {
		return true // Token is valid
	}

	s.logger.Debug().Msg("Access token invalid, attempting refresh")

	// Access token is invalid, try to refresh
	refreshTokenCookie, err := r.Cookie("refresh_token")
	if err != nil {
		s.logger.Debug().Msg("No refresh token cookie found")
		return false
	}

	// Attempt token refresh
	loginResp, err := s.authService.RefreshToken(accessTokenCookie.Value, refreshTokenCookie)
	if err != nil {
		s.logger.Debug().Err(err).Msg("Token refresh failed")
		return false
	}

	// Set new access token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    loginResp.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.config.Environment == "prod",
		MaxAge:   loginResp.ExpiresIn,
	})

	s.logger.Debug().Msg("Token refreshed successfully")
	return true
}
