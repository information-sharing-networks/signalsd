package auth

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
// 4. Authentication data is extracted from cookies and added to request context
//
// Handlers can read auth data from context using helper methods.
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

			// Extract authentication data from cookies and add to context
			ctx, err := a.addAuthDataToContext(r.Context(), r)
			if err != nil {
				reqLogger.Error("Failed to extract authentication data from cookies",
					slog.String("component", "ui.RequireAuth"),
					slog.String("error", err.Error()),
				)
				redirectToLogin(w, r)
				return
			}

			// Authentication is valid, proceed to handler with context
			next.ServeHTTP(w, r.WithContext(ctx))
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

			// Extract authentication data from fresh cookies and add to context
			ctx, err := a.addAuthDataToContext(r.Context(), r)
			if err != nil {
				reqLogger.Error("Failed to extract authentication data from refreshed cookies",
					slog.String("component", "ui.RequireAuth"),
					slog.String("error", err.Error()),
				)
				redirectToLogin(w, r)
				return
			}

			// Continue to handler with fresh cookies set and context
			next.ServeHTTP(w, r.WithContext(ctx))
		}
	})
}

// RequireAdminOrOwnerRole is middleware that checks if user has admin/owner role
func (a *AuthService) RequireAdminOrOwnerRole(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())

		accountInfo, ok := ContextAccountInfo(r.Context())
		if !ok {
			reqLogger.Error("Could not get accountInfo from context",
				slog.String("component", "ui.RequireAdminAccess"),
			)
			redirectToAccessDeniedPage(w, r)
			return
		}

		if accountInfo.Role != "owner" && accountInfo.Role != "admin" {
			reqLogger.Debug("Access denied - account attempted to access admin feature",
				slog.String("component", "ui.RequireAdminAccess"),
				slog.String("account_id", accountInfo.AccountID),
				slog.String("role", accountInfo.Role),
			)
			redirectToAccessDeniedPage(w, r)
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

		isnPerms, ok := ContextIsnPerms(r.Context())
		if !ok {
			reqLogger.Debug("access denied - user does not have access to any ISNs",
				slog.String("component", "ui.RequireIsnAccess"),
			)
			redirectToNeedIsnAdminPage(w, r)
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
		redirectToNeedIsnAdminPage(w, r)

	})
}

// RequireIsnAccess is middleware that checks if user has access to one or more ISNs.
// Use this middlware to prevent accounts from accessing pages that are only relevant to ISN members
func (a *AuthService) RequireIsnAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())

		// Check if ISN permissions exist in context
		// ISN permissions are only added to context if the account has access to one or more ISNs
		_, ok := ContextIsnPerms(r.Context())
		if !ok {
			reqLogger.Debug("access denied - user does not have access to any ISNs",
				slog.String("component", "ui.RequireIsnAccess"),
			)
			redirectToNeedIsnAccessPage(w, r)
			return
		}

		reqLogger.Debug("ISN access check successful",
			slog.String("component", "ui.RequireIsnAccess"),
		)

		// ISN permissions exist in context = user has access to one or more ISNs, proceed to handler
		next.ServeHTTP(w, r)
	})
}

// redirectToLogin is a helper for redirecting to login from a middleware function
// works for both HTMX and direct requests
func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

// redirectToAccessDeniedPage redirects to access denied page
func redirectToAccessDeniedPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/access-denied")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/access-denied", http.StatusSeeOther)
	}
}

// redirectToNeedIsnAdminPage redirects to access denied page for both HTMX and direct requests
func redirectToNeedIsnAdminPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/need-isn-admin")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/need-isn-admin", http.StatusSeeOther)
	}
}

// redirectToNeedIsnAccessPage redirects to page explaining the user needs to be granted access to one or more isns
func redirectToNeedIsnAccessPage(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/need-isn-access")
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, "/need-isn-access", http.StatusSeeOther)
	}
}
