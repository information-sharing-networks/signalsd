package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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

type CreateIsnResponse struct {
	ID          uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	Slug        string    `json:"slug" example:"sample-isn--example-org"`
	ResourceURL string    `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn--example-org"`
}

// used in GET handler
type IsnAndLinkedInfo struct {
	database.GetForDisplayIsnBySlugRow
	User database.GetForDisplayUserByIsnIDRow `json:"user"`
}

// CreateIsnHandler godoc
//
//	@Summary		Create an ISN
//	@Description	Create an Information Sharing Network (ISN)
//	@Description
//	@Description	visibility = "private" means that signalsd on the network can only be seen by network participants.
//	@Description
//	@Description	ISN admins automatically get write permission for their own sites, so this endpoint also starts a signals batch for them.
//	@Description	Owners automatically get write permission on all isns, so a batch is started for them also.
//	@Description
//	@Description	This endpoint can only be used by the site owner or an admin
//
//	@Tags			ISN configuration
//
//	@Param			request	body		handlers.CreateIsnRequest	true	"ISN details"
//
//	@Success		201		{object}	handlers.CreateIsnResponse
//	@Failure		400		{object}	responses.ErrorResponse
//	@Failure		409		{object}	responses.ErrorResponse
//	@Failure		500		{object}	responses.ErrorResponse
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

	claims, ok := auth.ContextAccessTokenClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive claims from middleware")
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
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to being transaction: %v", err))
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
	// crete a signals batch for the user creating the admin
	_, err = txQueries.CreateSignalBatch(r.Context(), database.CreateSignalBatchParams{
		IsnID:       returnedIsn.ID,
		AccountID:   userAccountID,
		AccountType: "user", // only users can use the ISN configuration endpoints
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert signal_batch: %v", err))
		return
	}

	// if the isn was created by someone other than owner then create a batch for the owner so they can post to the ISN (owners have unlimited access)
	if claims.Role != "owner" {
		_, err = txQueries.CreateOwnerSignalBatch(r.Context(), database.CreateOwnerSignalBatchParams{
			IsnID:       returnedIsn.ID,
			AccountType: "user", // only users can use the ISN configuration endpoints
		})
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not insert signal_batch: %v", err))
			return
		}
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
//	@Failure		500	{object}	responses.ErrorResponse
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

	if isn.UserAccountID != userAccountID {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
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
//	@Tags			ISN view
//
//	@Success		200	{array}		database.Isn
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Router			/api/isn [get]
func (s *IsnHandler) GetIsnsHandler(w http.ResponseWriter, r *http.Request) {

	res, err := s.queries.GetIsns(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error getting ISNs from database: %v", err))
		return
	}
	responses.RespondWithJSON(w, http.StatusOK, res)

}

// GetIsnHandler godoc
//
//	@Summary		Get an ISN configurationuration
//	@Description	Returns details about the ISN
//	@Param			isn_slug	path	string	true	"isn slug"	example(sample-isn--example-org)
//
//	@Tags			ISN view
//
//	@Success		200	{object}	handlers.IsnAndLinkedInfo
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Router			/api/isn/{slug} [get]
func (s *IsnHandler) GetIsnHandler(w http.ResponseWriter, r *http.Request) {

	slug := r.PathValue("isn_slug")

	// check isn exists
	isn, err := s.queries.GetForDisplayIsnBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No isn found for %s", slug))
			return
		}
	}

	// get the owner of the isn
	user, err := s.queries.GetForDisplayUserByIsnID(r.Context(), isn.ID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user for this isn: %v", err))
		return
	}
	//send response
	res := IsnAndLinkedInfo{
		GetForDisplayIsnBySlugRow: isn,
		User:                      user,
	}
	responses.RespondWithJSON(w, http.StatusOK, res)
}
