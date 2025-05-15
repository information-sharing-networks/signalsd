package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/apperrors"
	"github.com/nickabs/signals/internal/context"
	"github.com/nickabs/signals/internal/database"
	"github.com/nickabs/signals/internal/helpers"
)

type IsnHandler struct {
	cfg *signals.ServiceConfig
}

func NewIsnHandler(cfg *signals.ServiceConfig) *IsnHandler {
	return &IsnHandler{cfg: cfg}
}

type CreateIsnRequest struct {
	Title string `json:"title" example:"Sample ISN @example.org"`
	UpdateIsnRequest
}

type UpdateIsnRequest struct {
	Detail      *string `json:"detail" example:"Sample ISN description"`
	IsInUse     *bool   `json:"is_in_use" example:"true"`
	Visibility  *string `json:"visibility" example:"private" enums:"public,private"`
	StorageType *string `json:"storage_type" example:"mq"`
}

type CreateIsnResponse struct {
	ID          uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	Slug        string    `json:"slug" example:"sample-ISN--example-org"`
	ResourceURL string    `json:"resource_url" example:"http://localhost:8080/api/isn/sample-ISN--example-org"`
}

// CreateIsnHandler godoc
//
//	@Summary		Create an ISN
//	@Description	Create an Information Sharing Network (ISN)
//	@Description
//	@Description	visibility = "private" means that signals on the network can only be seen by network participants.
//	@Description
//	@Description	The only storage_type currently supported is "local"
//	@Description	when storage_type = "local" the signals are stored in the relational database used by the API service to store the admin configuration
//
//	@Tags			ISN config
//
//	@Param			request	body		handlers.CreateIsnRequest	true	"ISN details"
//
//	@Success		201		{object}	handlers.CreateIsnResponse
//	@Failure		400		{object}	apperrors.ErrorResponse
//	@Failure		409		{object}	apperrors.ErrorResponse
//	@Failure		500		{object}	apperrors.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug} [post]
func (i *IsnHandler) CreateIsnHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateIsnRequest

	var slug string

	userID, ok := context.UserID(r.Context())
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userID from middleware")
		return
	}
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// validate fields
	if req.Title == "" ||
		req.Detail == nil ||
		req.IsInUse == nil ||
		req.Visibility == nil ||
		req.StorageType == nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you have not supplied all the required fields in the payload")
		return
	}

	// generate slug and check it is not already in use
	slug, err := helpers.GenerateSlug(req.Title)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not create slug from title")
		return
	}
	exists, err := i.cfg.DB.ExistsIsnWithSlug(r.Context(), slug)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "database error")
		return
	}
	if exists {
		helpers.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("the {%s} slug is already in use - pick a new title for your ISN", slug))
		return
	}

	if !signals.ValidVisibilities[*req.Visibility] {
		helpers.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid visiblity value: %s", *req.Visibility))
		return
	}

	// create isn
	returnedIsn, err := i.cfg.DB.CreateIsn(r.Context(), database.CreateIsnParams{
		UserID:      userID,
		Title:       req.Title,
		Slug:        slug,
		Detail:      *req.Detail,
		IsInUse:     *req.IsInUse,
		Visibility:  *req.Visibility,
		StorageType: *req.StorageType,
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/isn/%s",
		helpers.GetScheme(r),
		r.Host,
		slug,
	)

	helpers.RespondWithJSON(w, http.StatusCreated, CreateIsnResponse{
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
//	@Param			isn_slug	path	string								true	"isn slug"	example(sample-ISN--example-org)
//	@Param			request				body	handlers.UpdateIsnRequest	true	"ISN details"
//
//	@Success		204
//	@Failure		400	{object}	apperrors.ErrorResponse
//	@Failure		401	{object}	apperrors.ErrorResponse
//	@Failure		500	{object}	apperrors.ErrorResponse
//
// //
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug} [put]
func (i *IsnHandler) UpdateIsnHandler(w http.ResponseWriter, r *http.Request) {
	var req UpdateIsnRequest

	userID, ok := context.UserID(r.Context())
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userID from middleware")
		return
	}

	isnSlug := r.PathValue("isn_slug")

	// check ISN exists and is owned by user
	isn, err := i.cfg.DB.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	if isn.UserID != userID {
		helpers.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
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

	// update isn_receiever
	_, err = i.cfg.DB.UpdateIsn(r.Context(), database.UpdateIsnParams{
		ID:          isn.ID,
		Detail:      isn.Detail,
		IsInUse:     isn.IsInUse,
		Visibility:  isn.Visibility,
		StorageType: isn.StorageType,
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN: %v", err))
		return
	}

	helpers.RespondWithJSON(w, http.StatusNoContent, "")
}

// GetIsnsHandler godoc
//
//	@Summary	Get the ISNs
//	@Description		get a list of the configured ISNs
//	@Tags	ISN view
//
//	@Success	200	{array}		database.Isn
//	@Failure	500	{object}	apperrors.ErrorResponse
//
//	@Router		/api/isn [get]
func (s *IsnHandler) GetIsnsHandler(w http.ResponseWriter, r *http.Request) {

	res, err := s.cfg.DB.GetIsns(r.Context())
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error getting ISNs from database: %v", err))
		return
	}
	helpers.RespondWithJSON(w, http.StatusOK, res)

}
