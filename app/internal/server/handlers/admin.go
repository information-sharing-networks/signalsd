package handlers

import (
	"context"
	"encoding/json"
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
	ClientID           string    `json:"client_id" example:"client-123"`
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
//	@Description	Service accounts also need re-registration via `/api/auth/register/service-accounts`
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
//	@Description	- **Service Accounts**: Must re-register via `/api/auth/register/service-accounts` (same client_id, new credentials)
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
//	@Description	- to return a specific user supply one of the following query parameters: ?id=uuid or ?email=address
//	@Tags			Site Admin
//
//	@Param			id		query		string					false	"user account ID"		example(68fb5f5b-e3f5-4a96-8d35-cd2203a06f73)
//	@Param			email	query		string					false	"user email address"	example(user@example.com)
//	@Success		200		{array}		handlers.UserDetails	"All users (when no query params)"
//	@Success		200		{object}	handlers.UserDetails	"Specific user (when query params provided)"
//	@Failure		400		{object}	responses.ErrorResponse	"Invalid request - cannot provide both id and email parameters"
//	@Failure		401		{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403		{object}	responses.ErrorResponse	"Insufficient permissions "
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

	// If query parameters provided, return specific user
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
	} else {
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
	}

	responses.RespondWithJSON(w, http.StatusOK, user)
}

// GetServiceAccountHandler godoc
//
//	@Summary		Get service account
//	@Description	Get a specific service account by account ID.
//	@Description	Only owners and admins can view service account details.
//	@Tags			Site Admin
//
//	@Param			id	path		string	true	"Service Account ID"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//
//	@Success		200	{object}	handlers.ServiceAccountDetails
//	@Failure		400	{object}	responses.ErrorResponse	"Invalid service account ID format"
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Insufficient permissions "
//	@Failure		404	{object}	responses.ErrorResponse	"Service account not found"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/service-accounts/{id} [get]
func (a *AdminHandler) GetServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	serviceAccountIDString := r.PathValue("id")

	serviceAccountID, err := uuid.Parse(serviceAccountIDString)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("service_account_id_string", serviceAccountIDString),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid service account ID format")
		return
	}

	// Get service account by account ID
	dbServiceAccount, err := a.queries.GetServiceAccountByAccountID(r.Context(), serviceAccountID)
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

	// Convert database struct to our response struct
	serviceAccount := ServiceAccountDetails{
		AccountID:          dbServiceAccount.AccountID,
		CreatedAt:          dbServiceAccount.CreatedAt,
		UpdatedAt:          dbServiceAccount.UpdatedAt,
		ClientID:           dbServiceAccount.ClientID,
		ClientContactEmail: dbServiceAccount.ClientContactEmail,
		ClientOrganization: dbServiceAccount.ClientOrganization,
	}

	responses.RespondWithJSON(w, http.StatusOK, serviceAccount)
}

// GetServiceAccountsHandler godoc
//
//	@Summary		Get all service accounts
//	@Description	Get a list of all service accounts in the system.
//	@Description	Only owners and admins can view service account lists.
//	@Tags			Site Admin
//
//	@Success		200	{array}		handlers.ServiceAccountDetails
//	@Failure		401	{object}	responses.ErrorResponse	"Authentication failed "
//	@Failure		403	{object}	responses.ErrorResponse	"Insufficient permissions "
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/service-accounts [get]
func (a *AdminHandler) GetServiceAccountsHandler(w http.ResponseWriter, r *http.Request) {

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
}

// Simple password reset types
type ResetUserPasswordRequest struct {
	NewPassword string `json:"new_password"` // Admin provides the new password
}

type ResetUserPasswordResponse struct {
	Message string `json:"message"`
}

// ResetUserPasswordHandler godoc
//
//	@Summary		Reset user password
//	@Description	Allows admins to reset a user's password (use this endpoint if the user has forgotten their password)
//	@Tags			Site Admin
//
//	@Param			user_id	path		string								true	"User Account ID"	example(a38c99ed-c75c-4a4a-a901-c9485cf93cf3)
//	@Param			request	body		handlers.ResetUserPasswordRequest	true	"New password"
//
//	@Success		200		{object}	handlers.ResetUserPasswordResponse
//	@Failure		400		{object}	responses.ErrorResponse
//	@Failure		401		{object}	responses.ErrorResponse	"Unauthorized"
//	@Failure		403		{object}	responses.ErrorResponse	"Forbidden - admin role required"
//	@Failure		404		{object}	responses.ErrorResponse	"User not found"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/users/{user_id}/reset-password [put]
func (a *AdminHandler) ResetUserPasswordHandler(w http.ResponseWriter, r *http.Request) {

	// Get admin account ID from context (set by middleware)
	adminAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "admin account ID not found in context")
		return
	}

	// Get user ID from URL parameter
	userIDStr := r.PathValue("user_id")
	if userIDStr == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "user_id parameter is required")
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam, "invalid user_id format")
		return
	}

	// Parse request body
	var req ResetUserPasswordRequest
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	// Verify user exists
	user, err := a.queries.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "user not found")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("user_id", userID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Validate the new password
	if req.NewPassword == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "new_password is required")
		return
	}

	if len(req.NewPassword) < signalsd.MinimumPasswordLength {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodePasswordTooShort, fmt.Sprintf("password must be at least %d characters", signalsd.MinimumPasswordLength))
		return
	}

	// Hash the new password
	hashedPassword, err := a.authService.HashPassword(req.NewPassword)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "internal server error")
		return
	}

	// Update the password in the database
	rowsAffected, err := a.queries.UpdatePassword(r.Context(), database.UpdatePasswordParams{
		AccountID:      userID,
		HashedPassword: hashedPassword,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("user_id", userID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if rowsAffected != 1 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "password update affected unexpected number of rows")
		return
	}

	logger.ContextWithLogAttrs(r.Context(),
		slog.String("admin_account_id", adminAccountID.String()),
		slog.String("user_id", userID.String()),
	)

	response := ResetUserPasswordResponse{
		Message: fmt.Sprintf("Password successfully reset for user %s", user.Email),
	}

	responses.RespondWithJSON(w, http.StatusOK, response)
}
