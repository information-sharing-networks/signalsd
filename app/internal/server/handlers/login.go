package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
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
//	@Description	The response body includes an access token which can be used to access the protected enpoints, assuming the account has the appropriate permissions.
//	@Description	The access_token is valid for 30 minutes.
//	@Description
//	@Description	As part of the login response, the server sets a http-only cookie on the client that will allow it to refresh the token (use the /oauth/token endpoint with a grant_type=refresh_token param)
//	@Description	The refresh_token lasts 30 days unless it is revoked earlier.
//	@Description	- To renew the refresh_token, log in again.
//	@Description	- To revoke the refresh_token, call the /oauth/revoke endpoint.
//	@Description
//	@Description	The account's role and permissions are encoded as part of the jwt access token and this information is also provided in the response body.
//
//	@Tags			auth
//
//	@Param			request	body		handlers.LoginRequest	true	"email and password"
//
//	@Success		200		{object}	auth.AccessTokenResponse
//	@Example		value { "access_token": "abc...", "token_type": "Bearer", "expires_in": 1800, "role": "member", "isn_perms": { "isn-slug-1": "write", "isn-slug-2": "read" } }
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Router			/api/auth/login [post]
func (l *LoginHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	exists, err := l.queries.ExistsUserWithEmail(r.Context(), req.Email)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("email", req.Email),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}
	if !exists {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceNotFound, "no user found with this email address")
		return
	}

	user, err := l.queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("email", req.Email),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	err = l.authService.CheckPasswordHash(user.HashedPassword, req.Password)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "Incorrect email or password")
		return
	}

	// check if the user account is active
	account, err := l.queries.GetAccountByID(r.Context(), user.AccountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("account_id", user.AccountID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if !account.IsActive {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("user_account_id", user.AccountID.String()),
		)

		responses.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthenticationFailure, "account is disabled")
		return
	}

	// new access token
	ctx := auth.ContextWithAccountID(r.Context(), user.AccountID)

	accessTokenResponse, err := l.authService.CreateAccessToken(ctx)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("account_id", user.AccountID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, "error creating access token")
		return
	}

	// new refresh token
	refreshToken, err := l.authService.RotateRefreshToken(ctx)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("account_id", user.AccountID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeTokenInvalid, "error creating refresh token")
		return
	}

	// include the new refresh token in a http-only cookie
	newCookie := l.authService.NewRefreshTokenCookie(l.environment, refreshToken)

	http.SetCookie(w, newCookie)

	logger.ContextWithLogAttrs(r.Context(),
		slog.String("user_account_id", user.AccountID.String()),
	)

	responses.RespondWithJSON(w, http.StatusOK, accessTokenResponse)
}
