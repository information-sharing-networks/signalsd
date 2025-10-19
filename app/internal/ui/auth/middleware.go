package auth

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
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
			refreshTokenCookie, err := r.Cookie(config.RefreshTokenCookieName)
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

// RequireRole checks if the user has one of the specified roles
func (a *AuthService) RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqLogger := logger.ContextRequestLogger(r.Context())

			accessTokenDetails, ok := ContextAccessTokenDetails(r.Context())
			if !ok {
				reqLogger.Error("Could not get accessTokenDetails from context", slog.String("component", "ui.RequireRole"))
				redirectToAccessDeniedPage(w, r, "unexpected error - please log in again")
				return
			}

			for _, role := range allowedRoles {
				if accessTokenDetails.Role == role {
					reqLogger.Debug("Role authorization successful",
						slog.String("component", "ui.RequireRole"),
						slog.String("account_id", accessTokenDetails.AccountID),
						slog.Any("allowed_roles", allowedRoles),
						slog.String("role", role),
					)

					next.ServeHTTP(w, r)
					return
				}
			}

			// Access denied - log and redirect
			reqLogger.Debug("Access denied - account does not have required role",
				slog.String("component", "ui.RequireRole"),
				slog.String("account_id", accessTokenDetails.AccountID),
				slog.String("user_role", accessTokenDetails.Role),
				slog.Any("allowed_roles", allowedRoles),
			)
			msg := ""
			if len(allowedRoles) == 1 {
				msg = "Your account needs the " + allowedRoles[0] + " role to access this feature."
			} else {
				msg = "You must be one of the following roles to access this feature: " + strings.Join(allowedRoles, ", ")
			}
			redirectToAccessDeniedPage(w, r, msg)
		})
	}
}

// RequireIsnAdmin is middleware that checks if user is the admin for one or more ISNs.
// redirects to the 'access denied' page if not.
func (a *AuthService) RequireIsnAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		reqLogger := logger.ContextRequestLogger(r.Context())

		accessTokenDetails, ok := ContextAccessTokenDetails(r.Context())
		if !ok {
			reqLogger.Error("Could not get accessTokenDetails from context", slog.String("component", "ui.RequireIsnAdmin"))
			redirectToAccessDeniedPage(w, r, "unexpected error - please log in again")
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
			redirectToAccessDeniedPage(w, r, "unexpected error - please log in again")
			return
		}

		if len(accessTokenDetails.IsnPerms) == 0 {
			reqLogger.Debug("access denied - user does not have access to any ISNs", slog.String("component", "ui.RequireIsnAccess"))
			redirectToAccessDeniedPage(w, r, "You need to be added to one or more ISNs before accessing this page")
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

// redirectToAccessDeniedPage redirects to access denied page with a message
func redirectToAccessDeniedPage(w http.ResponseWriter, r *http.Request, msg string) {

	accessDeniedURL := "/access-denied?msg=" + url.QueryEscape(msg)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", accessDeniedURL)
		w.WriteHeader(http.StatusOK)
	} else {
		http.Redirect(w, r, accessDeniedURL, http.StatusSeeOther)
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
