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
	errorTemplates "github.com/information-sharing-networks/signalsd/app/internal/server/templates/errors"
	serviceAccountTemplates "github.com/information-sharing-networks/signalsd/app/internal/server/templates/service_accounts"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ServiceAccountHandler struct {
	queries       *database.Queries
	authService   *auth.AuthService
	pool          *pgxpool.Pool
	publicBaseURL string
}

func NewServiceAccountHandler(queries *database.Queries, authService *auth.AuthService, pool *pgxpool.Pool, publicBaseURL string) *ServiceAccountHandler {
	return &ServiceAccountHandler{
		queries:       queries,
		authService:   authService,
		pool:          pool,
		publicBaseURL: publicBaseURL,
	}
}

type CreateServiceAccountRequest struct {
	ClientOrganization string `json:"client_organization" example:"example org"`
	ClientContactEmail string `json:"client_contact_email" example:"example@example.com"`
}

type CreateServiceAccountResponse struct {
	ClientID  string    `json:"client_id" example:"sa_example-org_k7j2m9x1"`
	AccountID uuid.UUID `json:"account_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	SetupURL  string    `json:"setup_url" example:"https://api.example.com/api/auth/service-accounts/setup/550e8400-e29b-41d4-a716-446655440000"`
	ExpiresAt time.Time `json:"expires_at" example:"2024-12-25T10:30:00Z"`
	ExpiresIn int       `json:"expires_in" example:"172800"`
}

type ReissueServiceAccountCredentialsRequest struct {
	ClientID string `json:"client_id" example:"sa_example-org_k7j2m9x1"`
}

type ReissueServiceAccountCredentialsResponse struct {
	ClientID  string    `json:"client_id" example:"sa_example-org_k7j2m9x1"`
	AccountID uuid.UUID `json:"account_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	SetupURL  string    `json:"setup_url" example:"https://api.example.com/api/auth/service-accounts/setup/550e8400-e29b-41d4-a716-446655440000"`
	ExpiresAt time.Time `json:"expires_at" example:"2024-12-25T10:30:00Z"`
	ExpiresIn int       `json:"expires_in" example:"172800"`
}

type SetupPageData struct {
	ClientID     string
	ClientSecret string
	ExpiresAt    time.Time
}

// RegisterServiceAccountHandler godocs
//
//	@Summary		Register service account
//	@Description	Registring a new service account creates a one-time link with the client credentials in it - this must be used by the client within 48 hrs.
//	@Description
//	@Description	Note that where an organization needs more than one service account they must supply unique contact emails for each account.
//	@Description
//	@Description
//	@Description	To reissue credentials for an existing service account, use the **Reissue Service Account Credentials** endpoint.
//	@Description
//	@Description	You have to be an admin or the site owner to use this endpoint
//	@Description
//	@Tags		Service Accounts
//
//	@Param		request	body		handlers.CreateServiceAccountRequest	true	"service account details"
//
//	@Success	201		{object}	handlers.CreateServiceAccountResponse
//	@Failure	400		{object}	responses.ErrorResponse
//	@Failure	401		{object}	responses.ErrorResponse
//	@Failure	409		{object}	responses.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/auth/service-accounts/register [post]
func (s *ServiceAccountHandler) RegisterServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateServiceAccountRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	if req.ClientContactEmail == "" || req.ClientOrganization == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "client_organization, client_contact_email are required")
		return
	}

	// Check if service account already exists
	exists, err := s.queries.ExistsServiceAccountWithOrganizationAndEmail(r.Context(), database.ExistsServiceAccountWithOrganizationAndEmailParams{
		ClientOrganization: req.ClientOrganization,
		ClientContactEmail: req.ClientContactEmail,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}
	if exists {
		responses.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, "service account already exists for this organization and email combination")
		return
	}

	// Create new service account - generate client_id
	clientID, err := utils.GenerateClientID(req.ClientOrganization)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "internal server error")
		return
	}

	// transaction
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
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

	txQueries := s.queries.WithTx(tx)

	logger.ContextWithLogAttrs(r.Context(),
		slog.String("client_id", clientID),
	)

	// create serviceAccount
	serviceAccount, err := txQueries.CreateServiceAccountAccount(r.Context())
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	serviceAccountID := serviceAccount.ID

	// create service account
	_, err = txQueries.CreateServiceAccount(r.Context(), database.CreateServiceAccountParams{
		AccountID:          serviceAccount.ID,
		ClientID:           clientID,
		ClientContactEmail: req.ClientContactEmail,
		ClientOrganization: req.ClientOrganization,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Generate the actual client secret (this gets stored temporarily)
	clientSecret, err := s.authService.GenerateSecureToken(32)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "internal server error")
		return
	}

	// Create one-time secret record (used to complete the set up the service account)
	expiresAt := time.Now().Add(signalsd.OneTimeSecretExpiry)
	oneTimeSecretID, err := txQueries.CreateOneTimeClientSecret(r.Context(), database.CreateOneTimeClientSecretParams{
		ID:                      uuid.New(),
		ServiceAccountAccountID: serviceAccountID,
		PlaintextSecret:         clientSecret,
		ExpiresAt:               expiresAt,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Generate the one-time setup URL using forwarded headers
	setupURL := fmt.Sprintf("%s/api/auth/service-accounts/setup/%s",
		s.publicBaseURL,
		oneTimeSecretID.String(),
	)

	// add setup url and account id to final request log context
	logger.ContextWithLogAttrs(r.Context(),
		slog.String("setup_url", setupURL),
		slog.String("account_id", serviceAccountID.String()),
	)

	// Return the setup information
	response := CreateServiceAccountResponse{
		ClientID:  clientID,
		AccountID: serviceAccountID,
		SetupURL:  setupURL,
		ExpiresAt: expiresAt,
		ExpiresIn: int(signalsd.OneTimeSecretExpiry.Seconds()),
	}

	responses.RespondWithJSON(w, http.StatusCreated, response)
}

// ReissueServiceAccountCredentialsHandler godocs
//
//	@Summary		Reissue service account credentials
//	@Description	Reissue credentials for an existing service account.
//	@Description	This creates a new one-time link with fresh client credentials - this must be used by the client within 48 hrs.
//	@Description
//	@Description	This endpoint revokes all existing client secrets and one-time setup URLs for the service account, then generates new credentials.
//	@Description
//	@Description	The client_id will remain the same, but a new client_secret will be generated.
//	@Description
//	@Description	You have to be an admin or the site owner to use this endpoint
//	@Description
//	@Tags		Service Accounts
//
//	@Param		request	body		handlers.ReissueServiceAccountCredentialsRequest	true	"service account details"
//
//	@Success	200		{object}	handlers.ReissueServiceAccountCredentialsResponse
//	@Failure	400		{object}	responses.ErrorResponse
//	@Failure	401		{object}	responses.ErrorResponse
//	@Failure	404		{object}	responses.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/auth/service-accounts/reissue-credentials [post]
func (s *ServiceAccountHandler) ReissueServiceAccountCredentialsHandler(w http.ResponseWriter, r *http.Request) {
	var req ReissueServiceAccountCredentialsRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	if req.ClientID == "" {

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "client_id is required")
		return
	}

	// Check if service account exists
	serviceAccount, err := s.queries.GetServiceAccountByClientID(r.Context(), req.ClientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "service account not found for this organization and email combination")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	clientID := serviceAccount.ClientID
	serviceAccountID := serviceAccount.AccountID

	// Revoke existing client secrets and one-time secrets
	_, err = s.queries.RevokeAllClientSecretsForAccount(r.Context(), serviceAccount.AccountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("client_id", clientID),
			slog.String("account_id", serviceAccountID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	_, err = s.queries.DeleteOneTimeClientSecretsByOrgAndEmail(r.Context(), database.DeleteOneTimeClientSecretsByOrgAndEmailParams{
		ClientContactEmail: serviceAccount.ClientContactEmail,
		ClientOrganization: serviceAccount.ClientOrganization,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("client_id", clientID),
			slog.String("account_id", serviceAccountID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// transaction
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
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

	txQueries := s.queries.WithTx(tx)

	// Generate the actual client secret (this gets stored temporarily)
	clientSecret, err := s.authService.GenerateSecureToken(32)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "internal server error")
		return
	}

	// Create one-time secret record
	expiresAt := time.Now().Add(signalsd.OneTimeSecretExpiry)
	oneTimeSecretID, err := txQueries.CreateOneTimeClientSecret(r.Context(), database.CreateOneTimeClientSecretParams{
		ID:                      uuid.New(),
		ServiceAccountAccountID: serviceAccountID,
		PlaintextSecret:         clientSecret,
		ExpiresAt:               expiresAt,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Generate the one-time setup URL using forwarded headers
	setupURL := fmt.Sprintf("%s/api/auth/service-accounts/setup/%s",
		s.publicBaseURL,
		oneTimeSecretID.String(),
	)

	// add setup url and account id to final request log context
	logger.ContextWithLogAttrs(r.Context(),
		slog.String("setup_url", setupURL),
		slog.String("client_id", clientID),
		slog.String("account_id", serviceAccountID.String()),
	)

	// Return the setup information
	response := ReissueServiceAccountCredentialsResponse{
		ClientID:  clientID,
		AccountID: serviceAccountID,
		SetupURL:  setupURL,
		ExpiresAt: expiresAt,
		ExpiresIn: int(signalsd.OneTimeSecretExpiry.Seconds()),
	}

	responses.RespondWithJSON(w, http.StatusOK, response)
}

// SetupServiceAccountHandler godoc
//
//	@Summary		Complete service account setup
//	@Description	Exchange one-time setup token for permanent client credentials (the one-time request url is created when a new service account is registered).
//	@Description	the endpoint renders a html page that the user can use to copy their client credentials.
//	@Description	The setup url is only valid for 48 hours.
//	@Description
//	@Tags		Service Accounts
//
//	@Param		setup_id	path	string	true	"One-time setup ID"	example(550e8400-e29b-41d4-a716-446655440000)
//
//	@Success	201
//
//	@Failure	400	{object}	responses.ErrorResponse
//	@Failure	404	{object}	responses.ErrorResponse
//	@Failure	410	{object}	responses.ErrorResponse
//
//	@Router		/api/auth/service-accounts/setup/{setup_id} [get]
func (s *ServiceAccountHandler) SetupServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	// Extract token from URL path
	oneTimeSecretIDString := r.PathValue("setup_id")

	// Parse token as UUID
	oneTimeSecretID, err := uuid.Parse(oneTimeSecretIDString)
	if err != nil {

		s.renderErrorPage(w, "Invalid Setup ID", "The setup ID you provided is not valid. Please check the URL and try again.")
		return
	}

	// Start transaction
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

			s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
			return
		}
	}()

	txQueries := s.queries.WithTx(tx)

	// Retrieve and validate one-time secret
	oneTimeSecret, err := txQueries.GetOneTimeClientSecret(r.Context(), oneTimeSecretID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", "setup id has already been used or is no longer valid"),
			)

			s.renderErrorPage(w, "set up ID not found ", "The setup ID you provided has already been used or is no longer valid")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	// Check if token has expired
	if time.Now().After(oneTimeSecret.ExpiresAt) {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error:", "setup id has expired"),
		)

		s.renderErrorPage(w, "set up ID not found or already used", "The setup ID you provided has already been used or is no longer valid")
		return
	}

	serviceAccount, err := txQueries.GetServiceAccountByAccountID(r.Context(), oneTimeSecret.ServiceAccountAccountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	// Revoke any existing client secrets for this service account
	_, err = txQueries.RevokeAllClientSecretsForAccount(r.Context(), oneTimeSecret.ServiceAccountAccountID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	// Create long-term client secret
	hashedSecret := s.authService.HashToken(oneTimeSecret.PlaintextSecret)
	expiresAt := time.Now().Add(signalsd.ClientSecretExpiry)

	_, err = txQueries.CreateClientSecret(r.Context(), database.CreateClientSecretParams{
		HashedSecret:            hashedSecret,
		ServiceAccountAccountID: oneTimeSecret.ServiceAccountAccountID,
		ExpiresAt:               expiresAt,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	// Delete the one-time secret
	_, err = txQueries.DeleteOneTimeClientSecret(r.Context(), oneTimeSecretID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	// Prepare template data
	data := serviceAccountTemplates.SetupPageData{
		ClientID:     serviceAccount.ClientID,
		ClientSecret: oneTimeSecret.PlaintextSecret,
		ExpiresAt:    expiresAt,
	}

	logger.ContextWithLogAttrs(r.Context(),
		slog.String("client_id", serviceAccount.ClientID),
		slog.String("account_id", serviceAccount.AccountID.String()),
	)

	// Render service account setup page using templ
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusCreated)

	if err := serviceAccountTemplates.SetupPage(data).Render(r.Context(), w); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}
}

func (s *ServiceAccountHandler) renderErrorPage(w http.ResponseWriter, title, message string) {
	data := errorTemplates.ErrorPageData{
		Title:   title,
		Message: message,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)

	if err := errorTemplates.ErrorPage(data).Render(context.Background(), w); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}
