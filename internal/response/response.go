package response

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/nickabs/signals/internal/apperrors"
	"github.com/nickabs/signals/internal/context"
	"github.com/rs/zerolog/log"
)

type ErrorResponse struct {
	StatusCode int                 `json:"-"`
	ErrorCode  apperrors.ErrorCode `json:"error_code" example:"example_error_code"`
	Message    string              `json:"message"`
	ReqID      string              `json:"-"`
}

func RespondWithError(w http.ResponseWriter, r *http.Request, statusCode int, errorCode apperrors.ErrorCode, message string) {
	reqLog, ok := context.RequestLogger(r.Context())
	if !ok {
		reqLog = &log.Logger
	}
	reqID := middleware.GetReqID(r.Context())

	reqLog.Error().
		Int("status", statusCode).
		Any("error_code", errorCode).
		Str("error_message", message).
		Str("request_id", reqID).
		Msg("Error response")

	errResponse := ErrorResponse{
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Message:    message,
		ReqID:      reqID,
	}

	dat, err := json.Marshal(errResponse)
	if err != nil {
		reqLog.Error().
			Err(err).
			Str("request_id", reqID).
			Msg("error marshaling error response")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error_code":"internal_error","message":"Internal Server Error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	w.Write(dat)
}

func RespondWithJSON(w http.ResponseWriter, status int, payload any) {

	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}

	dat, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(dat)
}
