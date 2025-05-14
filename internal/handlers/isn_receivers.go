package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/database"
	"github.com/nickabs/signals/internal/helpers"
)

type IsnReceiverHandler struct {
	cfg *signals.ServiceConfig
}

func NewIsnReceiverHandler(cfg *signals.ServiceConfig) *IsnReceiverHandler {
	return &IsnReceiverHandler{cfg: cfg}
}

type CreateIsnReceiverRequest struct {
	Title   string `json:"title" example:"Sample ISN Receiver @example.org"`
	IsnSlug string `json:"isn_slug" example:"sample-ISN--example-org"`
	UpdateIsnReceiverRequest
}

type CreateIsnReceiverResponse struct {
	ID          uuid.UUID `json:"id" example:"4f1bc74b-cf79-410f-9c21-dc2cba047385"`
	Slug        string    `json:"slug" example:"sample-ISN-receiver--example-org"`
	ResourceURL string    `json:"resource_url" example:"http://localhost:8080/api/isn/receiver/sample-ISN-receiver--example-org"`
	ReceiverURL string    `json:"receiver_url" example:"http://localhost:8080/signals/receiver/sample-ISN-receiver--example-org"`
}

type UpdateIsnReceiverRequest struct {
	Detail                     *string `json:"detail" example:"Sample ISN Receiver description"`
	ReceiverOrigin             *string `json:"receiver_origin" example:"http://example.com:8080"` // do not provide this field if the isn is using local storage
	MinBatchRecords            *int32  `json:"min_batch_records" example:"10"`
	MaxBatchRecords            *int32  `json:"max_batch_records" example:"100"`
	MaxDailyValidationFailures *int32  `json:"max_daily_validation_failures" example:"5"` //default = 0
	MaxPayloadKilobytes        *int32  `json:"max_payload_kilobytes" example:"50"`
	PayloadValidation          *string `json:"payload_validation" example:"always" enums:"always,never,optional"`
	DefaultRateLimit           *int32  `json:"default_rate_limit" example:"600"` //maximum number of requests per minute
	ReceiverStatus             *string `json:"receiver_status" example:"offline" enums:"offline, online, error, closed"`
}

// CreateIsnReceiverHandler godoc
//
//	@Summary		Create an ISN Receiver
//	@Description	A receiver service handles incoming signals and will be hosted on {receiver_origin}/signals/receiver/{receiver_slug}
//	@Description
//	@Description	When the ISN storage_type is set to "local", the receiver_origin must also be "local", indicating that the signals are stored in the relational database used by the API service.
//	@Description
//	@Description	the receiver service should be hosted on {receiver_origin}/signals/receiver/{receiver_slug}
//
//	@Tags			ISN config
//
//	@Param			request	body		handlers.CreateIsnReceiverRequest	true	"ISN receiver details"
//
//	@Success		201		{object}	handlers.CreateIsnReceiverResponse
//	@Failure		400		{object}	signals.ErrorResponse
//	@Failure		409		{object}	signals.ErrorResponse
//	@Failure		500		{object}	signals.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/receiver/{receiver_slug} [post]
func (i *IsnReceiverHandler) CreateIsnReceiverHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateIsnReceiverRequest

	userID, ok := r.Context().Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
		return
	}
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// check isn exists and is owned by user
	isn, err := i.cfg.DB.GetIsnBySlug(r.Context(), req.IsnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusNotFound, signals.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isn.UserID != userID {
		helpers.RespondWithError(w, r, http.StatusForbidden, signals.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}

	// check the isn is in use
	if !isn.IsInUse {
		helpers.RespondWithError(w, r, http.StatusForbidden, signals.ErrCodeForbidden, "this ISN is marked as 'not in use'")
		return
	}

	// check all fields were supplied
	if req.Detail == nil ||
		req.MinBatchRecords == nil ||
		req.MaxBatchRecords == nil ||
		req.MaxDailyValidationFailures == nil ||
		req.MaxPayloadKilobytes == nil ||
		req.DefaultRateLimit == nil ||
		req.ReceiverStatus == nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "you must supply a value for all fields")
		return
	}

	if isn.StorageType == "local" {
		if req.ReceiverOrigin != nil {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "do not specify a receiver_origin when using local storage")
			return
		}
		req.ReceiverOrigin = new(string)
		*req.ReceiverOrigin = "local"
	} else {
		if req.ReceiverOrigin != nil || !helpers.IsValidOrigin(*req.ReceiverOrigin) {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "you must specify a receiver_origin when using anything other than local storage, e.g https://example.com")
			return
		}
	}

	if !signals.ValidReceiverStatus[*req.ReceiverStatus] {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "invalid receiver status")
		return
	}

	if !signals.ValidPayloadValidations[*req.PayloadValidation] {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "invalid payload validation")
		return
	}

	// generate slug and check it is not already in use.
	slug, err := helpers.GenerateSlug(req.Title)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "could not create slug from title")
		return
	}

	exists, err := i.cfg.DB.ExistsIsnReceiverWithSlug(r.Context(), slug)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}
	if exists {
		helpers.RespondWithError(w, r, http.StatusConflict, signals.ErrCodeResourceAlreadyExists, fmt.Sprintf("the {%s} slug is already in use - pick a new title for your ISN receiver", slug))
		return
	}

	// create isn receiever
	returnedIsnReceiver, err := i.cfg.DB.CreateIsnReceiver(r.Context(), database.CreateIsnReceiverParams{
		UserID:                     userID,
		IsnID:                      isn.ID,
		Title:                      req.Title,
		Slug:                       slug,
		Detail:                     *req.Detail,
		ReceiverOrigin:             *req.ReceiverOrigin,
		MinBatchRecords:            *req.MinBatchRecords,
		MaxBatchRecords:            *req.MaxBatchRecords,
		MaxDailyValidationFailures: *req.MaxDailyValidationFailures,
		MaxPayloadKilobytes:        *req.MaxPayloadKilobytes,
		PayloadValidation:          *req.PayloadValidation,
		DefaultRateLimit:           *req.DefaultRateLimit,
		ReceiverStatus:             *req.ReceiverStatus,
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN receiver: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/isn/receiver/%s",
		helpers.GetScheme(r),
		r.Host,
		slug,
	)

	var receiverURL string
	if isn.StorageType == "local" {
		receiverURL = "local"
	} else {
		receiverURL = fmt.Sprintf("%s/signals/receiver/%s", *req.ReceiverOrigin, slug)
	}

	helpers.RespondWithJSON(w, http.StatusCreated, CreateIsnReceiverResponse{
		ID:          returnedIsnReceiver.ID,
		Slug:        returnedIsnReceiver.Slug,
		ResourceURL: resourceURL,
		ReceiverURL: receiverURL,
	})
}

// UpdateIsnReceiverHandler godoc
//
//	@Summary		Update an ISN Receiver
//	@Description	the receiver service should be hosted on {receiver_origin}/signals/receiver/{receiver_slug}
//
//	@Tags			ISN config
//
//	@Param			isn_receivers_slug	path	string								true	"isn receiver slug"	example(sample-ISN-receiver--example-org)
//	@Param			request				body	handlers.UpdateIsnReceiverRequest	true	"ISN receiver details"
//
//	@Success		204
//	@Failure		400	{object}	signals.ErrorResponse
//	@Failure		401	{object}	signals.ErrorResponse
//	@Failure		500	{object}	signals.ErrorResponse
//
// //
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/receiver/{isn_receivers_slug} [put]
func (i *IsnReceiverHandler) UpdateIsnReceiverHandler(w http.ResponseWriter, r *http.Request) {
	var req UpdateIsnReceiverRequest

	userID, ok := r.Context().Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
		return
	}

	// check receiver exists and is owned by user
	isnReceiverSlug := r.PathValue("isn_receivers_slug")
	isnReceiver, err := i.cfg.DB.GetIsnReceiverBySlug(r.Context(), isnReceiverSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusNotFound, signals.ErrCodeResourceNotFound, "ISN receiver not found")
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}

	if !isnReceiver.IsnIsInUse {
		helpers.RespondWithError(w, r, http.StatusForbidden, signals.ErrCodeForbidden, fmt.Sprintf("Can't update ISN receiver because ISN %s is not in use", isnReceiver.IsnSlug))
		return
	}

	if isnReceiver.UserID != userID {
		helpers.RespondWithError(w, r, http.StatusForbidden, signals.ErrCodeForbidden, "you are not the owner of this ISN receiver")
		return
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// prepare update fields
	if req.Detail != nil {
		isnReceiver.Detail = *req.Detail
	}
	if req.MinBatchRecords != nil {
		isnReceiver.MinBatchRecords = *req.MinBatchRecords
	}
	if req.MaxBatchRecords != nil {
		isnReceiver.MaxBatchRecords = *req.MaxBatchRecords
	}
	if req.MaxDailyValidationFailures != nil {
		isnReceiver.MaxDailyValidationFailures = *req.MaxDailyValidationFailures
	}
	if req.MaxPayloadKilobytes != nil {
		isnReceiver.MaxPayloadKilobytes = *req.MaxPayloadKilobytes
	}
	if req.PayloadValidation != nil {
		if !signals.ValidPayloadValidations[*req.PayloadValidation] {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "invalid payload validation")
			return
		}
		isnReceiver.PayloadValidation = *req.PayloadValidation
	}
	if req.DefaultRateLimit != nil {
		isnReceiver.DefaultRateLimit = *req.DefaultRateLimit
	}
	if req.ReceiverStatus != nil {
		if !signals.ValidReceiverStatus[*req.ReceiverStatus] {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "invalid payload validation")
			return
		}
		isnReceiver.ReceiverStatus = *req.ReceiverStatus
	}

	if req.ReceiverOrigin != nil {
		if *req.ReceiverOrigin != "local" && isnReceiver.IsnStorageType == "local" {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "do not specify a receiver_origin when using local storage")
			return
		}
		if !helpers.IsValidOrigin(*req.ReceiverOrigin) {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "you must specify a receiver_origin when using anything other than local storage, e.g https://example.com")
			return
		}
		isnReceiver.ReceiverOrigin = *req.ReceiverOrigin
	}

	// update isn receiever - todo checks on rows updated
	_, err = i.cfg.DB.UpdateIsnReceiver(r.Context(), database.UpdateIsnReceiverParams{
		ID:                         isnReceiver.ID,
		Detail:                     isnReceiver.Detail,
		ReceiverOrigin:             isnReceiver.ReceiverOrigin,
		MinBatchRecords:            isnReceiver.MinBatchRecords,
		MaxBatchRecords:            isnReceiver.MaxBatchRecords,
		MaxDailyValidationFailures: isnReceiver.MaxDailyValidationFailures,
		MaxPayloadKilobytes:        isnReceiver.MaxPayloadKilobytes,
		PayloadValidation:          isnReceiver.PayloadValidation,
		DefaultRateLimit:           isnReceiver.DefaultRateLimit,
		ReceiverStatus:             isnReceiver.ReceiverStatus,
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("could not create ISN receiver: %v", err))
		return
	}

	helpers.RespondWithJSON(w, http.StatusNoContent, "")
}
