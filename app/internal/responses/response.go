package responses

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
)

const internalServerErrorMessage = "Internal Server Error"

// ErrorResponse is the generic app error message format
type ErrorResponse struct {

	// Standard app error code
	ErrorCode apperrors.ErrorCode `json:"error_code" example:"error_code"`

	// Client message
	Message string `json:"message" example:"client message"`

	// request tracking id
	RequestID string `json:"request_id,omitempty" example:"a1b2c3d4"`
}

func writeError(w http.ResponseWriter, r *http.Request, statusCode int, errorCode apperrors.ErrorCode, message string) {
	requestID := middleware.GetReqID(r.Context())
	body, marshalErr := json.Marshal(ErrorResponse{
		ErrorCode: errorCode,
		Message:   message,
		RequestID: requestID,
	})

	if marshalErr != nil {
		slog.Error("error marshaling error response",
			slog.String("error", marshalErr.Error()),
			slog.String("request_id", requestID))
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error_code":"internal_error","message":"Internal Server Error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

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
// ErrorCode is an extension field that holds the internal error_code.
type OAuthErrorResponse struct {

	// Error is the RFC 6749 §5.2 token endpoint error code
	Error string `json:"error" example:"invalid_grant"`

	// ErrorCode contains the internal app error code
	ErrorCode apperrors.ErrorCode `json:"error_code,omitempty" example:"refresh_token_invalid"`

	// ErrorDescripton contains the detailed description
	ErrorDescription string `json:"error_description,omitempty" example:"session expired, please log in again"`
}

func writeOauthError(w http.ResponseWriter, statusCode int, oauthError string, description string, errorCode apperrors.ErrorCode) {
	body, marshalErr := json.Marshal(OAuthErrorResponse{
		Error:            oauthError,
		ErrorDescription: description,
		ErrorCode:        errorCode,
	})
	if marshalErr != nil {
		slog.Error("error marshaling oauth error response",
			slog.String("error", marshalErr.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server_error","error_description":"Internal Server Error"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

// JSON writes payload as a JSON response.
func JSON(w http.ResponseWriter, status int, payload any) error {
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return nil
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
	return nil
}

func NoContent(w http.ResponseWriter, status int) error {
	w.WriteHeader(status)
	return nil
}

// JSONHandler is the handler function type used by API endpoints that return JSON errors.
type JSONHandler func(http.ResponseWriter, *http.Request) error

// Wrap adapts a JSONHandler into an http.HandlerFunc so it can be registered with chi.
func Wrap(fn JSONHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		RenderError(w, r, fn(w, r))
	}
}

// RenderError writes err as a JSON response
//
// For apperrors.OauthError the response is OAuthErrorResponse (RFC 6749 §5.2)
// For apperrors.HTTPError the response is the app standard ErrorResponse format.
// All other errors are rendered as 500, except when context cancellation/timeout
// is detected in this case the error is overwritten with a 499 or 504.
//
// The client message and detailed error are logged. The client just gets
// the sanitised client message.
func RenderError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}

	// Oauth errors
	if oerr, ok := errors.AsType[*apperrors.OAuthError](err); ok {
		attrs := []slog.Attr{
			slog.String("oauth_error", oerr.OAuthCode),
			slog.String("error_code", oerr.AppCode.String()),
		}

		// client messsage
		if oerr.Description != "" {
			attrs = append(attrs, slog.String("description", oerr.Description))
		}

		// error message
		if oerr.Err != nil {
			attrs = append(attrs, slog.String("error", oerr.Err.Error()))
		}

		logger.ContextWithLogAttrs(r.Context(), attrs...)

		// write error json
		if oerr.Status >= 500 {
			writeOauthError(w, oerr.Status, oerr.OAuthCode, internalServerErrorMessage, oerr.AppCode)
		} else {
			writeOauthError(w, oerr.Status, oerr.OAuthCode, oerr.Description, oerr.AppCode)
		}
		return
	}

	// handler errors
	herr, ok := errors.AsType[*apperrors.HTTPError](err)
	if !ok {
		herr = apperrors.InternalError("", err)
	}

	// check for client request cancellations and timeouts
	switch {
	case errors.Is(r.Context().Err(), context.Canceled):
		herr = &apperrors.HTTPError{Status: 499, Code: apperrors.ErrCodeClientClosed, Err: err}
	case errors.Is(r.Context().Err(), context.DeadlineExceeded),
		errors.Is(err, context.DeadlineExceeded):
		herr = &apperrors.HTTPError{Status: http.StatusGatewayTimeout, Code: apperrors.ErrCodeTimeout, Err: err}
	}
	attrs := []slog.Attr{slog.String("error_code", herr.Code.String())}

	// client message
	if herr.Message != "" {
		attrs = append(attrs, slog.String("message", herr.Message))
	}

	// error message
	if herr.Err != nil {
		attrs = append(attrs, slog.String("error", herr.Err.Error()))
	}

	logger.ContextWithLogAttrs(r.Context(), attrs...)

	// write error JSON
	if herr.Status >= 500 {
		writeError(w, r, herr.Status, herr.Code, internalServerErrorMessage)
	} else {
		writeError(w, r, herr.Status, herr.Code, herr.Message)
	}
}
