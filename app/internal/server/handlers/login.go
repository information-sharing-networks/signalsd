package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/server/responses"
	"github.com/rs/zerolog"
)

type LoginHandler struct {
	queries     *database.Queries
	authService *auth.AuthService
	environment string
}

func NewLoginHandler(queries *database.Queries, authService *auth.AuthService, environment string) *LoginHandler {
	return &LoginHandler{
		queries:     queries,
		authService: authService,
		environment: environment,
	}
}

type LoginRequest struct {
	CreateUserRequest
}

// LoginHandler godoc
//
//	@Summary		Login
//	@Description	The response body includes an access token. A refresh token is included in a http-only cookie named refresh_token
//	@Description	The access_token is valid for 30 mins.
//	@Description
//	@Description	Use the refresh_token with the /auth/refresh endpoint to renew the access_token.
//	@Description
//	@Description	The refresh_token lasts 30 days unless it is revoked earlier.
//	@Description	To renew the refresh_token, log in again.
//
//	@Tags			auth
//
//	@Param			request	body	handlers.LoginRequest	true	"email and password"
//	@Example		value { "access_token": "abc...", "token_type": "Bearer", "expires_in": 1800, "role": "member", "isn_perms": { "isn-slug-1": "write", "isn-slug-2": "read" } }
//
//	@Success		200	{object}	auth.AccessTokenResponse
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Router			/auth/login [post]
func (l *LoginHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	logger := zerolog.Ctx(r.Context())

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	exists, err := l.queries.ExistsUserWithEmail(r.Context(), req.Email)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if !exists {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceNotFound, "no user found with this email address")
		return
	}

	user, err := l.queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	err = l.authService.CheckPasswordHash(user.HashedPassword, req.Password)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "Incorrect email or password")
		return
	}

	// new access token
	ctx := auth.ContextWithAccountID(r.Context(), user.AccountID)

	accessTokenResponse, err := l.authService.BuildAccessTokenResponse(ctx)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, fmt.Sprintf("error creating access token: %v", err))
		return
	}

	// new refresh token
	refreshToken, err := l.authService.RotateRefreshToken(ctx)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, fmt.Sprintf("error creating refresh token: %v", err))
		return
	}

	// include the new refresh token in a http-only cookie
	newCookie := l.authService.NewRefreshTokenCookie(l.environment, refreshToken)

	http.SetCookie(w, newCookie)

	logger.Info().Msgf("user %s logged in", user.AccountID)
	responses.RespondWithJSON(w, http.StatusOK, accessTokenResponse)
}
