package responses

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
)

// RFC 6749 §5.2 token endpoint error codes
const (
	// OAuthErrInvalidRequest is returned when the request is missing a required parameter,
	// includes an unsupported parameter value, repeats a parameter, or is otherwise malformed.
	OAuthErrInvalidRequest = "invalid_request"

	// OAuthErrInvalidClient is returned when client authentication failed, such as an unknown
	// client, no client authentication included, or unsupported authentication method. May
	// respond with HTTP 401 when the client authenticated via the Authorization header.
	OAuthErrInvalidClient = "invalid_client"

	// OAuthErrInvalidGrant is returned when the authorization code or refresh token is invalid,
	// expired, revoked, does not match the redirect URI, or was issued to another client.
	OAuthErrInvalidGrant = "invalid_grant"

	// OAuthErrUnauthorizedClient is returned when the authenticated client is not authorised
	// to use this authorization grant type.
	OAuthErrUnauthorizedClient = "unauthorized_client"

	// OAuthErrUnsupportedGrantType is returned when the authorization grant type is not
	// supported by the authorization server.
	OAuthErrUnsupportedGrantType = "unsupported_grant_type"

	// OAuthErrInvalidScope is returned when the requested scope is invalid, unknown, malformed,
	// or exceeds the scope granted by the resource owner.
	OAuthErrInvalidScope = "invalid_scope"

	// OAuthErrServerError is returned when the server encountered an unexpected condition
	// that prevented it from fulfilling the request.
	OAuthErrServerError = "server_error"
)

// OAuthErrorResponse is the RFC 6749 §5.2 token endpoint error response.
// ErrorCode is an extension field that mirrors the internal error_code
type OAuthErrorResponse struct {

	// Error is the RFC 6749 §5.2 token endpoint error code
	Error string `json:"error" example:"invalid_grant"`

	// ErrorCode contains the internal app error code
	ErrorCode apperrors.ErrorCode `json:"error_code,omitempty" example:"refresh_token_invalid"`

	// ErrorDescripton contains the detailed description
	ErrorDescription string `json:"error_description,omitempty" example:"session expired, please log in again"`
}

// RespondWithOAuthError writes an RFC 6749 §5.2 compliant error response.
// Use this for /oauth/token and /oauth/revoke endpoints only.
// For all other API errors use RespondWithError.
func RespondWithOAuthError(w http.ResponseWriter, r *http.Request, statusCode int, oauthError string, description string, errorCode apperrors.ErrorCode) {
	logger.ContextWithLogAttrs(r.Context(),
		slog.String("oauth_error", oauthError),
		slog.String("message", description),
	)

	dat, err := json.Marshal(OAuthErrorResponse{
		Error:            oauthError,
		ErrorDescription: description,
		ErrorCode:        errorCode,
	})
	if err != nil {
		slog.Error("error marshaling oauth error response", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server_error","error_description":"Internal Server Error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(dat)
}

type ErrorResponse struct {
	StatusCode int                 `json:"-"`
	ErrorCode  apperrors.ErrorCode `json:"error_code" example:"example_error_code"`
	Message    string              `json:"message" example:"message describing the error"`
	ReqID      string              `json:"-"`
}

func RespondWithError(w http.ResponseWriter, r *http.Request, statusCode int, errorCode apperrors.ErrorCode, message string) {
	requestID := middleware.GetReqID(r.Context())

	errResponse := ErrorResponse{
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Message:    message,
		ReqID:      requestID,
	}

	dat, err := json.Marshal(errResponse)
	if err != nil {
		// Log marshal error directly since this is a critical system error
		slog.Error("error marshaling error response", slog.String("error", err.Error()), slog.String("request_id", requestID))
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error_code":"internal_error","message":"Internal Server Error"}`))
		return
	}

	// add user message to the final log message
	logger.ContextWithLogAttrs(r.Context(), slog.String("message", message), slog.String("error_code", errorCode.String()))

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
