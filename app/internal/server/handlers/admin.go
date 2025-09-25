package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/version"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AdminHandler struct {
	queries     *database.Queries
	pool        *pgxpool.Pool
	authService *auth.AuthService
}

func NewAdminHandler(queries *database.Queries, pool *pgxpool.Pool, authService *auth.AuthService) *AdminHandler {
	return &AdminHandler{
		queries:     queries,
		pool:        pool,
		authService: authService,
	}
}

// Response structs for GET handlers
type UserDetails struct {
	AccountID uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	Email     string    `json:"email" example:"user@example.com"`
	UserRole  string    `json:"user_role" example:"admin" enums:"owner,admin,member"`
	CreatedAt time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`
}

type ServiceAccountDetails struct {
	AccountID          uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	CreatedAt          time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt          time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`
	ClientID           string    `json:"client_id" example:"sa_exampleorg_k7j2m9x1"`
	ClientContactEmail string    `json:"client_contact_email" example:"contact@example.com"`
	ClientOrganization string    `json:"client_organization" example:"Example Organization"`
}

// ResetHandler godoc
//
//	@Summary		reset
//	@Description	Delete all registered users and associated data.
//	@Description	This endpoint only works on environments configured as 'dev'
//	@Tags			Site Admin
//
//	@Success		200
//	@Failure		403	{object}	responses.ErrorResponse
//
//	@Router			/api/admin/reset [post]
func (a *AdminHandler) ResetHandler(w http.ResponseWriter, r *http.Request) {

	deletedAccountsCount, err := a.queries.DeleteAccounts(r.Context())
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf("%d accounts deleted", deletedAccountsCount)))
}

// ReadinessHandler godoc
//
//	@Summary		Readiness Check
//	@Description	Check if the signalsd service is ready to accept traffic.
//	@Tags			Health
//	@Produce		plain
//
//	@Success		200	{string}	string	"OK - Service is ready"
//	@Failure		503	{string}	string	"Service Unavailable - Database connection failed"
//
//	@Router			/health/ready [get]
func (a *AdminHandler) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), signalsd.ReadinessTimeout)
	defer cancel()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	_, err := a.queries.IsDatabaseRunning(ctx)
	if err == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Service Unavailable"))
	}
}

// LivenessHandler godoc
//
//	@Summary		Liveness Check
//	@Description	Check if the signalsd http service is alive and responding.
//	@Tags			Health
//	@Produce		plain
//
//	@Success		200	{string}	string	"OK - Service is alive"
//
//	@Router			/health/live [get]
func (a *AdminHandler) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// VersionHandler godoc
//
//	@Summary		Get API version
//	@Description	Returns the current API version details
//	@Tags			Site Admin
//
//	@Success		200	{object}	version.Info
//
//	@Router			/version [get]
func (a *AdminHandler) VersionHandler(w http.ResponseWriter, r *http.Request) {
	responses.RespondWithJSON(w, http.StatusOK, version.Get())
}

// DisableAccountHandler godoc
//
//	@Summary	Disable an account
//	@Description
//	@Description	**Use Cases:**
//	@Description	- **Security Incident**: Compromised account needs immediate lockout
//	@Description	- **Employee Departure**: Remove access for departed staff
//	@Description
//	@Description	**Actions Performed:**
//	@Description	- Sets `is_active = false` (account becomes unusable)
//	@Description	- Revokes all client secrets/one-time secrets (service accounts)
//	@Description	- Revokes all refresh tokens (web users)
//	@Description
//	@Description	**Recovery:** Account must be re-enabled by admin via `/admin/accounts/{id}/enable`
//	@Description	Service accounts will also need a new client secret via `/api/auth/service-accounts/reissue-credentials`
//	@Description
//	@Description	**Note:** The site owner account cannot be disabled to prevent system lockout.
//	@Description	Only owners and admins can disable accounts.
//	@Tags			Site Admin
//
//	@Param			account_id	path	string	true	"Account ID to disable"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		200
//	@Failure		400	{object}	responses.ErrorResponse	"Invalid account ID format"
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Cannot disable site owner account"
//	@Failure		404	{object}	responses.ErrorResponse	"Account not found"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/accounts/{account_id}/disable [post]
func (a *AdminHandler) DisableAccountHandler(w http.ResponseWriter, r *http.Request) {
	accountIDString := r.PathValue("account_id")

	accountID, err := uuid.Parse(accountIDString)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid account ID format")
		return
	}

	// Start transaction
	tx, err := a.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

		}
	}()

	txQueries := a.queries.WithTx(tx)

	account, err := txQueries.GetAccountByID(r.Context(), accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", "account not found"),
			)

			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "account not found")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Prevent disabling the site owner account
	if account.AccountRole == "owner" {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", "cannot disable the site owner account"),
		)

		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "cannot disable the site owner account")
		return
	}

	// Disable the account
	rowsAffected, err := txQueries.DisableAccount(r.Context(), accountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if rowsAffected == 0 {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error_code", "account not found or already disabled"),
		)

		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "account not found or already disabled")
		return
	}

	// Revoke tokens based on account type
	switch account.AccountType {
	case "service_account":
		// Revoke all client secrets for service accounts
		_, err = txQueries.RevokeAllClientSecretsForAccount(r.Context(), accountID)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}

		// Delete any one-time client secrets
		serviceAccount, err := txQueries.GetServiceAccountByAccountID(r.Context(), accountID)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}

		_, err = txQueries.DeleteOneTimeClientSecretsByOrgAndEmail(r.Context(), database.DeleteOneTimeClientSecretsByOrgAndEmailParams{
			ClientOrganization: serviceAccount.ClientOrganization,
			ClientContactEmail: serviceAccount.ClientContactEmail,
		})
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}

	case "user":
		// Revoke all refresh tokens for user accounts
		_, err = txQueries.RevokeAllRefreshTokensForUser(r.Context(), accountID)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}

	}

	// Commit transaction
	if err := tx.Commit(r.Context()); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf("account %v (type %s) disabled", accountID, account.AccountType)))
}

// EnableAccountHandler godoc
//
//	@Summary		Enable an account
//	@Description	**Administrative endpoint to re-enable previously disabled accounts.**
//	@Description	Sets account status to `is_active = true` (does not create new tokens).
//	@Description
//	@Description	**Post-Enable Steps Required:**
//	@Description	- **Service Accounts**: will need a new client secret via `/api/auth/service-accounts/reissue_credentials`
//	@Description	- **Web Users**: Can immediately log in again via `/api/auth/login`
//	@Description
//	@Description	Only owners and admins can enable accounts.
//	@Tags			Site Admin
//
//	@Param			account_id	path	string	true	"Account ID to enable"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		200
//	@Failure		400	{object}	responses.ErrorResponse	"Invalid account ID format"
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Insufficient permissions "
//	@Failure		404	{object}	responses.ErrorResponse	"Account not found"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/accounts/{account_id}/enable [post]
func (a *AdminHandler) EnableAccountHandler(w http.ResponseWriter, r *http.Request) {
	accountIDString := r.PathValue("account_id")

	// Parse account ID as UUID
	accountID, err := uuid.Parse(accountIDString)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("account_id_string", accountIDString),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid account ID format")
		return
	}

	account, err := a.queries.GetAccountByID(r.Context(), accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", "account not found"),
			)

			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "account not found")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Enable the account
	rowsAffected, err := a.queries.EnableAccount(r.Context(), accountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if rowsAffected == 0 {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", "account not found or already enabled"),
		)

		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "account not found or already enabled")
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf("account %v (type %s) enabled", accountID, account.AccountType)))
}

// GetUsersHandler godoc
//
//	@Summary		Get users
//	@Description	This api displays site users and their email addresses (can only be used by owner and admin accounts)
//	@Description
//	@Description	- No query parameters = return all users
//	@Description	- to return a specific user supply one of the following query parameters: `?id=uuid` or `?email=address`
//	@Tags			Site Admin
//
//	@Param			id		query		string					false	"user account ID"		example(68fb5f5b-e3f5-4a96-8d35-cd2203a06f73)
//	@Param			email	query		string					false	"user email address"	example(user@example.com)
//	@Success		200		{array}		handlers.UserDetails	"All users (when no query params)"
//	@Success		200		{object}	handlers.UserDetails	"Specific user (when query params provided)"
//	@Failure		400		{object}	responses.ErrorResponse	"Invalid request - cannot provide both id and email parameters"
//	@Failure		401		{object}	responses.ErrorResponse	"Authentication failed"
//	@Failure		403		{object}	responses.ErrorResponse	"Insufficient permissions"
//	@Failure		404		{object}	responses.ErrorResponse	"User not found"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/users [get]
func (a *AdminHandler) GetUsersHandler(w http.ResponseWriter, r *http.Request) {

	accountIdParam := r.URL.Query().Get("id")
	emailParam := r.URL.Query().Get("email")

	// If no query parameters, return all users
	if accountIdParam == "" && emailParam == "" {
		dbUsers, err := a.queries.GetUsers(r.Context())
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}

		users := make([]UserDetails, len(dbUsers))
		for i, dbUser := range dbUsers {
			users[i] = UserDetails{
				AccountID: dbUser.AccountID,
				Email:     dbUser.Email,
				UserRole:  dbUser.UserRole,
				CreatedAt: dbUser.CreatedAt,
				UpdatedAt: dbUser.UpdatedAt,
			}
		}

		responses.RespondWithJSON(w, http.StatusOK, users)
		return
	}

	// If query parameters provided
	if accountIdParam != "" && emailParam != "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "Cannot provide both 'id' and 'email' query parameters")
		return
	}

	var user UserDetails

	if accountIdParam != "" {
		// Lookup by ID
		userAccountID, err := uuid.Parse(accountIdParam)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "invalid user ID format")
			return
		}

		dbUser, err := a.queries.GetUserByID(r.Context(), userAccountID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No user found for ID %v", userAccountID))
				return
			}
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}

		// Convert GetUserByIDRow to UserDetails
		user = UserDetails{
			AccountID: dbUser.AccountID,
			Email:     dbUser.Email,
			UserRole:  dbUser.UserRole,
			CreatedAt: dbUser.CreatedAt,
			UpdatedAt: dbUser.UpdatedAt,
		}

		responses.RespondWithJSON(w, http.StatusOK, user)
		return
	}

	// Lookup by email
	dbUser, err := a.queries.GetUserByEmail(r.Context(), emailParam)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No user found for email %v", emailParam))
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("email", emailParam),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Convert User to UserDetails
	user = UserDetails{
		AccountID: dbUser.AccountID,
		Email:     dbUser.Email,
		UserRole:  dbUser.UserRole,
		CreatedAt: dbUser.CreatedAt,
		UpdatedAt: dbUser.UpdatedAt,
	}

	responses.RespondWithJSON(w, http.StatusOK, user)
}

// GetServiceAccountsHandler godoc
//
//	@Summary		Get service accounts
//	@Description	Only owners and admins can view service account lists.
//	@Description
//	@Description	To return a specific service account supply one of the following query parameter combinations:
//	@Description	-	id (account ID)
//	@Description	-	client_id
//	@Description	-	client_email & client_organization
//	@Descriotion
//	@Description	No query parameters = return all service accounts
//	@Description
//	@Tags		Site Admin
//
//	@Param		id					query		string	false	"Service Account ID"													example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//	@Param		client_id			query		string	false	"Service Account Client ID"												example(sa_exampleorg_k7j2m9x1)
//	@Param		client_email		query		string	false	"Service Account Contact Email (must be used with client_organization)"	example(contact@example.com)
//	@Param		client_organization	query		string	false	"Service Account Organization (must be used with client_email)"			example(Example Org)
//
//	@Success	200					{array}		handlers.ServiceAccountDetails
//	@Failure	401					{object}	responses.ErrorResponse	"Authentication failed"
//	@Failure	403					{object}	responses.ErrorResponse	"Insufficient permissions"
//
//	@Security	BearerAccessToken
//
//	@Router		/api/admin/service-accounts [get]
func (a *AdminHandler) GetServiceAccountsHandler(w http.ResponseWriter, r *http.Request) {

	accountIDString := r.URL.Query().Get("id")
	clientID := r.URL.Query().Get("client_id")
	clientEmail := r.URL.Query().Get("client_email")
	clientOrganization := r.URL.Query().Get("client_organization")

	// if no query parameters provided, return all service accounts
	if accountIDString == "" && clientID == "" && clientEmail == "" && clientOrganization == "" {
		// Get all service accounts
		dbServiceAccounts, err := a.queries.GetServiceAccounts(r.Context())
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}

		// Convert database structs to our response structs
		serviceAccounts := make([]ServiceAccountDetails, len(dbServiceAccounts))
		for i, dbServiceAccount := range dbServiceAccounts {
			serviceAccounts[i] = ServiceAccountDetails{
				AccountID:          dbServiceAccount.AccountID,
				CreatedAt:          dbServiceAccount.CreatedAt,
				UpdatedAt:          dbServiceAccount.UpdatedAt,
				ClientID:           dbServiceAccount.ClientID,
				ClientContactEmail: dbServiceAccount.ClientContactEmail,
				ClientOrganization: dbServiceAccount.ClientOrganization,
			}
		}

		logger.ContextWithLogAttrs(r.Context(),
			slog.Int("count", len(serviceAccounts)),
		)

		responses.RespondWithJSON(w, http.StatusOK, serviceAccounts)
		return
	}

	// Validate query parameter combinations
	paramCount := 0
	if accountIDString != "" {
		paramCount++
	}
	if clientID != "" {
		paramCount++
	}
	if clientEmail != "" || clientOrganization != "" {
		paramCount++
	}

	if paramCount > 1 {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "Cannot provide multiple query parameter combinations. Use only one of: 'id', 'client_id', or 'client_email & client_organization'")
		return
	}

	// Validate that email and organization are used together
	if (clientEmail != "" && clientOrganization == "") || (clientEmail == "" && clientOrganization != "") {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "Both 'client_email' and 'client_organization' parameters are required when querying by email/organization")
		return
	}

	// query by account id
	if accountIDString != "" {

		accountID, err := uuid.Parse(accountIDString)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "Invalid account ID format")
			return
		}

		dbServiceAccount, err := a.queries.GetServiceAccountByAccountID(r.Context(), accountID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "service account not found")
				return
			}

			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
		serviceAccountDetails := ServiceAccountDetails{
			AccountID:          dbServiceAccount.AccountID,
			CreatedAt:          dbServiceAccount.CreatedAt,
			UpdatedAt:          dbServiceAccount.UpdatedAt,
			ClientID:           dbServiceAccount.ClientID,
			ClientContactEmail: dbServiceAccount.ClientContactEmail,
			ClientOrganization: dbServiceAccount.ClientOrganization,
		}
		responses.RespondWithJSON(w, http.StatusOK, serviceAccountDetails)
		return
	}

	// query by email and organization
	if clientEmail != "" && clientOrganization != "" {
		dbServiceAccount, err := a.queries.GetServiceAccountWithOrganizationAndEmail(r.Context(), database.GetServiceAccountWithOrganizationAndEmailParams{
			ClientOrganization: clientOrganization,
			ClientContactEmail: clientEmail,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "service account not found")
				return
			}

			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
		serviceAccountDetails := ServiceAccountDetails{
			AccountID:          dbServiceAccount.AccountID,
			CreatedAt:          dbServiceAccount.CreatedAt,
			UpdatedAt:          dbServiceAccount.UpdatedAt,
			ClientID:           dbServiceAccount.ClientID,
			ClientContactEmail: dbServiceAccount.ClientContactEmail,
			ClientOrganization: dbServiceAccount.ClientOrganization,
		}
		responses.RespondWithJSON(w, http.StatusOK, serviceAccountDetails)
		return
	}

	// query by clientId
	dbServiceAccount, err := a.queries.GetServiceAccountByClientID(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "service account not found")
			return
		}

		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}
	serviceAccountDetails := ServiceAccountDetails{
		AccountID:          dbServiceAccount.AccountID,
		CreatedAt:          dbServiceAccount.CreatedAt,
		UpdatedAt:          dbServiceAccount.UpdatedAt,
		ClientID:           dbServiceAccount.ClientID,
		ClientContactEmail: dbServiceAccount.ClientContactEmail,
		ClientOrganization: dbServiceAccount.ClientOrganization,
	}
	responses.RespondWithJSON(w, http.StatusOK, serviceAccountDetails)
}

// Password reset link generation types
type GeneratePasswordResetLinkResponse struct {
	UserEmail string    `json:"user_email" example:"user@example.com"`
	AccountID uuid.UUID `json:"account_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	ResetURL  string    `json:"reset_url" example:"https://api.example.com/api/auth/password-reset/0ce71234-34d5-4fb5-beb8-ad50d8b40c7d"`
	ExpiresAt time.Time `json:"expires_at" example:"2024-12-25T10:30:00Z"`
	ExpiresIn int       `json:"expires_in" example:"1800"`
}

// GeneratePasswordResetLinkHandler godoc
//
//	@Summary		Generate password reset link
//	@Description	Allows admins or the site owner to generate a one-time password reset link for a user (use this endpoint when a user has forgotten their password)
//	@Description
//	@Description	The generated link can be used to reset the password of the associated account using the page rendered by the PasswordResetTokenPageHandler.
//	@Description	The generated link expires in 30 minutes and can only be used once.
//	@Description
//	@Description	Admins can create links on behalf of users with a member role.  The site owner role can create links for admins and members.
//	@Description
//	@Description	**Note:** The generated link can be used by any user in possession of the link to reset the password of the associated account.
//	@Description	The link should be treated as sensitive and protected accordingly.
//	@Tags			Site Admin
//
//	@Param			user_id	path		string	true	"User Account ID"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		200		{object}	handlers.GeneratePasswordResetLinkResponse
//	@Failure		400		{object}	responses.ErrorResponse
//	@Failure		401		{object}	responses.ErrorResponse	"Unauthorized"
//	@Failure		403		{object}	responses.ErrorResponse	"Forbidden - admin role required"
//	@Failure		404		{object}	responses.ErrorResponse	"User not found"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/users/{user_id}/generate-password-reset-link [post]
//
//	this handler must use the RequireRole (admin/owner) middleware
func (a *AdminHandler) GeneratePasswordResetLinkHandler(w http.ResponseWriter, r *http.Request) {

	// Get account ID from context (set by middleware)
	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "account ID not found in context")
		return
	}

	// verify the account generating the request is an admin or owner
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	if claims.Role != "owner" && claims.Role != "admin" {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you do not have permission to generate password reset links")
		return
	}

	// Get user ID from URL parameter
	tagetUserIDStr := r.PathValue("user_id")
	if tagetUserIDStr == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "user_id parameter is required")
		return
	}

	tagetUserID, err := uuid.Parse(tagetUserIDStr)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid user_id format")
		return
	}

	// Verify user exists
	user, err := a.queries.GetUserByID(r.Context(), tagetUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "user not found")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("user_id", tagetUserID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// admins can only update members
	if claims.Role == "admin" && user.UserRole != "member" {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "admins cannot generate password reset for other admins or site owners")
		return
	}
	// Delete any existing password reset tokens for this user (following service account pattern)
	_, err = a.queries.DeletePasswordResetTokensForUser(r.Context(), tagetUserID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("user_id", tagetUserID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Create password reset token record
	expiresAt := time.Now().Add(signalsd.PasswordResetExpiry)
	tokenID, err := a.queries.CreatePasswordResetToken(r.Context(), database.CreatePasswordResetTokenParams{
		ID:               uuid.New(),
		UserAccountID:    tagetUserID,
		ExpiresAt:        expiresAt,
		CreatedByAdminID: accountID,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("user_id", tagetUserID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Generate the one-time password reset URL using forwarded headers
	resetURL := fmt.Sprintf("%s/api/auth/password-reset/%s",
		signalsd.GetPublicBaseURL(r),
		tokenID.String(),
	)

	// Add reset URL and account ID to final request log context
	logger.ContextWithLogAttrs(r.Context(),
		slog.String("reset_url", resetURL),
		slog.String("user_id", tagetUserID.String()),
		slog.String("admin_account_id", accountID.String()),
	)

	// Return the reset link information
	response := GeneratePasswordResetLinkResponse{
		UserEmail: user.Email,
		AccountID: tagetUserID,
		ResetURL:  resetURL,
		ExpiresAt: expiresAt,
		ExpiresIn: int(signalsd.PasswordResetExpiry.Seconds()),
	}

	responses.RespondWithJSON(w, http.StatusOK, response)
}
