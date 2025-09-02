package responses

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
)

type ErrorResponse struct {
	StatusCode int                 `json:"-"`
	ErrorCode  apperrors.ErrorCode `json:"error_code" example:"example_error_code"`
	Message    string              `json:"message" example:"message describing the error"`
	ReqID      string              `json:"-"`
}

func RespondWithError(w http.ResponseWriter, r *http.Request, statusCode int, errorCode apperrors.ErrorCode, message string) {
	requestLogger := logger.ContextLogger(r.Context())
	requestID := middleware.GetReqID(r.Context())

	// Log the error with appropriate level
	switch {
	case statusCode >= 500:
		requestLogger.Error("Request failed",
			slog.Int("status", statusCode),
			slog.String("error_code", string(errorCode)),
			slog.String("error_message", message))
	case statusCode >= 400:
		requestLogger.Warn("Request failed",
			slog.Int("status", statusCode),
			slog.String("error_code", string(errorCode)),
			slog.String("error_message", message))
	default:
		requestLogger.Info("Request failed",
			slog.Int("status", statusCode),
			slog.String("error_code", string(errorCode)),
			slog.String("error_message", message))
	}

	errResponse := ErrorResponse{
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Message:    message,
		ReqID:      requestID,
	}

	dat, err := json.Marshal(errResponse)
	if err != nil {
		requestLogger.Error("error marshaling error response",
			slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error_code":"internal_error","message":"Internal Server Error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(dat)
}

func RespondWithJSON(w http.ResponseWriter, status int, payload any) {
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error_code":"marshal_error","message":"Internal Server Error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func RespondWithStatusCodeOnly(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}
