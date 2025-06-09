package handlers

import (
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ServiceAccountHandler struct {
	queries     *database.Queries
	authService *auth.AuthService
	pool        *pgxpool.Pool
}

func NewServiceAccountHandler(queries *database.Queries, authService *auth.AuthService, pool *pgxpool.Pool) *ServiceAccountHandler {
	return &ServiceAccountHandler{
		queries:     queries,
		authService: authService,
		pool:        pool,
	}
}

// RegisterServiceAccountHandler godocs
//
//	@Summary		Register a new service account
//	@Description	Access tokens expire after 30 mins and subsequent requests using the token will fail with HTTP status 401 and an error_code of "access_token_expired"
//	@Tags			auth
//
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerServiceAccount
//
//	@Router			/auth/register/service-accounts [post]
func (a *ServiceAccountHandler) RegisterServiceAccountHandler(w http.ResponseWriter, r *http.Request) {

	// transaction
	// create account
	// create service account
	// create one time secret + retrieval url

	responses.RespondWithStatusCodeOnly(w, http.StatusNotImplemented)
}

// RevokeServiceAccountHandler godoc
//
//	@Summary		Revoke client secret
//	@Description	Revoke a client secret to prevent it being used to create new access ServiceAccounts.
//	@Description
//	@Tags		auth
//
//	@Success	204
//	@Failure	400	{object}	responses.ErrorResponse
//	@Failure	404	{object}	responses.ErrorResponse
//	@Failure	500	{object}	responses.ErrorResponse
//
//	@Security	BearerServiceAccount
//
//	@Router		/auth/service-accounts/revoke [post]
func (a *ServiceAccountHandler) RevokeServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	responses.RespondWithStatusCodeOnly(w, http.StatusNotImplemented)
}

// RetrieveOneTimeClientSecret godoc
//
//	@Summary		Revoke client secret
//	@Description	Revoke a client secret to prevent it being used to create new access ServiceAccounts.
//	@Description
//	@Tags		auth
//
//	@Router		/auth/service-accounts/revoke [post]
func (a *ServiceAccountHandler) RetrieveOneTimeClientSecret(w http.ResponseWriter, r *http.Request) {

	// retieve by id / account id
	// check not expired
	// delete entry from one time table
	// revoke any previously issued credentials for the service account
	// record hashed client secret secret the expiry time on token for 1 year.

	responses.RespondWithStatusCodeOnly(w, http.StatusNotImplemented)
}
