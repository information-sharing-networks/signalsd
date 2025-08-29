package ui

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// RequireAuth is middleware that checks authentication and attempts token refresh if needed.
//
// IMPORTANT: We read user data from cookies and put it in request context because:
// 1. Cookies persist across requests (session-scoped)
// 2. Context only exists during a single request (request-scoped)
// 3. When tokens are refreshed mid-request, cookies get updated but context doesn't
// 4. This ensures handlers always get the most current data after refresh
//
// All handlers should read from context, never directly from cookies.
func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := s.authService.CheckTokenStatus(r)

		switch status {
		case TokenValid:
			// We read from cookies and put in context because when tokens are refreshed
			// mid-request, cookies get updated but the current request context doesn't.
			// This ensures handlers always get the most current data.
			ctx := r.Context()

			// Add access token to context
			accessTokenCookie, err := r.Cookie(accessTokenCookieName)
			if err == nil {
				ctx = ContextWithAccessToken(ctx, accessTokenCookie.Value)
			}

			// Add account info to context if available
			if accountInfo := s.getAccountInfoFromCookie(r); accountInfo != nil {
				ctx = ContextWithAccountInfo(ctx, *accountInfo)
			}

			// Add ISN permissions to context
			isnPerms := s.getIsnPermsFromCookie(r)
			ctx = ContextWithIsnPerms(ctx, isnPerms)

			next.ServeHTTP(w, r.WithContext(ctx))
			return
		case TokenMissing, TokenInvalid:
			s.redirectToLogin(w, r)
			return
		case TokenExpired: // attempt refresh
			refreshTokenCookie, err := r.Cookie(signalsd.RefreshTokenCookieName)
			if err != nil {
				s.logger.Err(err).Msg("Failed to get refresh token cookie")
				s.redirectToLogin(w, r)
				return
			}

			// Need to get the access token cookie for refresh
			accessTokenCookie, err := r.Cookie(accessTokenCookieName)
			if err != nil {
				s.logger.Err(err).Msg("Failed to get access token cookie")
				s.redirectToLogin(w, r)
				return
			}

			// Attempt token refresh
			loginResp, newRefreshTokenCookie, err := s.authService.RefreshToken(accessTokenCookie, refreshTokenCookie)
			if err != nil {
				s.logger.Error().Err(err).Msg("Token refresh failed")
				s.redirectToLogin(w, r)
				return
			}

			// Set all authentication cookies using shared method (includes updated permissions)
			if err := s.authService.SetAuthCookies(w, loginResp, newRefreshTokenCookie, s.config.Environment); err != nil {
				s.logger.Error().Err(err).Msg("Failed to set authentication cookies after refresh")
				s.redirectToLogin(w, r)
				return
			}

			// After token refresh, we need to read the updated data from cookies and put in context
			// because the refresh process updates cookies with new permissions/account info.
			ctx := ContextWithAccessToken(r.Context(), loginResp.AccessToken)

			// Add account info to context (will be available from the refreshed cookie)
			if accountInfo := s.getAccountInfoFromCookie(r); accountInfo != nil {
				ctx = ContextWithAccountInfo(ctx, *accountInfo)
			}

			// Add ISN permissions to context (refreshed permissions from new cookie)
			isnPerms := s.getIsnPermsFromCookie(r)
			ctx = ContextWithIsnPerms(ctx, isnPerms)

			next.ServeHTTP(w, r.WithContext(ctx)) // Continue with refreshed token in context
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
		s.logger.Error().Err(err).Msg("Failed to decode account info cookie")
		return nil
	}

	// Unmarshal JSON
	var accountInfo AccountInfo
	if err := json.Unmarshal(decodedAccountInfo, &accountInfo); err != nil {
		s.logger.Error().Err(err).Msg("Failed to unmarshal account info")
		return nil
	}

	return &accountInfo
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
		s.logger.Error().Err(err).Msg("Failed to decode ISN permissions cookie")
		return make(map[string]IsnPerms)
	}

	// Unmarshal JSON
	var perms map[string]IsnPerms
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		s.logger.Error().Err(err).Msg("Failed to unmarshal ISN permissions")
		return make(map[string]IsnPerms)
	}

	return perms
}
