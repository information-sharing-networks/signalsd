package ui

import (
	"encoding/base64"
	"encoding/json"
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
				s.logger.Err(err).Msg("Failed to get refresh token cookie")
				s.redirectToLogin(w, r)
				return
			}

			loginResp, newRefreshTokenCookie, err := s.authService.RefreshToken(refreshTokenCookie)
			if err != nil {
				s.logger.Error().Err(err).Msg("Token refresh failed")
				s.redirectToLogin(w, r)
				return
			}

			if err := s.authService.SetAuthCookies(w, loginResp, newRefreshTokenCookie, s.config.Environment); err != nil {
				s.logger.Error().Err(err).Msg("Failed to set authentication cookies after refresh")
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
