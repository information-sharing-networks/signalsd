package ui

import (
	"net/http"

	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// RequireAuth is middleware that checks authentication and attempts token refresh if needed
func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := s.authService.CheckTokenStatus(r)

		switch status {
		case TokenValid:
			next.ServeHTTP(w, r)
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

			s.logger.Info().Msg("Access token refreshed successfully")
			next.ServeHTTP(w, r) // Continue with refreshed token
		}
	})
}
