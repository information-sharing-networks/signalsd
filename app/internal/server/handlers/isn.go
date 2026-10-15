package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IsnHandler struct {
	queries       *database.Queries
	pool          *pgxpool.Pool
	publicBaseURL string
}

func NewIsnHandler(queries *database.Queries, pool *pgxpool.Pool, publicBaseURL string) *IsnHandler {
	return &IsnHandler{
		queries:       queries,
		pool:          pool,
		publicBaseURL: publicBaseURL,
	}
}

type CreateIsnRequest struct {
	Title string `json:"title" example:"Sample ISN @example.org"`
	UpdateIsnRequest
}

type UpdateIsnRequest struct {
	Detail     *string `json:"detail" example:"Sample ISN description"`
	IsInUse    *bool   `json:"is_in_use" example:"true"`
	Visibility *string `json:"visibility" example:"private" enums:"public,private"`
}

type TransferIsnOwnershipRequest struct {
	NewOwnerAccountID string `json:"new_owner_account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
}

type CreateIsnResponse struct {
	ID          uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	Slug        string    `json:"slug" example:"sample-isn"`
	ResourceURL string    `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn"`
}

// Isn is the ISN detail returned by the GET ISN endpoints
type Isn struct {
	ID        uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	CreatedAt time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`

	// OwnerAccountID is the account ID of the admin that owns the ISN
	OwnerAccountID uuid.UUID `json:"owner_account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`

	Title string `json:"title" example:"Sample ISN @example.org"`

	// Slug is the URL-friendly identifier generated from the title
	Slug string `json:"slug" example:"sample-isn"`

	Detail string `json:"detail" example:"Sample ISN description"`

	// IsInUse is false if the ISN has been disabled
	IsInUse bool `json:"is_in_use" example:"true"`

	// Visibility is private (signals visible to ISN members only) or public (signals visible to anyone)
	Visibility string `json:"visibility" example:"private" enums:"public,private"`
}

// SignalType is the detail of a signal type registered on the ISN
type SignalType struct {
	ID        uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	CreatedAt time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`
	Slug      string    `json:"slug" example:"sample-signal-type"`

	// SchemaURL is empty if signals are not validated against a schema
	SchemaURL string `json:"schema_url" example:"https://example.com/schema.json"`

	// ReadmeURL is empty if no readme was supplied
	ReadmeURL string `json:"readme_url" example:"https://example.com/readme.md"`

	Title       string `json:"title" example:"Sample Signal Type"`
	Detail      string `json:"detail" example:"Sample signal type description"`
	SemVer      string `json:"sem_ver" example:"1.0.0"`
	ContentKind string `json:"content_kind" example:"json" enums:"json,document,event"`

	// IsInUse shows whether the signal type is enabled on this ISN
	IsInUse bool `json:"is_in_use" example:"true"`
}

// IsnAndLinkedInfo is the response for the GET ISN endpoint
type IsnAndLinkedInfo struct {
	Isn Isn `json:"isn"`

	// SignalTypes lists the signal types registered on the ISN (omitted if there are none)
	SignalTypes *[]SignalType `json:"signal_types,omitempty"`
}

// CreateIsn godoc
//
//	@Summary		Create an ISN
//	@Description	Create an Information Sharing Network (ISN)
//	@Description
//	@Description	*Visibility*
//	@Description	Signals in a private ISN can only be seen by members of the ISN.
//	@Description	Signals in a public ISN can be viewed by anyone (no authentication required).
//	@Description	Accounts need write permission for the ISN before they can submit signals
//	@Description
//	@Description	Once the ISN is created you can:
//	@Description	- Grant accounts permission to use it
//	@Description	- Add the Signal Types that can be shared over the network
//	@Description
//	@Description	There can be multiple ISNs on the site and each ISN can have a
//	@Description	different membership and be configured for different Signal Types.
//	@Description
//	@Description	This endpoint can only be used by ISN admins and site admins
//	@Description
//	@Description	All fields (title, detail, is_in_use and visibility) are required.
//
//	@Tags			ISN Configuration
//
//	@Param			request	body		handlers.CreateIsnRequest	true	"ISN configuration"
//
//	@Success		201		{object}	handlers.CreateIsnResponse
//	@Failure		400		{object}	responses.ErrorResponse	"malformed_body"
//	@Failure		401		{object}	responses.ErrorResponse	"authorization_error | access_token_expired"
//	@Failure		403		{object}	responses.ErrorResponse	"forbidden"
//	@Failure		409		{object}	responses.ErrorResponse	"resource_already_exists"
//	@Failure		500		{object}	responses.ErrorResponse	"database_error | internal_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/ [post]
//
// Use with RequireRole (isnadmin,siteadmin)
func (i *IsnHandler) CreateIsn(w http.ResponseWriter, r *http.Request) error {
	var req CreateIsnRequest

	var slug string

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		return apperrors.InternalError("did not receive userAccountID from middleware", nil)
	}

	defer r.Body.Close()

	if err := decodeJSONBody(r, &req); err != nil {
		return err
	}

	// validate fields
	if req.Title == "" ||
		req.Detail == nil ||
		req.IsInUse == nil ||
		req.Visibility == nil {
		return apperrors.MalformedBody("you have not supplied all the required fields in the payload", nil)
	}

	// generate slug and check it is not already in use
	slug, err := utils.GenerateSlug(req.Title)
	if err != nil {
		return apperrors.InternalError("could not create slug from title", nil)
	}

	tx, err := i.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		return apperrors.DatabaseError("database error", err)
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.AddLogAttrs(r.Context(),
				slog.String("rollback_error", err.Error()),
			)

		}
	}()

	txQueries := i.queries.WithTx(tx)

	exists, err := txQueries.ExistsIsnWithSlug(r.Context(), slug)
	if err != nil {
		return apperrors.InternalError("database error", nil)
	}
	if exists {
		return apperrors.AlreadyExists(fmt.Sprintf("the {%s} slug is already in use - pick a new title for your ISN", slug), nil)
	}

	if !signalsd.ValidVisibilities[*req.Visibility] {
		return apperrors.MalformedBody(fmt.Sprintf("invalid visibility value: %s", *req.Visibility), nil)
	}

	// create isn
	returnedIsn, err := txQueries.CreateIsn(r.Context(), database.CreateIsnParams{
		UserAccountID: userAccountID,
		Title:         req.Title,
		Slug:          slug,
		Detail:        *req.Detail,
		IsInUse:       *req.IsInUse,
		Visibility:    *req.Visibility,
	})
	if err != nil {
		logger.AddLogAttrs(r.Context(),
			slog.String("slug", slug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	if err := tx.Commit(r.Context()); err != nil {
		return apperrors.DatabaseError("database error", err)
	}

	resourceURL := fmt.Sprintf("%s/api/isn/%s",
		i.publicBaseURL,
		slug,
	)

	return responses.JSON(w, http.StatusCreated, CreateIsnResponse{
		ID:          returnedIsn.ID,
		Slug:        returnedIsn.Slug,
		ResourceURL: resourceURL,
	})
}

// UpdateIsn godoc
//
//	@Summary		Update an ISN
//	@Description	Update the ISN configuration
//	@Description	This endpoint can only be used by admin accounts
//	@Description	ISN admins can only update ISNs they own
//	@Description	Site admins can update any ISN
//
//	@Tags			ISN Configuration
//
//	@Param			isn_slug	path	string						true	"ISN slug"	example(sample-isn)
//	@Param			request		body	handlers.UpdateIsnRequest	true	"ISN configuration"
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse	"malformed_body"
//	@Failure		401	{object}	responses.ErrorResponse	"authorization_error | access_token_expired"
//	@Failure		403	{object}	responses.ErrorResponse	"forbidden"
//	@Failure		404	{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		500	{object}	responses.ErrorResponse	"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug} [put]
//
// Use with RequireRole (isnadmin,siteadmin) middleware
func (i *IsnHandler) UpdateIsn(w http.ResponseWriter, r *http.Request) error {
	var req UpdateIsnRequest

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		return apperrors.InternalError("did not receive userAccountID from middleware", nil)
	}

	isnSlug := r.PathValue("isn_slug")

	// get the isn
	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound("ISN not found", nil)
		}
		logger.AddLogAttrs(r.Context(),
			slog.String("isn_slug", isnSlug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	// If the requester is an ISN admin, they must own this ISN.
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		return apperrors.InternalError("could not get claims from context", nil)
	}

	if claims.Role == "isnadmin" && isn.UserAccountID != userAccountID {
		return apperrors.Forbidden("you must be the ISN owner to update this ISN", nil)
	}

	defer r.Body.Close()
	if err := decodeJSONBodyStrict(r, &req); err != nil {
		return err
	}

	// set up values for update
	if req.Detail != nil {
		isn.Detail = *req.Detail
	}
	if req.IsInUse != nil {
		isn.IsInUse = *req.IsInUse
	}
	if req.Visibility != nil {
		if !signalsd.ValidVisibilities[*req.Visibility] {
			return apperrors.MalformedBody(fmt.Sprintf("invalid visibility value: %s", *req.Visibility), nil)
		}
		isn.Visibility = *req.Visibility
	}

	_, err = i.queries.UpdateIsn(r.Context(), database.UpdateIsnParams{
		ID:         isn.ID,
		Detail:     isn.Detail,
		IsInUse:    isn.IsInUse,
		Visibility: isn.Visibility,
	})
	if err != nil {
		logger.AddLogAttrs(r.Context(),
			slog.String("isn_slug", isnSlug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	return responses.NoContent(w, http.StatusNoContent)
}

// GetIsns godoc
//
//	@Summary		Get ISN configurations
//	@Description	get a list of the configured ISNs (any authenticated account can use this endpoint - private ISNs are included)
//	@Param			include_inactive	query	bool	false	"Include inactive ISNs"	default(false)
//	@Tags			ISN Configuration
//
//	@Success		200	{array}		handlers.Isn
//	@Failure		401	{object}	responses.ErrorResponse	"authorization_error | access_token_expired"
//	@Failure		500	{object}	responses.ErrorResponse	"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn [get]
func (s *IsnHandler) GetIsns(w http.ResponseWriter, r *http.Request) error {

	includeInactive := r.URL.Query().Get("include_inactive") == "true"

	var dbIsns []database.Isn
	var err error

	if includeInactive {
		dbIsns, err = s.queries.GetIsns(r.Context())
	} else {
		dbIsns, err = s.queries.GetInUseIsns(r.Context())
	}

	if err != nil {
		return apperrors.DatabaseError("database error", err)
	}

	isns := make([]Isn, len(dbIsns))
	for i, dbIsn := range dbIsns {
		isns[i] = Isn{
			ID:             dbIsn.ID,
			CreatedAt:      dbIsn.CreatedAt,
			UpdatedAt:      dbIsn.UpdatedAt,
			OwnerAccountID: dbIsn.UserAccountID,
			Title:          dbIsn.Title,
			Slug:           dbIsn.Slug,
			Detail:         dbIsn.Detail,
			IsInUse:        dbIsn.IsInUse,
			Visibility:     dbIsn.Visibility,
		}
	}

	return responses.JSON(w, http.StatusOK, isns)
}

// GetIsn godoc
//
//	@Summary		Get an ISN configuration
//	@Description	Returns details about the ISN (any authenticated account can use this endpoint)
//	@Param			isn_slug	path	string	true	"ISN slug"	example(sample-isn)
//
//	@Tags			ISN Configuration
//
//	@Success		200	{object}	handlers.IsnAndLinkedInfo
//	@Failure		401	{object}	responses.ErrorResponse	"authorization_error | access_token_expired"
//	@Failure		404	{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		500	{object}	responses.ErrorResponse	"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug} [get]
func (s *IsnHandler) GetIsn(w http.ResponseWriter, r *http.Request) error {

	slug := r.PathValue("isn_slug")

	// check isn exists
	dbIsn, err := s.queries.GetIsnBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound(fmt.Sprintf("No isn found for %s", slug), nil)
		}
		logger.AddLogAttrs(r.Context(),
			slog.String("isn_slug", slug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	// Convert database structs to our response structs
	isn := Isn{
		ID:             dbIsn.ID,
		CreatedAt:      dbIsn.CreatedAt,
		UpdatedAt:      dbIsn.UpdatedAt,
		OwnerAccountID: dbIsn.UserAccountID,
		Title:          dbIsn.Title,
		Slug:           dbIsn.Slug,
		Detail:         dbIsn.Detail,
		IsInUse:        dbIsn.IsInUse,
		Visibility:     dbIsn.Visibility,
	}

	var signalTypes *[]SignalType
	dbSignalTypes, err := s.queries.GetSignalTypesByIsnID(r.Context(), dbIsn.ID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logger.AddLogAttrs(r.Context(),
				slog.String("isn_id", dbIsn.ID.String()),
			)

			return apperrors.DatabaseError("database error", err)
		}
		// If no rows found, signalTypes remains nil
	} else {
		convertedSignalTypes := make([]SignalType, len(dbSignalTypes))
		for i, st := range dbSignalTypes {
			// the placeholder URLs used when there is no schema or readme are not shown
			schemaURL := st.SchemaURL
			if schemaURL == signalsd.SkipValidationURL {
				schemaURL = ""
			}
			readmeURL := st.ReadmeURL
			if readmeURL == signalsd.SkipReadmeURL {
				readmeURL = ""
			}

			convertedSignalTypes[i] = SignalType{
				ID:          st.ID,
				CreatedAt:   st.CreatedAt,
				UpdatedAt:   st.UpdatedAt,
				Slug:        st.Slug,
				SchemaURL:   schemaURL,
				ReadmeURL:   readmeURL,
				Title:       st.Title,
				Detail:      st.Detail,
				SemVer:      st.SemVer,
				ContentKind: st.ContentKind,
				IsInUse:     st.IsInUse,
			}
		}
		signalTypes = &convertedSignalTypes
	}

	// Send response
	res := IsnAndLinkedInfo{
		Isn:         isn,
		SignalTypes: signalTypes,
	}
	return responses.JSON(w, http.StatusOK, res)
}

// TransferIsnOwnership godoc
//
//	@Summary		Transfer ISN Ownership
//	@Description	Transfer ownership of an ISN to another admin account.
//	@Description	This can be used when an admin leaves or when reorganizing responsibilities.
//	@Description	Only site admins can transfer ISN ownership.
//	@Tags			ISN Configuration
//
//	@Param			isn_slug	path	string									true	"ISN slug"	example(sample-isn)
//	@Param			request		body	handlers.TransferIsnOwnershipRequest	true	"Transfer details"
//
//	@Success		200
//	@Failure		400	{object}	responses.ErrorResponse	"malformed_body | invalid_request"
//	@Failure		401	{object}	responses.ErrorResponse	"authorization_error | access_token_expired"
//	@Failure		403	{object}	responses.ErrorResponse	"forbidden"
//	@Failure		404	{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		500	{object}	responses.ErrorResponse	"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/isn/{isn_slug}/transfer-ownership [put]
//
// Use with RequireRole (siteadmin)
func (i *IsnHandler) TransferIsnOwnership(w http.ResponseWriter, r *http.Request) error {
	var req TransferIsnOwnershipRequest

	isnSlug := r.PathValue("isn_slug")

	// check ISN exists
	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound("ISN not found", nil)
		}
		logger.AddLogAttrs(r.Context(),
			slog.String("isn_slug", isnSlug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	defer r.Body.Close()
	if err := decodeJSONBody(r, &req); err != nil {
		return err
	}

	// validate new account ID
	newOwnerAccountID, err := uuid.Parse(req.NewOwnerAccountID)
	if err != nil {
		logger.AddLogAttrs(r.Context(),
			slog.String("new_owner_account_id", req.NewOwnerAccountID),
		)

		return apperrors.MalformedBody("invalid new owner account ID", err)
	}

	// check if new owner account exists and is an admin
	newOwnerAccount, err := i.queries.GetAccountByID(r.Context(), newOwnerAccountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.InvalidRequest("new owner account not found", nil)
		}
		logger.AddLogAttrs(r.Context(),
			slog.String("new_owner_account_id", newOwnerAccountID.String()),
		)

		return apperrors.DatabaseError("database error", err)
	}

	// verify new owner is an isn admin or owner
	if newOwnerAccount.AccountType != "user" {
		return apperrors.InvalidRequest("new owner must be a user account", nil)
	}

	if newOwnerAccount.AccountRole != "isnadmin" && newOwnerAccount.AccountRole != "siteadmin" {
		return apperrors.InvalidRequest("new owner must be an admin", nil)
	}

	// check if account is active
	if !newOwnerAccount.IsActive {
		return apperrors.InvalidRequest("new owner account is not active", nil)
	}

	// prevent transferring to the same account
	if isn.UserAccountID == newOwnerAccountID {
		return apperrors.InvalidRequest("ISN is already owned by this account", nil)
	}

	// update ISN ownership
	rowsAffected, err := i.queries.UpdateIsnOwner(r.Context(), database.UpdateIsnOwnerParams{
		ID:            isn.ID,
		UserAccountID: newOwnerAccountID,
	})
	if err != nil {
		logger.AddLogAttrs(r.Context(),
			slog.String("isn_id", isn.ID.String()),
		)

		return apperrors.DatabaseError("database error", err)
	}

	if rowsAffected == 0 {
		return apperrors.DatabaseError("no rows updated during ownership transfer", nil)
	}

	logger.AddLogAttrs(r.Context(),
		slog.String("isn_slug", isn.Slug),
		slog.String("from_user_id", isn.UserAccountID.String()),
		slog.String("to_user_id", newOwnerAccountID.String()),
	)

	return responses.NoContent(w, http.StatusOK)
}
