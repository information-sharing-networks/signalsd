package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	signalsd "github.com/information-sharing-networks/signalsd/app"
)

type IsnHandler struct {
	queries *database.Queries
	pool    *pgxpool.Pool
}

func NewIsnHandler(queries *database.Queries, pool *pgxpool.Pool) *IsnHandler {
	return &IsnHandler{
		queries: queries,
		pool:    pool,
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
	NewAdminAccountID string `json:"new_owner_account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
}

type CreateIsnResponse struct {
	ID          uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	Slug        string    `json:"slug" example:"sample-isn--example-org"`
	ResourceURL string    `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn--example-org"`
}

// Response structs for GET handlers
type Isn struct {
	ID         uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	CreatedAt  time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt  time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`
	Title      string    `json:"title" example:"Sample ISN @example.org"`
	Slug       string    `json:"slug" example:"sample-isn--example-org"`
	Detail     string    `json:"detail" example:"Sample ISN description"`
	IsInUse    bool      `json:"is_in_use" example:"true"`
	Visibility string    `json:"visibility" example:"private" enums:"public,private"`
}

type User struct {
	AccountID uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	CreatedAt time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`
}

type SignalType struct {
	ID        uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	CreatedAt time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`
	Slug      string    `json:"slug" example:"sample-signal-type"`
	SchemaURL string    `json:"schema_url" example:"https://example.com/schema.json"`
	ReadmeURL string    `json:"readme_url" example:"https://example.com/readme.md"`
	Title     string    `json:"title" example:"Sample Signal Type"`
	Detail    string    `json:"detail" example:"Sample signal type description"`
	SemVer    string    `json:"sem_ver" example:"1.0.0"`
	IsInUse   bool      `json:"is_in_use" example:"true"`
}

type IsnAndLinkedInfo struct {
	Isn         Isn           `json:"isn"`
	User        User          `json:"user"`
	SignalTypes *[]SignalType `json:"signal_types,omitempty"`
}

// CreateIsnHandler godoc
//
//	@Summary		Create an ISN
//	@Description	Create an Information Sharing Network (ISN)
//	@Description
//	@Description	visibility = "private" means that signalsd on the network can only be seen by network participants.
//	@Description
//	@Description	ISN admins automatically get write permission for their own ISNs.
//	@Description	Site owners automatically get write permission on all ISNs.
//	@Description
//	@Description	This endpoint can only be used by the site owner or an admin
//	@Description
//	@Description	Note there is a cache of public ISNs that is used by the search endpoints. This cache is not dynamically loaded, so adding public ISNs requires a restart of the service
//
//	@Tags			ISN configuration
//
//	@Param			request	body		handlers.CreateIsnRequest	true	"ISN details"
//
//	@Success		201		{object}	handlers.CreateIsnResponse
//	@Failure		400		{object}	responses.ErrorResponse
//	@Failure		409		{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//	@Security		RefreshTokenCookieAuth
//
//	@Router			/api/isn/ [post]
//
// Use with RequireRole (admin,owner)
func (i *IsnHandler) CreateIsnHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateIsnRequest

	var slug string

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// validate fields
	if req.Title == "" ||
		req.Detail == nil ||
		req.IsInUse == nil ||
		req.Visibility == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you have not supplied all the required fields in the payload")
		return
	}

	// generate slug and check it is not already in use
	slug, err := utils.GenerateSlug(req.Title)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not create slug from title")
		return
	}

	tx, err := i.pool.BeginTx(r.Context(), pgx.TxOptions{})
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

	txQueries := i.queries.WithTx(tx)

	exists, err := txQueries.ExistsIsnWithSlug(r.Context(), slug)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "database error")
		return
	}
	if exists {
		responses.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("the {%s} slug is already in use - pick a new title for your ISN", slug))
		return
	}

	if !signalsd.ValidVisibilities[*req.Visibility] {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid visiblity value: %s", *req.Visibility))
		return
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
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN: %v", err))
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to commit transaction: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/isn/%s",
		utils.GetScheme(r),
		r.Host,
		slug,
	)

	responses.RespondWithJSON(w, http.StatusCreated, CreateIsnResponse{
		ID:          returnedIsn.ID,
		Slug:        returnedIsn.Slug,
		ResourceURL: resourceURL,
	})
}

// UpdateIsnHandler godoc
//
//	@Summary		Update an ISN
//	@Description	Update the ISN details
//	@Description	This endpoint can only be used by the site owner or the ISN admin
//
//	@Tags			ISN configuration
//
//	@Param			isn_slug	path	string						true	"isn slug"	example(sample-isn--example-org)
//	@Param			request		body	handlers.UpdateIsnRequest	true	"ISN details"
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug} [put]
//
// Use with RequireRole (admin,owner)
func (i *IsnHandler) UpdateIsnHandler(w http.ResponseWriter, r *http.Request) {
	var req UpdateIsnRequest

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	isnSlug := r.PathValue("isn_slug")

	// check ISN exists and is owned by user
	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	// check if user is either the ISN owner or a site owner
	claims, ok := auth.ContextAccessTokenClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	isIsnOwner := isn.UserAccountID == userAccountID
	isSiteOwner := claims.Role == "owner"

	if !isIsnOwner && !isSiteOwner {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you must be either the ISN owner or a site owner to update this ISN")
		return
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// set up values for update
	if req.Detail != nil {
		isn.Detail = *req.Detail
	}
	if req.IsInUse != nil {
		isn.IsInUse = *req.IsInUse
	}
	if req.Visibility != nil {
		isn.Visibility = *req.Visibility
	}

	_, err = i.queries.UpdateIsn(r.Context(), database.UpdateIsnParams{
		ID:         isn.ID,
		Detail:     isn.Detail,
		IsInUse:    isn.IsInUse,
		Visibility: isn.Visibility,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN: %v", err))
		return
	}

	responses.RespondWithStatusCodeOnly(w, http.StatusCreated)
}

// GetIsnsHandler godoc
//
//	@Summary		Get the ISNs
//	@Description	get a list of the configured ISNs
//	@Tags			ISN details
//
//	@Success		200	{array}	handlers.Isn
//
//	@Router			/api/isn [get]
func (s *IsnHandler) GetIsnsHandler(w http.ResponseWriter, r *http.Request) {

	dbIsns, err := s.queries.GetIsns(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error getting ISNs from database: %v", err))
		return
	}

	isns := make([]Isn, len(dbIsns))
	for i, dbIsn := range dbIsns {
		isns[i] = Isn{
			ID:         dbIsn.ID,
			CreatedAt:  dbIsn.CreatedAt,
			UpdatedAt:  dbIsn.UpdatedAt,
			Title:      dbIsn.Title,
			Slug:       dbIsn.Slug,
			Detail:     dbIsn.Detail,
			IsInUse:    dbIsn.IsInUse,
			Visibility: dbIsn.Visibility,
		}
	}

	responses.RespondWithJSON(w, http.StatusOK, isns)
}

// GetIsnHandler godoc
//
//	@Summary		Get an ISN configuration
//	@Description	Returns details about the ISN
//	@Param			isn_slug	path	string	true	"isn slug"	example(sample-isn--example-org)
//
//	@Tags			ISN details
//
//	@Success		200	{object}	handlers.IsnAndLinkedInfo
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Router			/api/isn/{isn_slug} [get]
func (s *IsnHandler) GetIsnHandler(w http.ResponseWriter, r *http.Request) {

	slug := r.PathValue("isn_slug")

	// check isn exists
	dbIsn, err := s.queries.GetIsnBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No isn found for %s", slug))
			return
		}
	}

	// get the owner of the isn
	dbUser, err := s.queries.GetUserByIsnID(r.Context(), dbIsn.ID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user for this isn: %v", err))
		return
	}

	// Convert database structs to our response structs
	isn := Isn{
		ID:         dbIsn.ID,
		CreatedAt:  dbIsn.CreatedAt,
		UpdatedAt:  dbIsn.UpdatedAt,
		Title:      dbIsn.Title,
		Slug:       dbIsn.Slug,
		Detail:     dbIsn.Detail,
		IsInUse:    dbIsn.IsInUse,
		Visibility: dbIsn.Visibility,
	}

	user := User{
		AccountID: dbUser.AccountID,
		CreatedAt: dbUser.CreatedAt,
		UpdatedAt: dbUser.UpdatedAt,
	}

	var signalTypes *[]SignalType
	dbSignalTypes, err := s.queries.GetSignalTypeByIsnID(r.Context(), dbIsn.ID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
			return
		}
		// If no rows found, signalTypes remains nil
	} else {
		convertedSignalTypes := make([]SignalType, len(dbSignalTypes))
		for i, st := range dbSignalTypes {
			convertedSignalTypes[i] = SignalType{
				ID:        st.ID,
				CreatedAt: st.CreatedAt,
				UpdatedAt: st.UpdatedAt,
				Slug:      st.Slug,
				SchemaURL: st.SchemaURL,
				ReadmeURL: st.ReadmeURL,
				Title:     st.Title,
				Detail:    st.Detail,
				SemVer:    st.SemVer,
				IsInUse:   st.IsInUse,
			}
		}
		signalTypes = &convertedSignalTypes
	}

	// Send response
	res := IsnAndLinkedInfo{
		Isn:         isn,
		User:        user,
		SignalTypes: signalTypes,
	}
	responses.RespondWithJSON(w, http.StatusOK, res)
}

// TransferIsnOwnershipHandler godoc
//
//	@Summary		Transfer ISN ownership
//	@Description	Transfer ownership of an ISN to another admin account.
//	@Description	This can be used when an admin leaves or when reorganizing responsibilities.
//	@Description	Only the site owner can transfer ISN ownership.
//	@Tags			ISN configuration
//
//	@Param			isn_slug	path	string									true	"ISN slug"
//	@Param			request		body	handlers.TransferIsnOwnershipRequest	true	"Transfer details"
//
//	@Success		200
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//	@Security		RefreshTokenCookieAuth
//
//	@Router			/api/isn/{isn_slug}/transfer-ownership [put]
//
// Use with RequireRole (owner)
func (i *IsnHandler) TransferIsnOwnershipHandler(w http.ResponseWriter, r *http.Request) {
	var req TransferIsnOwnershipRequest
	logger := zerolog.Ctx(r.Context())

	isnSlug := r.PathValue("isn_slug")

	// check ISN exists
	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// validate new account ID
	newAdminAccountID, err := uuid.Parse(req.NewAdminAccountID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid new_owner_account_id: %v", err))
		return
	}

	// check if new owner account exists and is an admin
	newAdminAccount, err := i.queries.GetAccountByID(r.Context(), newAdminAccountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "new owner account not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not get new owner account: %v", err))
		return
	}

	// verify new owner is an admin or owner
	if newAdminAccount.AccountType != "user" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "new owner must be a user account")
		return
	}

	if newAdminAccount.AccountRole != "admin" && newAdminAccount.AccountRole != "owner" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "new owner must be an admin or owner")
		return
	}

	// check if account is active
	if !newAdminAccount.IsActive {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "new owner account is not active")
		return
	}

	// prevent transferring to the same account
	if isn.UserAccountID == newAdminAccountID {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "ISN is already owned by this account")
		return
	}

	// update ISN ownership
	rowsAffected, err := i.queries.UpdateIsnOwner(r.Context(), database.UpdateIsnOwnerParams{
		ID:            isn.ID,
		UserAccountID: newAdminAccountID,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to update ISN ownership: %v", err))
		return
	}

	if rowsAffected == 0 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "no rows updated during ownership transfer")
		return
	}

	logger.Info().Msgf("ISN %v ownership transferred from %v to %v ", isn.Slug, isn.UserAccountID, newAdminAccountID)
	responses.RespondWithStatusCodeOnly(w, http.StatusOK)
}
