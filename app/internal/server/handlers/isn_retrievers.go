package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/utils"

	signalsd "github.com/nickabs/signalsd/app"
)

type IsnRetrieverHandler struct {
	queries *database.Queries
}

func NewIsnRetrieverHandler(queries *database.Queries) *IsnRetrieverHandler {
	return &IsnRetrieverHandler{queries: queries}
}

type CreateIsnRetrieverRequest struct {
	IsnSlug          string `json:"isn_slug" example:"sample-isn--example-org"`
	DefaultRateLimit *int32 `json:"default_rate_limit" example:"600"` //maximum number of requests per minute per session
	ListenerCount    *int32 `json:"listener_count" example:"1"`
}

type CreateIsnRetrieverResponse struct {
	ResourceURL string `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn--example-org/signals/retriever"`
}

type UpdateIsnRetrieverRequest struct {
	DefaultRateLimit *int32  `json:"default_rate_limit" example:"600"` //maximum number of requests per minute per session
	RetrieverStatus  *string `json:"retriever_status" example:"offline" enums:"offline,online,error,closed"`
	ListenerCount    *int32  `json:"listener_count" example:"1"`
}

// CreateIsnRetrieverHandler godoc
//
//	@Summary		Create an ISN Retriever definition
//	@Description	An ISN retriever handles the http requests sent by clients to get Signals from the ISN
//	@Description
//	@Description	You can specify how many retrievers should be started for the ISN and they will listen on an automatically generted port
//	@Description
//	@Description	The public facing url will be hosted on https://{isn_host}/isn/{isn_slug}/signals/retriever
//	@Description	the isn_host will typically be a load balancer or API gateway that proxies requests to the internal signald services
//	@Description
//	@Description	note retrievers are created in 'offline' mode.
//
//	@Tags			ISN config
//
//	@Param			request	body		handlers.CreateIsnRetrieverRequest	true	"ISN retriever details"
//
//	@Success		201		{object}	handlers.CreateIsnRetrieverResponse
//	@Failure		400		{object}	utils.ErrorResponse
//	@Failure		409		{object}	utils.ErrorResponse
//	@Failure		500		{object}	utils.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signals/retriever [post]
func (i *IsnRetrieverHandler) CreateIsnRetrieverHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateIsnRetrieverRequest

	isnSlug := r.PathValue("isn_slug")

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// check isn exists and is owned by user
	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isn.UserAccountID != userAccountID {
		utils.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}

	// check the isn is in use
	if !isn.IsInUse {
		utils.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this ISN is marked as 'not in use'")
		return
	}

	// check if the isn retriever already exists
	exists, err := i.queries.ExistsIsnRetriever(r.Context(), isn.ID)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}
	if exists {
		utils.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("Retriever already exists for isn %s", isn.Slug))
		return
	}

	// check all fields were supplied
	if req.DefaultRateLimit == nil ||
		req.ListenerCount == nil {
		utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply a value for all fields")
		return
	}

	// create isn retriever
	_, err = i.queries.CreateIsnRetriever(r.Context(), database.CreateIsnRetrieverParams{
		IsnID:            isn.ID,
		DefaultRateLimit: *req.DefaultRateLimit,
		ListenerCount:    *req.ListenerCount,
	})
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN retriever: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/isn/%s/signals/retriever", utils.GetScheme(r), r.Host, isn.Slug)

	utils.RespondWithJSON(w, http.StatusCreated, CreateIsnRetrieverResponse{
		ResourceURL: resourceURL,
	})
}

// UpdateIsnRetrieverHandler godoc
//
//	@Summary	Update an ISN Retriever
//
//	@Tags		ISN config
//
//	@Param		isn_slug	path	string								true	"isn slug"	example(sample-isn--example-org)
//	@Param		request		body	handlers.UpdateIsnRetrieverRequest	true	"ISN retriever details"
//
//	@Success	204
//	@Failure	400	{object}	utils.ErrorResponse
//	@Failure	401	{object}	utils.ErrorResponse
//	@Failure	500	{object}	utils.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/isn/{isn_slug}/signals/retriever [put]
func (i *IsnRetrieverHandler) UpdateIsnRetrieverHandler(w http.ResponseWriter, r *http.Request) {
	var req UpdateIsnRetrieverRequest

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	isnSlug := r.PathValue("isn_slug")

	// check isn exists and is owned by user
	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isn.UserAccountID != userAccountID {
		utils.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}

	if !isn.IsInUse {
		utils.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, fmt.Sprintf("Can't update ISN retriever because ISN %s is not in use", isnSlug))
		return
	}

	// check retriever exists and is owned by user
	isnRetriever, err := i.queries.GetIsnRetrieverByIsnSlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN retriever not found")
			return
		}
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// prepare update fields
	if req.DefaultRateLimit != nil {
		isnRetriever.DefaultRateLimit = *req.DefaultRateLimit
	}
	if req.RetrieverStatus != nil {
		if !signalsd.ValidRetrieverStatus[*req.RetrieverStatus] {
			utils.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid payload validation")
			return
		}
		isnRetriever.RetrieverStatus = *req.RetrieverStatus
	}

	if req.ListenerCount != nil {
		isnRetriever.ListenerCount = *req.ListenerCount
	}
	// update isn retriever - todo checks on rows updated
	_, err = i.queries.UpdateIsnRetriever(r.Context(), database.UpdateIsnRetrieverParams{
		IsnID:            isn.ID,
		DefaultRateLimit: isnRetriever.DefaultRateLimit,
		RetrieverStatus:  isnRetriever.RetrieverStatus,
		ListenerCount:    isnRetriever.ListenerCount,
	})
	if err != nil {
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not update ISN retriever: %v", err))
		return
	}

	utils.RespondWithJSON(w, http.StatusNoContent, "")
}

// GetIsnRetrieverHandler godoc
//
//	@Summary	Get an ISN retriever config
//	@Tags		ISN view
//
//	@Param		slug	path		string	true	"isn slug"	example(sample-isn--example-org)
//	@Success	200		{array}		database.GetIsnRetrieverByIsnSlugRow
//	@Failure	500		{object}	utils.ErrorResponse
//
//	@Router		/api/isn/{isn_slug}/signals/retriever [get]
func (u *IsnRetrieverHandler) GetIsnRetrieverHandler(w http.ResponseWriter, r *http.Request) {

	isnSlug := r.PathValue("isn_slug")

	res, err := u.queries.GetIsnRetrieverByIsnSlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			utils.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No isn_retriever found for id %v", isnSlug))
			return
		}
		utils.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user from the database %v", err))
		return
	}
	//
	utils.RespondWithJSON(w, http.StatusOK, res)
}
