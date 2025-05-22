package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/context"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/helpers"
	"github.com/nickabs/signalsd/app/internal/response"

	signalsd "github.com/nickabs/signalsd/app"
)

type IsnHandler struct {
	queries *database.Queries
}

func NewIsnHandler(queries *database.Queries) *IsnHandler {
	return &IsnHandler{queries: queries}
}

type CreateIsnRequest struct {
	Title string `json:"title" example:"Sample ISN @example.org"`
	UpdateIsnRequest
}

type UpdateIsnRequest struct {
	Detail               *string `json:"detail" example:"Sample ISN description"`
	IsInUse              *bool   `json:"is_in_use" example:"true"`
	Visibility           *string `json:"visibility" example:"private" enums:"public,private"`
	StorageType          *string `json:"storage_type" example:"mq"`
	StorageConnectionURL *string `json:"storage_connection_url" example:"postgres:/signalsd:@localhost:5432/signals?sslmode=disable"`
}

type CreateIsnResponse struct {
	ID          uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	Slug        string    `json:"slug" example:"sample-isn--example-org"`
	ResourceURL string    `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn--example-org"`
}

// used in GET handler
type IsnAndLinkedInfo struct {
	database.GetForDisplayIsnBySlugRow
	User         database.GetForDisplayUserByIsnIDRow          `json:"user"`
	IsnReceiver  *database.GetForDisplayIsnReceiverByIsnIDRow  `json:"isn_receiver,omitempty"`
	IsnRetriever *database.GetForDisplayIsnRetrieverByIsnIDRow `json:"isn_rectriever,omitempty"`
}

// CreateIsnHandler godoc
//
//	@Summary		Create an ISN
//	@Description	Create an Information Sharing Network (ISN)
//	@Description
//	@Description	visibility = "private" means that signalsd on the network can only be seen by network participants.
//	@Description
//	@Description	The only storage_type currently supported is "admin_db"
//	@Description	when storage_type = "admin_db" the signalsd are stored in the relational database used by the API service to store the admin configuration
//	@Description	Specify "admin_db" for storage_connection_url in this case (anything else is overriwtten with this value)
//
//	@Tags			ISN config
//
//	@Param			request	body		handlers.CreateIsnRequest	true	"ISN details"
//
//	@Success		201		{object}	handlers.CreateIsnResponse
//	@Failure		400		{object}	response.ErrorResponse
//	@Failure		409		{object}	response.ErrorResponse
//	@Failure		500		{object}	response.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug} [post]
func (i *IsnHandler) CreateIsnHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateIsnRequest

	var slug string

	userAccountID, ok := context.UserAccountID(r.Context())
	if !ok {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// validate fields
	if req.Title == "" ||
		req.Detail == nil ||
		req.IsInUse == nil ||
		req.Visibility == nil ||
		req.StorageType == nil ||
		req.StorageConnectionURL == nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you have not supplied all the required fields in the payload")
		return
	}

	// generate slug and check it is not already in use
	slug, err := helpers.GenerateSlug(req.Title)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not create slug from title")
		return
	}
	exists, err := i.queries.ExistsIsnWithSlug(r.Context(), slug)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "database error")
		return
	}
	if exists {
		response.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("the {%s} slug is already in use - pick a new title for your ISN", slug))
		return
	}

	if !signalsd.ValidVisibilities[*req.Visibility] {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid visiblity value: %s", *req.Visibility))
		return
	}

	if *req.StorageType == "admin_db" {
		*req.StorageConnectionURL = "admin_db"
	}
	// create isn
	returnedIsn, err := i.queries.CreateIsn(r.Context(), database.CreateIsnParams{
		UserAccountID:        userAccountID,
		Title:                req.Title,
		Slug:                 slug,
		Detail:               *req.Detail,
		IsInUse:              *req.IsInUse,
		Visibility:           *req.Visibility,
		StorageType:          *req.StorageType,
		StorageConnectionURL: *req.StorageConnectionURL,
	})
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/isn/%s",
		helpers.GetScheme(r),
		r.Host,
		slug,
	)

	response.RespondWithJSON(w, http.StatusCreated, CreateIsnResponse{
		ID:          returnedIsn.ID,
		Slug:        returnedIsn.Slug,
		ResourceURL: resourceURL,
	})
}

// UpdateIsnHandler godoc
//
//	@Summary		Update an ISN
//	@Description	Update the ISN details
//
//	@Tags			ISN config
//
//	@Param			isn_slug	path	string						true	"isn slug"	example(sample-isn--example-org)
//	@Param			request		body	handlers.UpdateIsnRequest	true	"ISN details"
//
//	@Success		204
//	@Failure		400	{object}	response.ErrorResponse
//	@Failure		401	{object}	response.ErrorResponse
//	@Failure		500	{object}	response.ErrorResponse
//
// //
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug} [put]
func (i *IsnHandler) UpdateIsnHandler(w http.ResponseWriter, r *http.Request) {
	var req UpdateIsnRequest

	userAccountID, ok := context.UserAccountID(r.Context())
	if !ok {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	isnSlug := r.PathValue("isn_slug")

	// check ISN exists and is owned by user
	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	if isn.UserAccountID != userAccountID {
		response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
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
	if req.StorageType != nil {
		isn.StorageType = *req.StorageType
	}
	if req.StorageConnectionURL != nil {
		isn.StorageConnectionURL = *req.StorageConnectionURL
	}

	if isn.StorageType == "admin_db" {
		isn.StorageConnectionURL = "admin_db"
	}

	// update isn_receiver
	_, err = i.queries.UpdateIsn(r.Context(), database.UpdateIsnParams{
		ID:                   isn.ID,
		Detail:               isn.Detail,
		IsInUse:              isn.IsInUse,
		Visibility:           isn.Visibility,
		StorageType:          isn.StorageType,
		StorageConnectionURL: isn.StorageConnectionURL,
	})
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN: %v", err))
		return
	}

	response.RespondWithJSON(w, http.StatusNoContent, "")
}

// GetIsnsHandler godoc
//
//	@Summary		Get the ISNs
//	@Description	get a list of the configured ISNs
//	@Tags			ISN view
//
//	@Success		200	{array}		database.Isn
//	@Failure		500	{object}	response.ErrorResponse
//
//	@Router			/api/isn [get]
func (s *IsnHandler) GetIsnsHandler(w http.ResponseWriter, r *http.Request) {

	res, err := s.queries.GetIsns(r.Context())
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error getting ISNs from database: %v", err))
		return
	}
	response.RespondWithJSON(w, http.StatusOK, res)

}

// GetIsnHandler godoc
//
//	@Summary		Get an ISN configuration
//	@Description	Returns details about the ISN plus details of any configured receivers/retrievers
//	@Param			isn_slug	path	string	true	"isn slug"	example(sample-isn--example-org)
//
//	@Tags			ISN view
//
//	@Success		200	{object}	handlers.IsnAndLinkedInfo
//	@Failure		400	{object}	response.ErrorResponse
//	@Failure		404	{object}	response.ErrorResponse
//	@Failure		500	{object}	response.ErrorResponse
//
//	@Router			/api/isn/{slug} [get]
func (s *IsnHandler) GetIsnHandler(w http.ResponseWriter, r *http.Request) {

	slug := r.PathValue("isn_slug")

	// check isn exists
	isn, err := s.queries.GetForDisplayIsnBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No isn found for %s", slug))
			return
		}
	}

	// get the owner of the isn
	user, err := s.queries.GetForDisplayUserByIsnID(r.Context(), isn.ID)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user for this isn: %v", err))
		return
	}

	// get receiver and retriever if they were defined

	var isnRetceiverRes *database.GetForDisplayIsnReceiverByIsnIDRow
	isnReceiver, err := s.queries.GetForDisplayIsnReceiverByIsnID(r.Context(), isn.ID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the receiver for this isn: %v", err))
			return
		}
	} else {
		isnRetceiverRes = &isnReceiver
	}

	var isnRetrieverRes *database.GetForDisplayIsnRetrieverByIsnIDRow
	isnRetriever, err := s.queries.GetForDisplayIsnRetrieverByIsnID(r.Context(), isn.ID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the retriever for this isn: %v", err))
			return
		}
	} else {
		isnRetrieverRes = &isnRetriever
	}
	//send response
	res := IsnAndLinkedInfo{
		GetForDisplayIsnBySlugRow: isn,
		User:                      user,
		IsnReceiver:               isnRetceiverRes,
		IsnRetriever:              isnRetrieverRes,
	}
	response.RespondWithJSON(w, http.StatusOK, res)
}
