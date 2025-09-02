package ui

import (
	"encoding/base64"
	"encoding/json"
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

// getAccountInfoFromCookie reads and decodes the account info from the cookie
func (s *Server) getAccountInfoFromCookie(r *http.Request) *AccountInfo {
	accountInfoCookie, err := r.Cookie(accountInfoCookieName)
	if err != nil {
		return nil
	}

	// Decode base64
	decodedAccountInfo, err := base64.StdEncoding.DecodeString(accountInfoCookie.Value)
	if err != nil {
		s.logger.Error("Failed to decode account info cookie", slog.String("error", err.Error()))
		return nil
	}

	// Unmarshal JSON
	var accountInfo AccountInfo
	if err := json.Unmarshal(decodedAccountInfo, &accountInfo); err != nil {
		s.logger.Error("Failed to unmarshal account info", slog.String("error", err.Error()))
		return nil
	}

	return &accountInfo
}

// RequireAdminAccess is middleware that checks if user has admin/owner role
func (s *Server) RequireAdminAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accountInfo := s.getAccountInfoFromCookie(r)
		if accountInfo == nil {
			s.logger.Error("Account info not found in RequireAdminAccess middleware")
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

// getIsnPermsFromCookie reads and decodes the ISN permissions from the cookie
func (s *Server) getIsnPermsFromCookie(r *http.Request) map[string]IsnPerms {
	permsCookie, err := r.Cookie(isnPermsCookieName)
	if err != nil {
		return make(map[string]IsnPerms) // Return empty map if no cookie
	}

	// Decode base64
	decodedPerms, err := base64.StdEncoding.DecodeString(permsCookie.Value)
	if err != nil {
		s.logger.Error("Failed to decode ISN permissions cookie", slog.String("error", err.Error()))
		return make(map[string]IsnPerms)
	}

	// Unmarshal JSON
	var perms map[string]IsnPerms
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		s.logger.Error("Failed to unmarshal ISN permissions", slog.String("error", err.Error()))
		return make(map[string]IsnPerms)
	}

	return perms
}
