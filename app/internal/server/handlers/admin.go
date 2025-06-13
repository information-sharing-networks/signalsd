package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type AdminHandler struct {
	queries *database.Queries
	pool    *pgxpool.Pool
}

func NewAdminHandler(queries *database.Queries, pool *pgxpool.Pool) *AdminHandler {
	return &AdminHandler{
		queries: queries,
		pool:    pool,
	}
}

// ResetHandler godoc
//
//	@Summary		reset
//	@Description	Delete all registered users and associated data.
//	@Description	This endpoint only works on environments configured as 'dev'
//	@Tags			Site admin
//
//	@Success		200
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Router			/admin/reset [post]
func (a *AdminHandler) ResetHandler(w http.ResponseWriter, r *http.Request) {

	deletedAccountsCount, err := a.queries.DeleteAccounts(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not delete accounts: %v", err))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf("%d accounts deleted", deletedAccountsCount)))
}

// ReadinessHandler godoc
//
//	@Summary		Readiness
//	@Description	check if the signalsd service is ready
//	@Tags			Site admin
//
//	@Success		200
//	@Failure		404
//
//	@Router			/health/ready [Get]
func (a *AdminHandler) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	_, err := a.queries.IsDatabaseRunning(ctx)
	if err == nil {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(http.StatusText(http.StatusOK)))
	}
}

// Liveness godoc
//
//	@Summary		Liveness check
//	@Description	check if the signalsd service is up
//	@Tags			Site admin
//
//	@Success		200
//	@Failure		404
//
//	@Router			/admin/live [Get]
func (a *AdminHandler) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(http.StatusText(http.StatusOK)))
}

// DisableAccountHandler godoc
//
//	@Summary		Disable an account
//	@Description	Disabling an account prevents it from being used to create new access tokens.
//	@Description
//	@Description	The handler will also revoke:
//	@Description		- client secrets/one-time secrets (service accounts)
//	@Description		- refresh tokens (web users).
//	@Description
//	@Description	Note: The site owner account cannot be disabled to prevent system lockout.
//	@Description	Only owners and admins can disable accounts.
//	@Tags			Site admin
//
//	@Param			account_id	path	string	true	"Account ID to disable"
//
//	@Success		200
//	@Failure		400	{object}	responses.ErrorResponse	"Invalid account ID format"
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Cannot disable site owner account"
//	@Failure		404	{object}	responses.ErrorResponse	"Account not found"
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/admin/accounts/{account_id}/disable [post]
func (a *AdminHandler) DisableAccountHandler(w http.ResponseWriter, r *http.Request) {
	accountIDString := chi.URLParam(r, "account_id")
	logger := zerolog.Ctx(r.Context())

	accountID, err := uuid.Parse(accountIDString)
	if err != nil {
		logger.Warn().Msgf("invalid account ID format: %v", accountIDString)
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid account ID format")
		return
	}

	// Start transaction
	tx, err := a.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to begin transaction: %v", err))
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.Error().Err(err).Msg("failed to rollback transaction")
		}
	}()

	txQueries := a.queries.WithTx(tx)

	account, err := txQueries.GetAccountByID(r.Context(), accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn().Msgf("account not found: %v", accountID)
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "account not found")
			return
		}
		logger.Error().Err(err).Msgf("database error retrieving account: %v", accountID)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	// Prevent disabling the site owner account
	if account.AccountRole == "owner" {
		logger.Warn().Msgf("attempt to disable site owner account: %v", accountID)
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "cannot disable the site owner account")
		return
	}

	// Disable the account
	rowsAffected, err := txQueries.DisableAccount(r.Context(), accountID)
	if err != nil {
		logger.Error().Err(err).Msgf("database error disabling account: %v", accountID)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error disabling account: %v", err))
		return
	}

	if rowsAffected == 0 {
		logger.Warn().Msgf("account not found or already disabled: %v", accountID)
		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "account not found or already disabled")
		return
	}

	// Revoke tokens based on account type
	if account.AccountType == "service_account" {
		// Revoke all client secrets for service accounts
		_, err = txQueries.RevokeAllClientSecretsForAccount(r.Context(), accountID)
		if err != nil {
			logger.Error().Err(err).Msgf("error revoking client secrets for account: %v", accountID)
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error revoking client secrets: %v", err))
			return
		}

		// Delete any one-time client secrets
		serviceAccount, err := txQueries.GetServiceAccountByAccountID(r.Context(), accountID)
		if err != nil {
			logger.Error().Err(err).Msgf("error retrieving service account: %v", accountID)
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error retrieving service account: %v", err))
			return
		}

		_, err = txQueries.DeleteOneTimeClientSecretsByOrgAndEmail(r.Context(), database.DeleteOneTimeClientSecretsByOrgAndEmailParams{
			ClientOrganization: serviceAccount.ClientOrganization,
			ClientContactEmail: serviceAccount.ClientContactEmail,
		})
		if err != nil {
			logger.Error().Err(err).Msgf("error deleting one-time client secrets for account: %v", accountID)
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error deleting one-time client secrets: %v", err))
			return
		}

		logger.Debug().Msgf("disabled service account and revoked client secrets: %v", accountID)
	} else if account.AccountType == "user" {
		// Revoke all refresh tokens for user accounts
		_, err = txQueries.RevokeAllRefreshTokensForUser(r.Context(), accountID)
		if err != nil {
			logger.Error().Err(err).Msgf("error revoking refresh tokens for account: %v", accountID)
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error revoking refresh tokens: %v", err))
			return
		}

		logger.Debug().Msgf("disabled user account and revoked refresh tokens: %v", accountID)
	}

	// Commit transaction
	if err := tx.Commit(r.Context()); err != nil {
		logger.Error().Err(err).Msg("failed to commit transaction")
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to commit transaction: %v", err))
		return
	}

	logger.Info().Msgf("successfully disabled account: %v (type: %v)", accountID, account.AccountType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf("account %v (type %s) disabled", accountID, account.AccountType)))
}

// EnableAccountHandler godoc
//
//	@Summary		Enable an account
//	@Description
//	@Description	For service accounts, you will need to register them again using /auth/register/service-accounts
//	@Description	The client ID will remain the same but they must go through	the setup process again.
//	@Description
//	@Description	For user accounts, they can immediately log in again.
//	@Description
//	@Description	Only owners and admins can enable accounts.
//	@Tags			Site admin
//
//	@Param			account_id	path	string	true	"Account ID to enable"
//
//	@Success		200
//	@Failure		400	{object}	responses.ErrorResponse	"Invalid account ID format"
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Insufficient permissions "
//	@Failure		404	{object}	responses.ErrorResponse	"Account not found"
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/admin/accounts/{account_id}/enable [post]
func (a *AdminHandler) EnableAccountHandler(w http.ResponseWriter, r *http.Request) {
	accountIDString := chi.URLParam(r, "account_id")
	logger := zerolog.Ctx(r.Context())

	// Parse account ID as UUID
	accountID, err := uuid.Parse(accountIDString)
	if err != nil {
		logger.Warn().Msgf("invalid account ID format: %v", accountIDString)
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid account ID format")
		return
	}

	account, err := a.queries.GetAccountByID(r.Context(), accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn().Msgf("account not found: %v", accountID)
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "account not found")
			return
		}
		logger.Error().Err(err).Msgf("database error retrieving account: %v", accountID)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	// Enable the account
	rowsAffected, err := a.queries.EnableAccount(r.Context(), accountID)
	if err != nil {
		logger.Error().Err(err).Msgf("database error enabling account: %v", accountID)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error enabling account: %v", err))
		return
	}

	if rowsAffected == 0 {
		logger.Warn().Msgf("account not found or already enabled: %v", accountID)
		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "account not found or already enabled")
		return
	}

	logger.Info().Msgf("successfully enabled account: %v (type: %v)", accountID, account.AccountType)

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf("account %v (type %s) enabled", accountID, account.AccountType)))
}

// GetUserbyIDHandler godoc
//
//	@Summary	Get registered user
//	@Description
//	@Description	This api displays a site user and their email addreses (can only be used by owner account)
//	@Tags			Site admin
//
//	@Param			id	path		string	true	"user id"	example(68fb5f5b-e3f5-4a96-8d35-cd2203a06f73)
//	@Success		200	{object}	database.GetUserByIDRow
//	@Failure		400	{object}	responses.ErrorResponse	"Invalid user ID format"
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Insufficient permissions "
//	@Failure		404	{object}	responses.ErrorResponse	"User not found"
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/admin/users/{id} [get]
func (a *AdminHandler) GetUserHandler(w http.ResponseWriter, r *http.Request) {

	userAccountIDstring := r.PathValue("id")
	userAccountID, err := uuid.Parse(userAccountIDstring)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, fmt.Sprintf("Invalid user ID: %v", err))
		return
	}

	res, err := a.queries.GetUserByID(r.Context(), userAccountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No user found for id %v", userAccountID))
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user from the database %v", err))
		return
	}
	//
	responses.RespondWithJSON(w, http.StatusOK, res)
}

// GetUsersHandler godoc
//
//	@Summary		Get registered users
//	@Description	This api displays all the site users and their email addreses (can only be used by owner account)
//	@Tags			Site admin
//
//	@Success		200	{array}		database.GetUsersRow
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Insufficient permissions "
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/admin/users [get]
func (a *AdminHandler) GetUsersHandler(w http.ResponseWriter, r *http.Request) {

	res, err := a.queries.GetUsers(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error getting user from database: %v", err))
		return
	}
	responses.RespondWithJSON(w, http.StatusOK, res)
}

// GetServiceAccountHandler godoc
//
//	@Summary		Get service account
//	@Description	Get a specific service account by account ID.
//	@Description	Only owners and admins can view service account details.
//	@Tags			Site admin
//
//	@Param			id	path	string	true	"Service Account ID"
//
//	@Success		200	{object}	database.ServiceAccount
//	@Failure		400	{object}	responses.ErrorResponse	"Invalid service account ID format"
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Insufficient permissions "
//	@Failure		404	{object}	responses.ErrorResponse	"Service account not found"
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/admin/service-accounts/{id} [get]
func (a *AdminHandler) GetServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	serviceAccountIDString := chi.URLParam(r, "id")
	logger := zerolog.Ctx(r.Context())

	serviceAccountID, err := uuid.Parse(serviceAccountIDString)
	if err != nil {
		logger.Warn().Msgf("invalid service account ID format: %v", serviceAccountIDString)
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid service account ID format")
		return
	}

	// Get service account by account ID
	serviceAccount, err := a.queries.GetServiceAccountByAccountID(r.Context(), serviceAccountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn().Msgf("service account not found: %v", serviceAccountID)
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "service account not found")
			return
		}
		logger.Error().Err(err).Msgf("database error retrieving service account: %v", serviceAccountID)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	logger.Info().Msgf("retrieved service account: %v", serviceAccountID)
	responses.RespondWithJSON(w, http.StatusOK, serviceAccount)
}

// GetServiceAccountsHandler godoc
//
//	@Summary		Get all service accounts
//	@Description	Get a list of all service accounts in the system.
//	@Description	Only owners and admins can view service account lists.
//	@Tags			Site admin
//
//	@Success		200	{array}		database.ServiceAccount
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Insufficient permissions "
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/admin/service-accounts [get]
func (a *AdminHandler) GetServiceAccountsHandler(w http.ResponseWriter, r *http.Request) {
	logger := zerolog.Ctx(r.Context())

	// Get all service accounts
	serviceAccounts, err := a.queries.GetServiceAccounts(r.Context())
	if err != nil {
		logger.Error().Err(err).Msg("database error retrieving service accounts")
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	logger.Info().Msgf("retrieved %d service accounts", len(serviceAccounts))
	responses.RespondWithJSON(w, http.StatusOK, serviceAccounts)
}
