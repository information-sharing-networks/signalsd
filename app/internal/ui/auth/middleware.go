package auth

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
)

// RequireAuth is middleware that checks authentication and attempts token refresh if needed.
//
// This middleware ensures that:
// 1. All requests have valid authentication (redirects to login if not)
// 2. Expired tokens are automatically refreshed when possible
// 3. Fresh cookies are set after token refresh for subsequent requests
//
// Handlers can read auth data directly from cookies using helper methods.
func (a *AuthService) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())
		tokenStatus := a.CheckTokenStatus(r)

		switch tokenStatus {
		case TokenValid:
			// Log successful authentication check
			reqLogger.Debug("Authentication check successful",
				slog.String("component", "ui.RequireAuth"),
			)
			// Authentication is valid, proceed to handler
			next.ServeHTTP(w, r)
			return
		case TokenMissing, TokenInvalid:
			reqLogger.Debug("Authentication failed - redirecting to login",
				slog.String("component", "ui.RequireAuth"),
				slog.String("status", tokenStatus.String()),
			)
			redirectToLogin(w, r)
			return
		case TokenExpired:
			// attempt refresh
			refreshTokenCookie, err := r.Cookie(signalsd.RefreshTokenCookieName)
			if err != nil {
				reqLogger.Error("Failed to get refresh token cookie",
					slog.String("component", "ui.RequireAuth"),
					slog.String("error", err.Error()),
				)
				redirectToLogin(w, r)
				return
			}

			// attempt a token refresh
			loginResp, newRefreshTokenCookie, err := a.RefreshToken(refreshTokenCookie)
			if err != nil {
				reqLogger.Error("Token refresh failed",
					slog.String("component", "ui.RequireAuth"),
					slog.String("error", err.Error()),
				)
				redirectToLogin(w, r)
				return
			}

			if err := a.SetAuthCookies(w, loginResp, newRefreshTokenCookie, a.environment); err != nil {
				reqLogger.Error("Failed to set authentication cookies after refresh",
					slog.String("component", "ui.RequireAuth"),
					slog.String("error", err.Error()),
				)
				redirectToLogin(w, r)
				return
			}

			reqLogger.Debug("Token refresh successful",
				slog.String("component", "ui.RequireAuth"),
			)

			// Continue to handler with fresh cookies set
			next.ServeHTTP(w, r)
		}
	})
}

// RequireAdminAccess is middleware that checks if user has admin/owner role
func (a *AuthService) RequireAdminAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())

		accountInfo, err := a.GetAccountInfoFromCookie(r)
		if err != nil {
			reqLogger.Error("Could not get accountInfo from Cookie",
				slog.String("component", "ui.RequireAdminAccess"),
				slog.String("error", err.Error()),
			)
			redirectToAccessDenied(w, r)
			return
		}

		if accountInfo.Role != "owner" && accountInfo.Role != "admin" {
			reqLogger.Debug("User attempted to access admin area without permission",
				slog.String("component", "ui.RequireAdminAccess"),
				slog.String("account_id", accountInfo.AccountID),
				slog.String("role", accountInfo.Role),
			)
			redirectToAccessDenied(w, r)
			return
		}

		// Log successful admin access check
		reqLogger.Debug("Admin access check successful",
			slog.String("component", "ui.RequireAdminAccess"),
		)

		next.ServeHTTP(w, r)
	})
}

// RequireIsnAccess is middleware that checks if user has access to any ISNs
func (a *AuthService) RequireIsnAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())

		_, err := r.Cookie(config.IsnPermsCookieName)
		if err != nil {
			// No ISN permissions cookie = no ISN access
			reqLogger.Debug("User attempted to access ISN features without ISN permissions",
				slog.String("component", "ui.RequireIsnAccess"),
			)
			redirectToAccessDenied(w, r)
			return
		}

		// Log successful ISN access check
		reqLogger.Debug("ISN access check successful",
			slog.String("component", "ui.RequireIsnAccess"),
		)

		// Cookie exists = user has ISN access, proceed to handler
		next.ServeHTTP(w, r)
	})
}

// Helper method for redirecting to login
func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

// redirectToAccessDenied redirects to access denied page for both HTMX and direct requests
func redirectToAccessDenied(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/access-denied")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/access-denied", http.StatusSeeOther)
	}
}
