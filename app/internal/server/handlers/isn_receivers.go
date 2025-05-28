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
	"github.com/nickabs/signalsd/app/internal/server/responses"
	"github.com/nickabs/signalsd/app/internal/server/utils"

	signalsd "github.com/nickabs/signalsd/app"
)

type IsnReceiverHandler struct {
	queries *database.Queries
}

func NewIsnReceiverHandler(queries *database.Queries) *IsnReceiverHandler {
	return &IsnReceiverHandler{queries: queries}
}

type CreateIsnReceiverRequest struct {
	IsnSlug                    string  `json:"isn_slug" example:"sample-isn--example-org"`
	MaxDailyValidationFailures *int32  `json:"max_daily_validation_failures" example:"5"` //default = 0
	MaxPayloadKilobytes        *int32  `json:"max_payload_kilobytes" example:"50"`
	PayloadValidation          *string `json:"payload_validation" example:"always" enums:"always,never,optional"`
	DefaultRateLimit           *int32  `json:"default_rate_limit" example:"600"` //maximum number of requests per minute per session
	ListenerCount              *int32  `json:"listener_count" example:"1"`
}

type CreateIsnReceiverResponse struct {
	ResourceURL string `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn--example-org/signals/receiver"`
}

type UpdateIsnReceiverRequest struct {
	MaxDailyValidationFailures *int32  `json:"max_daily_validation_failures" example:"5"` //default = 0
	MaxPayloadKilobytes        *int32  `json:"max_payload_kilobytes" example:"50"`
	PayloadValidation          *string `json:"payload_validation" example:"always" enums:"always,never,optional"`
	DefaultRateLimit           *int32  `json:"default_rate_limit" example:"600"` //maximum number of requests per minute per session
	ReceiverStatus             *string `json:"receiver_status" example:"offline" enums:"offline,online,error,closed"`
	ListenerCount              *int32  `json:"listener_count" example:"1"`
}

// CreateIsnReceiverHandler godoc
//
//	@Summary		Create an ISN Receiver definition
//	@Description	An ISN receiver handles the http requests sent by clients that pass Signals to the ISN
//	@Description
//	@Description	You can specify how many receivers should be started for the ISN and they will listen on an automatically generted port, starting at 8081
//	@Description
//	@Description	The public facing url will be hosted on https://{isn_host}/isn/{isn_slug}/signals/receiver
//	@Description	the isn_host will typically be a load balancer or API gateway that proxies requests to the internal signald services
//	@Description
//	@Description	note receivers are always created in 'offline' mode.
//	@Description
//	@Description	This endpoint can only be used by the site owner or the ISN admin
//
//	@Tags			ISN config
//
//	@Param			isn_slug	path		string								true	"isn slug"	example(sample-isn--example-org)
//	@Param			request		body		handlers.CreateIsnReceiverRequest	true	"ISN receiver details"
//
//	@Success		201			{object}	handlers.CreateIsnReceiverResponse
//	@Failure		400			{object}	responses.ErrorResponse
//	@Failure		409			{object}	responses.ErrorResponse
//	@Failure		500			{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signals/receiver [post]
func (i *IsnReceiverHandler) CreateIsnReceiverHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateIsnReceiverRequest

	isnSlug := r.PathValue("isn_slug")

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

	// check isn exists and is owned by user
	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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

	// check the isn is in use
	if !isn.IsInUse {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this ISN is marked as 'not in use'")
		return
	}

	// check if the isn receiver already exists
	exists, err := i.queries.ExistsIsnReceiver(r.Context(), isn.ID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}
	if exists {
		responses.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("Receiver already exists for isn %s", isn.Slug))
		return
	}

	// check all fields were supplied
	if req.MaxDailyValidationFailures == nil ||
		req.MaxPayloadKilobytes == nil ||
		req.DefaultRateLimit == nil ||
		req.ListenerCount == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply a value for all fields")
		return
	}

	if !signalsd.ValidPayloadValidations[*req.PayloadValidation] {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid payload validation")
		return
	}

	// create isn receiver
	_, err = i.queries.CreateIsnReceiver(r.Context(), database.CreateIsnReceiverParams{
		IsnID:                      isn.ID,
		MaxDailyValidationFailures: *req.MaxDailyValidationFailures,
		MaxPayloadKilobytes:        *req.MaxPayloadKilobytes,
		PayloadValidation:          *req.PayloadValidation,
		DefaultRateLimit:           *req.DefaultRateLimit,
		ListenerCount:              *req.ListenerCount,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN receiver: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/isn/%s/signals/receiver", utils.GetScheme(r), r.Host, isn.Slug)

	responses.RespondWithJSON(w, http.StatusCreated, CreateIsnReceiverResponse{
		ResourceURL: resourceURL,
	})
}

// UpdateIsnReceiverHandler godoc
//
//	@Summary	Update an ISN Receiver
//
//	@Tags		ISN config
//
//	@Description
//	@Description	This endpoint can only be used by the site owner or the ISN admin
//
//	@Param			isn_slug	path	string								true	"isn slug"	example(sample-isn--example-org)
//	@Param			request		body	handlers.UpdateIsnReceiverRequest	true	"ISN receiver details"
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signals/receiver [put]
func (i *IsnReceiverHandler) UpdateIsnReceiverHandler(w http.ResponseWriter, r *http.Request) {
	var req UpdateIsnReceiverRequest

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	isnSlug := r.PathValue("isn_slug")

	// check isn exists and is owned by user
	isn, err := i.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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

	if !isn.IsInUse {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, fmt.Sprintf("Can't update ISN receiver because ISN %s is not in use", isnSlug))
		return
	}

	// check receiver exists and is owned by user
	isnReceiver, err := i.queries.GetIsnReceiverByIsnSlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN receiver not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
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

	// prepare update fields
	if req.MaxDailyValidationFailures != nil {
		isnReceiver.MaxDailyValidationFailures = *req.MaxDailyValidationFailures
	}
	if req.MaxPayloadKilobytes != nil {
		isnReceiver.MaxPayloadKilobytes = *req.MaxPayloadKilobytes
	}
	if req.PayloadValidation != nil {
		if !signalsd.ValidPayloadValidations[*req.PayloadValidation] {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid payload validation")
			return
		}
		isnReceiver.PayloadValidation = *req.PayloadValidation
	}
	if req.DefaultRateLimit != nil {
		isnReceiver.DefaultRateLimit = *req.DefaultRateLimit
	}
	if req.ReceiverStatus != nil {
		if !signalsd.ValidReceiverStatus[*req.ReceiverStatus] {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid payload validation")
			return
		}
		isnReceiver.ReceiverStatus = *req.ReceiverStatus
	}

	if req.ListenerCount != nil {
		isnReceiver.ListenerCount = *req.ListenerCount
	}

	_, err = i.queries.UpdateIsnReceiver(r.Context(), database.UpdateIsnReceiverParams{
		IsnID:                      isn.ID,
		MaxDailyValidationFailures: isnReceiver.MaxDailyValidationFailures,
		MaxPayloadKilobytes:        isnReceiver.MaxPayloadKilobytes,
		PayloadValidation:          isnReceiver.PayloadValidation,
		DefaultRateLimit:           isnReceiver.DefaultRateLimit,
		ReceiverStatus:             isnReceiver.ReceiverStatus,
		ListenerCount:              isnReceiver.ListenerCount,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not update ISN receiver: %v", err))
		return
	}

	responses.RespondWithJSON(w, http.StatusNoContent, "")
}

// GetIsnReceiverHandler godoc
//
//	@Summary	Get an ISN receiver config
//	@Tags		ISN view
//
//	@Param		slug	path		string	true	"isn slug"	example(sample-isn--example-org)
//	@Success	200		{array}		database.GetIsnReceiverByIsnSlugRow
//	@Failure	500		{object}	responses.ErrorResponse
//
//	@Router		/api/isn/{isn_slug}/signals/receiver [get]
func (u *IsnReceiverHandler) GetIsnReceiverHandler(w http.ResponseWriter, r *http.Request) {

	isnSlug := r.PathValue("isn_slug")

	res, err := u.queries.GetIsnReceiverByIsnSlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No isn_receiver found for id %v", isnSlug))
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user from the database %v", err))
		return
	}
	//
	responses.RespondWithJSON(w, http.StatusOK, res)
}
