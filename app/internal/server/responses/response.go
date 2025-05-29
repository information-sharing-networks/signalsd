package responses

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/rs/zerolog"
)

type ErrorResponse struct {
	StatusCode int                 `json:"-"`
	ErrorCode  apperrors.ErrorCode `json:"error_code" example:"example_error_code"`
	Message    string              `json:"message" example:"message describing the error"`
	ReqID      string              `json:"-"`
}

func RespondWithError(w http.ResponseWriter, r *http.Request, statusCode int, errorCode apperrors.ErrorCode, message string) {
	requestLogger := zerolog.Ctx(r.Context())
	requestID := middleware.GetReqID(r.Context())

	// Only log real server errors
	if statusCode >= 500 {
		requestLogger.Error().
			Int("status", statusCode).
			Any("error_code", errorCode).
			Str("error_message", message).
			Str("request_id", requestID).
			Msg("Request failed")
	}

	errResponse := ErrorResponse{
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Message:    message,
		ReqID:      requestID,
	}

	dat, err := json.Marshal(errResponse)
	if err != nil {
		requestLogger.Error().
			Err(err).
			Str("request_id", requestID).
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

	data, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error_code":"marshal_error","message":"Internal Server Error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(data)
}
