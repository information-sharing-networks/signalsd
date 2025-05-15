package auth

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nickabs/signals/internal/apperrors"
	"github.com/nickabs/signals/internal/context"
	"github.com/nickabs/signals/internal/helpers"
)

func (a AuthService) ValidateAccessToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		bearerToken, err := a.BearerTokenFromHeader(r.Header)
		if err != nil {
			helpers.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		claims := jwt.RegisteredClaims{}

		_, err = jwt.ParseWithClaims(bearerToken, &claims, func(token *jwt.Token) (interface{}, error) {
			return []byte(a.cfg.SecretKey), nil
		})
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				helpers.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAccessTokenExpired, "access token expired, please use the refresh api to renew it")
				return
			}
			helpers.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}

		rawID := claims.Subject
		userID, err := uuid.Parse(rawID)
		if err != nil {
			helpers.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, fmt.Sprintf("unauthorized: %v", err))
			return
		}
		ctx := context.WithUserID(r.Context(), userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
