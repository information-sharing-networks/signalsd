package ui

import (
	"log/slog"
	"net/http"

	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// RequireAuth is middleware that checks authentication and attempts token refresh if needed.
//
// This middleware ensures that:
// 1. All requests have valid authentication (redirects to login if not)
// 2. Expired tokens are automatically refreshed when possible
// 3. Fresh cookies are set after token refresh for subsequent requests
//
// Handlers can read auth data directly from cookies using helper methods.
func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := s.authService.CheckTokenStatus(r)

		switch status {
		case TokenValid:
			// Authentication is valid, proceed to handler
			next.ServeHTTP(w, r)
			return
		case TokenMissing, TokenInvalid:
			s.redirectToLogin(w, r)
			return
		case TokenExpired:
			// attempt refresh
			refreshTokenCookie, err := r.Cookie(signalsd.RefreshTokenCookieName)
			if err != nil {
				s.logger.Error("Failed to get refresh token cookie", slog.String("error", err.Error()))
				s.redirectToLogin(w, r)
				return
			}

			loginResp, newRefreshTokenCookie, err := s.authService.RefreshToken(refreshTokenCookie)
			if err != nil {
				s.logger.Error("Token refresh failed", slog.String("error", err.Error()))
				s.redirectToLogin(w, r)
				return
			}

			if err := s.authService.SetAuthCookies(w, loginResp, newRefreshTokenCookie, s.config.Environment); err != nil {
				s.logger.Error("Failed to set authentication cookies after refresh", slog.String("error", err.Error()))
				s.redirectToLogin(w, r)
				return
			}

			// Continue to handler with fresh cookies set
			next.ServeHTTP(w, r)
		}
	})
}

// RequireAdminAccess is middleware that checks if user has admin/owner role
func (s *Server) RequireAdminAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accountInfo, err := s.getAccountInfoFromCookie(r)
		if err != nil {
			s.logger.Error("Could not get accountInfo from Cookie")
			s.handleAccessDenied(w, r, "Admin Dashboard", "Internal error - account info not found, please login again")
			return
		}

		if accountInfo.Role != "owner" && accountInfo.Role != "admin" {
			s.logger.Info("User attempted to access admin area without permission",
				slog.String("account_id", accountInfo.AccountID))
			s.handleAccessDenied(w, r, "Admin Dashboard", "You do not have permission to access the admin dashboard")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireIsnAccess is middleware that checks if user has access to any ISNs
func (s *Server) RequireIsnAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := r.Cookie(isnPermsCookieName)
		if err != nil {
			// No ISN permissions cookie = no ISN access
			s.logger.Info("User attempted to access ISN features without ISN permissions")
			s.handleAccessDenied(w, r, "Search Signals", "You do not have access to any ISNs - please contact your administrator")
			return
		}

		// Cookie exists = user has ISN access, proceed to handler
		next.ServeHTTP(w, r)
	})
}
