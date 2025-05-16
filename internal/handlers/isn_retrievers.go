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
	"github.com/nickabs/signals/internal/response"
)

type IsnRetrieverHandler struct {
	cfg *signals.ServiceConfig
}

func NewIsnRetrieverHandler(cfg *signals.ServiceConfig) *IsnRetrieverHandler {
	return &IsnRetrieverHandler{cfg: cfg}
}

type CreateIsnRetrieverRequest struct {
	Title   string `json:"title" example:"Sample ISN Retriever @example.org"`
	IsnSlug string `json:"isn_slug" example:"sample-isn--example-org"`
	UpdateIsnRetrieverRequest
}

type CreateIsnRetrieverResponse struct {
	ID           uuid.UUID `json:"id" example:"4f1bc74b-cf79-410f-9c21-dc2cba047385"`
	Slug         string    `json:"slug" example:"sample-isn-retriever--example-org"`
	ResourceURL  string    `json:"resource_url" example:"http://localhost:8080/api/isn/retriever/sample-isn-retriever--example-org"`
	RetrieverURL string    `json:"retriever_url" example:"http://localhost:8080/signals/retriever/sample-isn-retriever--example-org"`
}

type UpdateIsnRetrieverRequest struct {
	Detail           *string `json:"detail" example:"sample-isn--example-org"`
	RetrieverOrigin  *string `json:"retriever_origin" example:"http://example.com:8080"` // do not provide this field if the isn is using local storage
	DefaultRateLimit *int32  `json:"default_rate_limit" example:"600"`                   //maximum number of requests per minute
	RetrieverStatus  *string `json:"retriever_status" example:"offline" enums:"offline, online, error, closed"`
}
type IsnRetrieverAndLinkedInfo struct {
	database.IsnReceiver `json:"isn_receiver"`
}

// CreateIsnRetrieverHandler godoc
//
//	@Summary		Create an ISN Retriever
//	@Description	the retriever service handles requests to get signals and will be hosted on {retriever_origin}/signals/retriever/{retriever_slug}
//	@Description
//	@Description	When the ISN storage_type is set to "local", the retriever_origin must also be "local", indicating that the signals are retieved from the relational database used by the API service.
//	@Description
//
//	@Tags		ISN config
//
//	@Param		request	body		handlers.CreateIsnRetrieverRequest	true	"ISN retriever details"
//
//	@Success	201		{object}	handlers.CreateIsnRetrieverResponse
//	@Failure	400		{object}	response.ErrorResponse
//	@Failure	409		{object}	response.ErrorResponse
//	@Failure	500		{object}	response.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/isn/retriever/{retriever_slug} [post]
func (i *IsnRetrieverHandler) CreateIsnRetrieverHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateIsnRetrieverRequest

	userID, ok := context.UserID(r.Context())
	if !ok {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userID from middleware")
		return
	}
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// check isn exists and is owned by user
	isn, err := i.cfg.DB.GetIsnBySlug(r.Context(), req.IsnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isn.UserID != userID {
		response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}

	// check the isn is in use
	if !isn.IsInUse {
		response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this ISN is marked as 'not in use'")
		return
	}

	// check mandatory fields
	if req.DefaultRateLimit == nil || req.RetrieverStatus == nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply a value for all fields")
		return
	}

	if isn.StorageType == "local" {
		if req.RetrieverOrigin != nil {
			response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "do not specify a retriever_origin when using local storage")
			return
		}
		req.RetrieverOrigin = new(string)
		*req.RetrieverOrigin = "local"
	} else {
		if req.RetrieverOrigin != nil || !helpers.IsValidOrigin(*req.RetrieverOrigin) {
			response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must specify a retriever_origin when using local storage, e.g https://example.com")
			return
		}
	}
	if !signals.ValidRetrieverStatus[*req.RetrieverStatus] {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid retriever_status")
		return
	}

	// generate slug.
	slug, err := helpers.GenerateSlug(req.Title)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not create slug from title")
		return
	}

	// check if slug has already been used
	exists, err := i.cfg.DB.ExistsIsnRetrieverWithSlug(r.Context(), slug)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}
	if exists {
		response.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("the {%s} slug is already in use - pick a new title for your ISN retriever", slug))
		return
	}

	// create isn receiever
	returnedIsnRetriever, err := i.cfg.DB.CreateIsnRetriever(r.Context(), database.CreateIsnRetrieverParams{
		UserID:           userID,
		IsnID:            isn.ID,
		Title:            req.Title,
		Slug:             slug,
		Detail:           *req.Detail,
		RetrieverOrigin:  *req.RetrieverOrigin,
		DefaultRateLimit: *req.DefaultRateLimit,
		RetrieverStatus:  *req.RetrieverStatus,
	})
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN retriever: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/isn/retriever/%s",
		helpers.GetScheme(r),
		r.Host,
		slug,
	)

	var retrieverURL string
	if isn.StorageType == "local" {
		retrieverURL = "local"
	} else {
		retrieverURL = fmt.Sprintf("%s/signals/retriever/%s", *req.RetrieverOrigin, slug)
	}

	response.RespondWithJSON(w, http.StatusCreated, CreateIsnRetrieverResponse{
		ID:           returnedIsnRetriever.ID,
		Slug:         returnedIsnRetriever.Slug,
		ResourceURL:  resourceURL,
		RetrieverURL: retrieverURL,
	})
}

// UpdateIsnRetrieverHandler godoc
//
//	@Summary	Update an ISN Retriever
//
//	@Tags		ISN config
//
//	@Param		isn_retrievers_slug	path	string								true	"isn retriever slug"	example(sample-isn-retriever--example-org)
//	@Param		request				body	handlers.UpdateIsnRetrieverRequest	true	"ISN retriever details"
//
//	@Success	204
//	@Failure	400	{object}	response.ErrorResponse
//	@Failure	401	{object}	response.ErrorResponse
//	@Failure	500	{object}	response.ErrorResponse
//
// //
//
//	@Security	BearerAccessToken
//
//	@Router		/api/isn/retriever/{isn_retrievers_slug} [put]
func (i *IsnRetrieverHandler) UpdateIsnRetrieverHandler(w http.ResponseWriter, r *http.Request) {
	var req UpdateIsnRetrieverRequest

	userID, ok := context.UserID(r.Context())
	if !ok {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userID from middleware")
		return
	}

	isnRetrieverSlug := r.PathValue("isn_retrievers_slug")

	// check retriever exists and is owned by user
	isnRetriever, err := i.cfg.DB.GetIsnRetrieverBySlug(r.Context(), isnRetrieverSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN retriever not found")
			return
		}
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	if !isnRetriever.IsnIsInUse {
		response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, fmt.Sprintf("Can't update ISN retriever because ISN %s is not in use", isnRetriever.IsnSlug))
		return
	}

	if isnRetriever.UserID != userID {
		response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN retriever")
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
		isnRetriever.Detail = *req.Detail
	}

	if req.DefaultRateLimit != nil {
		isnRetriever.DefaultRateLimit = *req.DefaultRateLimit
	}

	if req.RetrieverOrigin != nil {
		if *req.RetrieverOrigin != "local" && isnRetriever.IsnStorageType == "local" {
			response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "do not specify a retriever_origin when using local storage")
			return
		}
		if !helpers.IsValidOrigin(*req.RetrieverOrigin) {
			response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must specify a retriever_origin when using anything other than local storage, e.g https://example.com")
			return
		}
		isnRetriever.RetrieverOrigin = *req.RetrieverOrigin
	}

	if req.RetrieverStatus != nil {
		if !signals.ValidRetrieverStatus[*req.RetrieverStatus] {
			response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid retriever status")
			return
		}
		isnRetriever.RetrieverStatus = *req.RetrieverStatus
	}

	// update isn_receiever
	_, err = i.cfg.DB.UpdateIsnRetriever(r.Context(), database.UpdateIsnRetrieverParams{
		ID:               isnRetriever.ID,
		Detail:           isnRetriever.Detail,
		RetrieverOrigin:  isnRetriever.RetrieverOrigin,
		DefaultRateLimit: isnRetriever.DefaultRateLimit,
		RetrieverStatus:  isnRetriever.RetrieverStatus,
	})
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN retriever: %v", err))
		return
	}

	response.RespondWithJSON(w, http.StatusNoContent, "")
}

// GetIsnRetrieverHandler godoc
//
//	@Summary	Get an ISN retriever config
//	@Tags		ISN view
//
//	@Param		slug	path		string	true	"isn retriever slug"	example(sample-isn-retriever--example-org)
//	@Success	200		{array}		database.GetIsnRetrieverBySlugRow
//	@Failure	500		{object}	response.ErrorResponse
//
//	@Router		/api/isn/retriever/{slug} [get]
func (u *IsnRetrieverHandler) GetIsnRetrieverHandler(w http.ResponseWriter, r *http.Request) {

	slug := r.PathValue("slug")

	res, err := u.cfg.DB.GetIsnRetrieverBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No isn_retriever found for id %v", slug))
			return
		}
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user from the database %v", err))
		return
	}
	//
	response.RespondWithJSON(w, http.StatusOK, res)
}
