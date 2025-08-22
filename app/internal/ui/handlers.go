package ui

import (
	"net/http"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	// Check if user is authenticated
	if !s.isAuthenticated(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Redirect authenticated users to dashboard
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, redirect to dashboard
	if s.isAuthenticated(r) {
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

	// Set authentication cookie with the JWT token
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
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
	// Clear authentication cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
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
	if !s.isAuthenticated(r) {
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

// Helper methods
func (s *Server) isAuthenticated(r *http.Request) bool {
	cookie, err := r.Cookie("auth_token")
	if err != nil {
		return false
	}

	// Validate JWT token with signalsd API
	if err := s.authService.ValidateToken(cookie.Value); err != nil {
		s.logger.Debug().Err(err).Msg("Token validation failed")
		return false
	}

	return true
}
