package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	signalsd "github.com/information-sharing-networks/signalsd/app"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// template for service account setup confirmation
const setupPageTemplate = `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Service Account Setup</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 600px; margin: 50px auto; padding: 20px; }
        .container { background: #f8f9fa; border-radius: 8px; padding: 30px; }
        .success { color: #28a745; font-size: 24px; margin-bottom: 20px; }
        .credential { background: white; border: 1px solid #dee2e6; border-radius: 4px; padding: 15px; margin: 15px 0; }
        .label { font-weight: bold; color: #495057; margin-bottom: 5px; }
        .value { font-family: monospace; background: #f8f9fa; padding: 8px; border-radius: 3px; word-break: break-all; }
        .warning { background: #fff3cd; border: 1px solid #ffeaa7; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .warning-title { font-weight: bold; color: #856404; }
        .expiry { text-align: center; margin: 20px 0; padding: 15px; background: #d1ecf1; border-radius: 4px; }
        .copy-btn { background: #007bff; color: white; border: none; padding: 5px 10px; border-radius: 3px; cursor: pointer; margin-left: 10px; }
        .copy-btn:hover { background: #0056b3; }
    </style>
</head>
<body>
    <div class="container">
        <div class="success">✓ Signalds Service Account Setup Complete</div>

        <div class="credential">
            <div class="label">Client ID</div>
            <div class="value">{{.ClientID}} <button class="copy-btn" onclick="copy('{{.ClientID}}', this)">Copy</button></div>
        </div>

        <div class="credential">
            <div class="label">Client Secret</div>
            <div class="value">{{.ClientSecret}} <button class="copy-btn" onclick="copy('{{.ClientSecret}}', this)">Copy</button></div>
        </div>

        <div class="warning">
            <div class="warning-title">⚠️ Important</div>
            This is the only time you'll see the client secret.
            These credentials expire on {{.ExpiresAt.Format "January 2, 2006"}}.
        </div>

        <h3>Next Steps:</h3>
		<p>Store the client ID and secret securely. You will need them to authenticate with the API.</p>
        <p>access tokens are issued by calling the /oauth/token endpoint with your client_id and client_secret in the request body</p>
        <p>When using the API include the access token as: <code>Authorization: Bearer &lt;token&gt;</code></li>
    </div>

	<script>
		function copy(text, btn) {
			navigator.clipboard.writeText(text).then(() => {
				btn.textContent = '✓ Copied!';
				btn.style.background = '#28a745';
				btn.disabled = true;
				setTimeout(() => {
					btn.textContent = 'Copy';
					btn.style.background = '#007bff';
					btn.disabled = false;
				}, 1500);
			});
		}
	</script>
</body>
</html>`

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

type SetupPageData struct {
	ClientID     string
	ClientSecret string
	ExpiresAt    time.Time
}

// RegisterServiceAccountHandler godocs
//
//	@Summary		Register a new service account
//	@Description	Registring a new service account creates a one time link with the client credentials in it - this must be used by the client within 48 hrs.
//	@Description
//	@Description	If you want to reissue a client's credentials call this endpoint again with the same client organization and contact email.
//	@Description	A new one time setup url will be generated and the old one will be revoked.
//	@Description	Note the client_id will remain the same and any existing client secrets will be revoked.
//	@Description
//	@Description	You have to be an admin or the site owner to use this endpoint
//	@Description
//	@Tags		Service accounts
//
//	@Param		request	body		handlers.CreateServiceAccountRequest	true	"service account details"
//
//	@Success	200		{object}	handlers.CreateServiceAccountResponse
//	@Failure	400		{object}	responses.ErrorResponse
//	@Failure	401		{object}	responses.ErrorResponse
//
//	@Security	BearerServiceAccount
//
//	@Router		/api/auth/register/service-accounts [post]
func (s *ServiceAccountHandler) RegisterServiceAccountHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateServiceAccountRequest
	logger := zerolog.Ctx(r.Context())

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.ClientContactEmail == "" || req.ClientOrganization == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "client_organization, client_contact_email are required")
		return
	}

	clientAlreadyExists := false
	clientID := ""
	serviceAccountID := uuid.UUID{}

	serviceAccount, err := s.queries.GetServiceAccountWithOrganizationAndEmail(r.Context(), database.GetServiceAccountWithOrganizationAndEmailParams{
		ClientOrganization: req.ClientOrganization,
		ClientContactEmail: req.ClientContactEmail,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		// new service account - create client_id
		clientID, err = utils.GenerateClientID(req.ClientOrganization)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("could not generate client id: %v", err))
			return
		}
	} else {
		// service account exists: reset the one time password, revoke any client secrets and reissue a one time url
		clientAlreadyExists = true
		clientID = serviceAccount.ClientID
		serviceAccountID = serviceAccount.AccountID

		logger.Info().Msgf("service account %v already exists - revoking exitng client secrets", clientID)

		_, err := s.queries.RevokeAllClientSecretsForAccount(r.Context(), serviceAccount.AccountID)
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error revoking client secrets: %v", err))
			return
		}

		_, err = s.queries.DeleteOneTimeClientSecretsByOrgAndEmail(r.Context(), database.DeleteOneTimeClientSecretsByOrgAndEmailParams{
			ClientContactEmail: req.ClientContactEmail,
			ClientOrganization: req.ClientOrganization,
		})
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error deleting one time client secrets: %v", err))
			return
		}
	}

	// transaction
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to begin transaction: %v", err))
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to rollback transaction: %v", err))
			return
		}
	}()

	txQueries := s.queries.WithTx(tx)

	if !clientAlreadyExists {
		logger.Info().Msgf("creating new service account %v", clientID)

		// create serviceAccount
		serviceAccount, err := txQueries.CreateServiceAccountAccount(r.Context())
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert account record: %v", err))
			return
		}

		serviceAccountID = serviceAccount.ID

		// create service account
		_, err = txQueries.CreateServiceAccount(r.Context(), database.CreateServiceAccountParams{
			AccountID:          serviceAccount.ID,
			ClientID:           clientID,
			ClientContactEmail: req.ClientContactEmail,
			ClientOrganization: req.ClientOrganization,
		})
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert service account record: %v", err))
			return
		}
	}

	// Generate the actual client secret (this gets stored temporarily)
	clientSecret, err := s.authService.GenerateSecureToken(32)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("could not generate client secret: %v", err))
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
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert one time client secret record: %v", err))
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to commit transaction: %v", err))
		return
	}

	// Generate the one-time setup URL
	setupURL := fmt.Sprintf("%s://%s/api/auth/service-accounts/setup/%s",
		utils.GetScheme(r),
		r.Host,
		oneTimeSecretID.String(),
	)

	logger.Info().Msgf("service account %v created - setup url: %v", clientID, setupURL)

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

// SetupServiceAccountHandler godoc
//
//	@Summary		Complete service account setup
//	@Description	Exchange one-time setup token for permanent client credentials (the one-time request url is created when a new service account is registered).
//	@Description	the endpoint renders a html page that the user can use to copy their client credentials.
//	@Description	The setup url is only valid for 48 hours.
//	@Description
//	@Tags		Service accounts
//
//	@Param		setup_id	path	string	true	"One-time setup ID"
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
	oneTimeSecretIDString := chi.URLParam(r, "setup_id")

	logger := zerolog.Ctx(r.Context())

	// Parse token as UUID
	oneTimeSecretID, err := uuid.Parse(oneTimeSecretIDString)
	if err != nil {
		logger.Warn().Msg("invalid setup ID format")
		s.renderErrorPage(w, "Invalid Setup ID", "The setup ID you provided is not valid. Please check the URL and try again.")
		return
	}

	// Start transaction
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		logger.Error().Err(err).Msg("failed to begin transaction")
		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.Error().Err(err).Msg("failed to rollback transaction")
			s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
			return
		}
	}()

	txQueries := s.queries.WithTx(tx)

	// Retrieve and validate one-time secret
	oneTimeSecret, err := txQueries.GetOneTimeClientSecret(r.Context(), oneTimeSecretID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logger.Warn().Msgf("setup ID %v not found or already used", oneTimeSecretID)
			s.renderErrorPage(w, "set up ID not found ", "The setup ID you provided has already been used or is no longer valid")
			return
		}
		logger.Error().Err(err).Msg("database error")
		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	// Check if token has expired
	if time.Now().After(oneTimeSecret.ExpiresAt) {
		logger.Warn().Msgf("setup ID %v has expired", oneTimeSecretID)
		s.renderErrorPage(w, "set up ID not found or already used", "The setup ID you provided has already been used or is no longer valid")
		return
	}

	serviceAccount, err := txQueries.GetServiceAccountByAccountID(r.Context(), oneTimeSecret.ServiceAccountAccountID)
	if err != nil {
		logger.Error().Err(err).Msg("database error - could not retrieve service account")
		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	// Revoke any existing client secrets for this service account
	_, err = txQueries.RevokeAllClientSecretsForAccount(r.Context(), oneTimeSecret.ServiceAccountAccountID)
	if err != nil {
		logger.Error().Err(err).Msg("database error - could not revoke existing secrets")
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
		logger.Error().Err(err).Msg("database error - could not create client secret")
		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	// Delete the one-time secret
	_, err = txQueries.DeleteOneTimeClientSecret(r.Context(), oneTimeSecretID)
	if err != nil {
		logger.Error().Err(err).Msg("database error - could not delete one-time client secret")
		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		logger.Error().Err(err).Msg("database error - failed to commit transaction")
		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	template, err := template.New("setup").Parse(setupPageTemplate)
	if err != nil {
		logger.Error().Err(err).Msg("template parsing error")
		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}

	// Prepare html response
	data := SetupPageData{
		ClientID:     serviceAccount.ClientID,
		ClientSecret: oneTimeSecret.PlaintextSecret,
		ExpiresAt:    expiresAt,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusCreated)

	if err := template.Execute(w, data); err != nil {
		logger.Error().Err(err).Msg("template execution error")
		s.renderErrorPage(w, "Internal Server Error", "Please try again later.")
		return
	}
}

func (s *ServiceAccountHandler) renderErrorPage(w http.ResponseWriter, title, message string) {
	errorHTML := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>` + title + `</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 600px; margin: 50px auto; padding: 20px; }
        .container { background: #f8f9fa; border-radius: 8px; padding: 30px; text-align: center; }
        .error { color: #dc3545; font-size: 24px; margin-bottom: 20px; }
        .message { color: #495057; margin-bottom: 30px; line-height: 1.5; }
        .back-link { color: #007bff; text-decoration: none; }
        .back-link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <div class="error">⚠️ ` + title + `</div>
        <div class="message">` + message + `</div>
        <a href="javascript:history.back()" class="back-link">← Go Back</a>
    </div>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(errorHTML))
}
