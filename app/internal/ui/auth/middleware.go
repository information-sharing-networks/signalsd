package auth

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// RequireAuth is middleware that checks authentication and attempts token refresh if needed.
//
// This middleware:
// Checks for an access token details cookie that was set at login:
//   - If found and not expired, adds the access token details to the context and allows the request to proceed
//   - if not found, or the token is expired, it will attempt to refresh the token.
//   - If the refresh is successful, sets the new access token details/refresh cookie in the browser and adds the access token details to the context and the request is allowed to proceed (otherwise redirects to login)
func (a *AuthService) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())

		ctx := r.Context()
		var tokenStatus AccessTokenStatus

		// Extract access token details from cookie
		accessTokenDetailsCookie, err := r.Cookie(config.AccessTokenDetailsCookieName)
		if err != nil {
			tokenStatus = TokenMissing

		} else {

			// Decode the access token details cookie
			decodedAccessTokenDetails, err := base64.StdEncoding.DecodeString(accessTokenDetailsCookie.Value)
			if err != nil {
				reqLogger.Error("could not decode access token detail cookie ", slog.String("error", err.Error()), slog.String("component", "ui.RequireAuth"))
				redirectToLogin(w, r)
			}

			accessTokenDetails := &types.AccessTokenDetails{}
			if err := json.Unmarshal(decodedAccessTokenDetails, accessTokenDetails); err != nil {
				reqLogger.Error("could not unmarshal access token detail", slog.String("error", err.Error()), slog.String("component", "ui.RequireAuth"))
				redirectToLogin(w, r)
			}

			// check the access token status
			tokenStatus = a.CheckAccessTokenStatus(accessTokenDetails)

			// create context with access token details
			ctx = ContextWithAccessTokenDetails(ctx, accessTokenDetails)
		}

		switch tokenStatus {
		case TokenValid:
			reqLogger.Debug("Authentication check successful",
				slog.String("component", "ui.RequireAuth"),
			)

			next.ServeHTTP(w, r.WithContext(ctx))
			return

		case TokenInvalid:
			reqLogger.Error("Authentication failed - invalid access token received - redirecting to login",
				slog.String("component", "ui.RequireAuth"),
				slog.String("status", tokenStatus.String()),
			)
			redirectToLogin(w, r)
			return

		case TokenMissing, TokenExpired:
			// check for a refresh token cookie
			refreshTokenCookie, err := r.Cookie(signalsd.RefreshTokenCookieName)
			if err != nil {
				reqLogger.Debug("Failed to get refresh token cookie - redirecting to login",
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

			if err := a.SetAuthCookies(w, accessTokenDetails, newRefreshTokenCookie); err != nil {
				reqLogger.Error("Failed to set authentication cookies after refresh",
					slog.String("component", "ui.RequireAuth"),
					slog.String("error", err.Error()),
				)
				redirectToLogin(w, r)
				return
			}

			// add refreshed access token details to context
			ctx := ContextWithAccessTokenDetails(r.Context(), accessTokenDetails)

			reqLogger.Info("Token refresh successful",
				slog.String("component", "ui.RequireAuth"),
				slog.String("account_id", accessTokenDetails.AccountID),
			)

			next.ServeHTTP(w, r.WithContext(ctx))
		}
	})
}

// RequireAdminOrOwnerRole is middleware that checks if user has admin/owner role
func (a *AuthService) RequireAdminOrOwnerRole(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())

		accessTokenDetails, ok := ContextAccessTokenDetails(r.Context())
		if !ok {
			reqLogger.Error("Could not get accessTokenDetails from context", slog.String("component", "ui.RequireAdminOrOwnerAccess"))
			redirectToAccessDeniedPage(w, r)
			return
		}

		if accessTokenDetails.Role != "owner" && accessTokenDetails.Role != "admin" {
			reqLogger.Debug("Access denied - account attempted to access admin feature",
				slog.String("component", "ui.RequireAdminAccess"),
				slog.String("account_id", accessTokenDetails.AccountID),
				slog.String("role", accessTokenDetails.Role),
			)
			redirectToAccessDeniedPage(w, r)
			return
		}

		// Log successful admin access check
		reqLogger.Debug("Admin access check successful", slog.String("component", "ui.RequireAdminAccess"))

		next.ServeHTTP(w, r)
	})
}

// RequireIsnAdmin is middleware that checks if user is the admin for one or more ISNs.
// redirects to the 'access denied' page if not.
func (a *AuthService) RequireIsnAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		reqLogger := logger.ContextRequestLogger(r.Context())

		accessTokenDetails, ok := ContextAccessTokenDetails(r.Context())
		if !ok {
			reqLogger.Error("Could not get accessTokenDetails from context", slog.String("component", "ui.RequireIsnAdmin"))
			redirectToAccessDeniedPage(w, r)
			return
		}

		for perm := range accessTokenDetails.IsnPerms {
			if accessTokenDetails.IsnPerms[perm].IsnAdmin {
				reqLogger.Debug("ISN admin check successful", slog.String("component", "ui.RequireIsnAdmin"))
				next.ServeHTTP(w, r)
				return
			}
		}

		reqLogger.Debug("access denied - user does not have admin role for any ISNs", slog.String("component", "ui.RequireIsnAdmin"))
		redirectToNeedIsnAdminPage(w, r)

	})
}

// RequireIsnAccess is middleware that checks if user has access to one or more ISNs.
// Use this middlware to prevent accounts from accessing pages that are only relevant to ISN members
func (a *AuthService) RequireIsnAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqLogger := logger.ContextRequestLogger(r.Context())

		// Check if this account has any ISN access
		accessTokenDetails, ok := ContextAccessTokenDetails(r.Context())
		if !ok {
			reqLogger.Error("Could not get accessTokenDetails from context", slog.String("component", "ui.RequireIsnAccess"))
			redirectToAccessDeniedPage(w, r)
			return
		}

		if len(accessTokenDetails.IsnPerms) == 0 {
			reqLogger.Debug("access denied - user does not have access to any ISNs", slog.String("component", "ui.RequireIsnAccess"))
			redirectToNeedIsnAccessPage(w, r)
			return
		}

		reqLogger.Debug("ISN access check successful", slog.String("component", "ui.RequireIsnAccess"))

		// ISN permissions exist in context = user has access to one or more ISNs, proceed to handler
		next.ServeHTTP(w, r)
	})
}

func (a *AuthService) AddAccountIDToLogContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accessTokenDetails, ok := ContextAccessTokenDetails(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		logger.ContextWithLogAttrs(r.Context(),
			slog.String("account_id", accessTokenDetails.AccountID),
		)

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
