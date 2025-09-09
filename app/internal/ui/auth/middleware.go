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
			accessTokenDetails, newRefreshTokenCookie, err := a.RefreshToken(refreshTokenCookie)
			if err != nil {
				reqLogger.Error("Token refresh failed",
					slog.String("component", "ui.RequireAuth"),
					slog.String("error", err.Error()),
				)
				redirectToLogin(w, r)
				return
			}

			if err := a.SetAuthCookies(w, accessTokenDetails, newRefreshTokenCookie, a.environment); err != nil {
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

// RequireAdminOrOwnerRole is middleware that checks if user has admin/owner role
func (a *AuthService) RequireAdminOrOwnerRole(next http.Handler) http.Handler {
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
			reqLogger.Debug("Access denied - account attempted to access admin feature",
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

// RequireIsnAdmin is middleware that checks if user is the admin for one or more ISNs.
// redirects to the 'access denied' page if not.
func (a *AuthService) RequireIsnAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		reqLogger := logger.ContextRequestLogger(r.Context())

		isnPerms, err := a.GetIsnPermsFromCookie(r)
		if err != nil {
			reqLogger.Debug("access denied - user does not have access to any ISNs",
				slog.String("component", "ui.RequireIsnAccess"),
			)
			redirectToNeedIsnAdmin(w, r)
			return
		}
		for perm := range isnPerms {
			if isnPerms[perm].IsnAdmin {
				reqLogger.Debug("ISN admin check successful",
					slog.String("component", "ui.RequireIsnAdmin"),
				)
				next.ServeHTTP(w, r)
				return
			}
		}

		reqLogger.Debug("access denied - user does not have admin role for any ISNs",
			slog.String("component", "ui.RequireIsnAdmin"),
		)
		redirectToNeedIsnAdmin(w, r)

	})
}

// RequireIsnAccess is middleware that checks if user has access to one or more ISNs.
// Use this middlware to prevent accounts from accessing pages that are only relevant to ISN members
func (a *AuthService) RequireIsnAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())

		// the UI middleware extracts the ISN permissions from the access token claims and stores them in a cookie.
		// The middleware only sets this cookie if the account has access to one or more ISNs
		_, err := r.Cookie(config.IsnPermsCookieName)
		if err != nil {
			reqLogger.Debug("access denied - user does not have access to any ISNs",
				slog.String("component", "ui.RequireIsnAccess"),
			)
			redirectToAccessDenied(w, r)
			return
		}

		reqLogger.Debug("ISN access check successful",
			slog.String("component", "ui.RequireIsnAccess"),
		)

		// Cookie exists = user has access to one or more ISNs, proceed to handler
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

// redirectToNeedIsnAdmin redirects to access denied page for both HTMX and direct requests
func redirectToNeedIsnAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/need-isn-admin")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/need-isn-admin", http.StatusSeeOther)
	}
}
