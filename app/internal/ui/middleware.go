package ui

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
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
		reqLogger := logger.ContextMiddlewareLogger(r.Context())
		tokenStatus := s.authService.CheckTokenStatus(r)

		switch tokenStatus {
		case TokenValid:
			// Log successful authentication check
			reqLogger.Debug("Authentication check successful",
				slog.String("component", "RequireAuth"),
			)
			// Authentication is valid, proceed to handler
			next.ServeHTTP(w, r)
			return
		case TokenMissing, TokenInvalid:
			reqLogger.Debug("Authentication failed - redirecting to login",
				slog.String("component", "RequireAuth"),
				slog.String("status", tokenStatus.String()),
			)
			s.redirectToLogin(w, r)
			return
		case TokenExpired:
			// attempt refresh
			refreshTokenCookie, err := r.Cookie(signalsd.RefreshTokenCookieName)
			if err != nil {
				reqLogger.Error("Failed to get refresh token cookie",
					slog.String("component", "RequireAuth"),
					slog.String("error", err.Error()),
				)
				s.redirectToLogin(w, r)
				return
			}

			loginResp, newRefreshTokenCookie, err := s.authService.RefreshToken(refreshTokenCookie)
			if err != nil {
				reqLogger.Error("Token refresh failed",
					slog.String("component", "RequireAuth"),
					slog.String("error", err.Error()),
				)
				s.redirectToLogin(w, r)
				return
			}

			if err := s.authService.SetAuthCookies(w, loginResp, newRefreshTokenCookie, s.config.Environment); err != nil {
				reqLogger.Error("Failed to set authentication cookies after refresh",
					slog.String("component", "RequireAuth"),
					slog.String("error", err.Error()),
				)
				s.redirectToLogin(w, r)
				return
			}

			reqLogger.Debug("Token refresh successful",
				slog.String("component", "RequireAuth"),
			)

			// Continue to handler with fresh cookies set
			next.ServeHTTP(w, r)
		}
	})
}

// RequireAdminAccess is middleware that checks if user has admin/owner role
func (s *Server) RequireAdminAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextMiddlewareLogger(r.Context())

		accountInfo, err := s.getAccountInfoFromCookie(r)
		if err != nil {
			reqLogger.Error("Could not get accountInfo from Cookie",
				slog.String("component", "RequireAdminAccess"),
				slog.String("error", err.Error()),
			)
			s.handleAccessDenied(w, r, "Admin Dashboard", "Internal error - account info not found, please login again")
			return
		}

		if accountInfo.Role != "owner" && accountInfo.Role != "admin" {
			reqLogger.Debug("User attempted to access admin area without permission",
				slog.String("component", "RequireAdminAccess"),
				slog.String("account_id", accountInfo.AccountID),
				slog.String("role", accountInfo.Role),
			)
			s.handleAccessDenied(w, r, "Admin Dashboard", "You do not have permission to access the admin dashboard")
			return
		}

		// Log successful admin access check
		reqLogger.Debug("Admin access check successful",
			slog.String("component", "RequireAdminAccess"),
			slog.String("account_id", accountInfo.AccountID),
			slog.String("role", accountInfo.Role),
		)

		next.ServeHTTP(w, r)
	})
}

// RequireIsnAccess is middleware that checks if user has access to any ISNs
func (s *Server) RequireIsnAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextMiddlewareLogger(r.Context())

		_, err := r.Cookie(isnPermsCookieName)
		if err != nil {
			// No ISN permissions cookie = no ISN access
			reqLogger.Debug("User attempted to access ISN features without ISN permissions",
				slog.String("component", "RequireIsnAccess"),
			)
			s.handleAccessDenied(w, r, "Search Signals", "You do not have access to any ISNs - please contact your administrator")
			return
		}

		// Log successful ISN access check
		reqLogger.Debug("ISN access check successful",
			slog.String("component", "RequireIsnAccess"),
		)

		// Cookie exists = user has ISN access, proceed to handler
		next.ServeHTTP(w, r)
	})
}
