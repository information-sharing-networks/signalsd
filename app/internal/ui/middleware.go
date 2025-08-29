package ui

import (
	"net/http"

	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// RequireAuth is middleware that checks authentication and attempts token refresh if needed.
// For HTMX requests with expired tokens, it returns HX-Refresh header to trigger a page refresh
// which allows HTMX to retry the request with the new token cookies.
func (s *Server) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := s.authService.CheckTokenStatus(r)

		switch status {
		case TokenValid:
			// Add the current valid access token to context for handlers to use
			accessTokenCookie, err := r.Cookie(accessTokenCookieName)
			if err == nil {
				ctx := ContextWithAccessToken(r.Context(), accessTokenCookie.Value)
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				next.ServeHTTP(w, r)
			}
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

			// Add the new access token to the request context so handlers can use it
			//todoctx := r.Context()
			//ctx = context.WithValue(ctx, "access_token", loginResp.AccessToken)
			ctx := ContextWithAccessToken(r.Context(), loginResp.AccessToken)

			next.ServeHTTP(w, r.WithContext(ctx)) // Continue with refreshed token in context
		}
	})
}
