package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	"github.com/jackc/pgx/v5"
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

// Login godoc
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
//	@Failure		400		{object}	responses.ErrorResponse	"malformed_body"
//	@Failure		401		{object}	responses.ErrorResponse	"authentication_error"
//	@Failure		500		{object}	responses.ErrorResponse	"database_error | token_creation_failed"
//
//	@Router			/api/auth/login [post]
func (l *LoginHandler) Login(w http.ResponseWriter, r *http.Request) error {
	var req LoginRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return apperrors.MalformedBody("invalid JSON body", err)
	}

	// check if the email is registered
	user, err := l.queries.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.AuthenticationFailure("incorrect email or password", nil)
		}
		return apperrors.DatabaseError("database error", err)
	}

	if err := l.authService.CheckPasswordHash(user.HashedPassword, req.Password); err != nil {
		return apperrors.AuthenticationFailure("incorrect email or password", nil)
	}

	// check if the user account is active
	account, err := l.queries.GetAccountByID(r.Context(), user.AccountID)
	if err != nil {
		return apperrors.DatabaseError("database error", err)
	}

	// add the account_id to the request log context
	logger.ContextWithLogAttrs(r.Context(),
		slog.String("account_id", user.AccountID.String()),
	)

	if !account.IsActive {
		return apperrors.AuthenticationFailure("account is disabled", nil)
	}

	// new access token
	ctx := auth.ContextWithAccountID(r.Context(), user.AccountID)

	accessTokenResponse, err := l.authService.CreateAccessToken(ctx)
	if err != nil {
		return apperrors.TokenCreationFailure("error creating access token", err)
	}

	// new refresh token
	refreshToken, err := l.authService.RotateRefreshToken(ctx)
	if err != nil {
		return apperrors.TokenCreationFailure("error creating refresh token", err)
	}

	// include the new refresh token in a http-only cookie
	newCookie := l.authService.NewRefreshTokenCookie(refreshToken)

	http.SetCookie(w, newCookie)

	return responses.JSON(w, http.StatusOK, accessTokenResponse)
}
