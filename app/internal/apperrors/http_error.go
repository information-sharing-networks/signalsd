package apperrors

import (
	"fmt"
	"net/http"
)

// HTTPError holds the error information used when processing handler errors
type HTTPError struct {
	// Status is the HTTP status code
	Status int

	// Code is the internal app error code
	Code ErrorCode

	// Message is the sanitised client message
	Message string

	// Err is the underlying Go error. It is logged but not sent to the client.
	// Pass nil when there is no wrapped error (e.g validation failures)
	Err error
}

func (e *HTTPError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *HTTPError) Unwrap() error { return e.Err }

// InternalError responds with 500 + internal_error.
func InternalError(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusInternalServerError, Code: ErrCodeInternalError, Message: message, Err: err}
}

// DatabaseError responds with 500 + database_error.
func DatabaseError(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusInternalServerError, Code: ErrCodeDatabaseError, Message: message, Err: err}
}

// MalformedBody responds with 400 + malformed_body.
func MalformedBody(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusBadRequest, Code: ErrCodeMalformedBody, Message: message, Err: err}
}

// InvalidRequest responds with 400 + invalid_request.
func InvalidRequest(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusBadRequest, Code: ErrCodeInvalidRequest, Message: message, Err: err}
}

// InvalidURLParam responds with 400 + invalid_url_param.
func InvalidURLParam(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusBadRequest, Code: ErrCodeInvalidURLParam, Message: message, Err: err}
}

// NotFound responds with 404 + resource_not_found.
func NotFound(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusNotFound, Code: ErrCodeResourceNotFound, Message: message, Err: err}
}

// Forbidden responds with 403 + forbidden.
func Forbidden(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusForbidden, Code: ErrCodeForbidden, Message: message, Err: err}
}

// AlreadyExists responds with 409 + resource_already_exists.
func AlreadyExists(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusConflict, Code: ErrCodeResourceAlreadyExists, Message: message, Err: err}
}

// AuthenticationFailure responds with 401 + authentication_error.
// Use for errors authenticating logged in users
func AuthenticationFailure(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusUnauthorized, Code: ErrCodeAuthenticationFailure, Message: message, Err: err}
}

// TokenCreationFailure responds with 500 + token_creation_failed.
func TokenCreationFailure(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusInternalServerError, Code: ErrCodeFailedToCreateToken, Message: message, Err: err}
}

// PasswordTooShort responds with 400 + password_too_short.
func PasswordTooShort(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusBadRequest, Code: ErrCodePasswordTooShort, Message: message, Err: err}
}

// ResourceExpired responds with 410 + resource_expired.
func ResourceExpired(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusGone, Code: ErrCodeResourceExpired, Message: message, Err: err}
}

// ResourceInUse responds with 409 + resource_in_use.
func ResourceInUse(message string, err error) *HTTPError {
	return &HTTPError{Status: http.StatusConflict, Code: ErrCodeResourceInUse, Message: message, Err: err}
}

// OAuthError is returned by OAuth 2.0 endpoints (/oauth/token, /oauth/revoke).
// responses.Render writes it as an RFC 6749 §5.2 compliant response
type OAuthError struct {
	// Status is the HTTP status code
	Status int

	// OauthCode is the RFC 6749 §5.2 error code (e.g. "invalid_grant")
	OAuthCode string

	// human-readable description (sent to client for 4xx; log-only for 5xx)
	Description string

	// AppCode is the internal app error code (logged)
	AppCode ErrorCode // internal app code (logged)

	// Err is the wrapped go error (logged)
	Err error
}

func (e *OAuthError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.OAuthCode, e.Description, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.OAuthCode, e.Description)
}

func (e *OAuthError) Unwrap() error { return e.Err }

// OAuth constructors. Pass nil for err when there is no wrapped error.

// OAuthServerError responds with 500 + server_error.
// Description is logged but not sent to the client.
func OAuthServerError(description string, appCode ErrorCode, err error) *OAuthError {
	return &OAuthError{Status: http.StatusInternalServerError, OAuthCode: "server_error", Description: description, AppCode: appCode, Err: err}
}

// OAuthInvalidRequest responds with 400 + invalid_request.
func OAuthInvalidRequest(description string, appCode ErrorCode, err error) *OAuthError {
	return &OAuthError{Status: http.StatusBadRequest, OAuthCode: "invalid_request", Description: description, AppCode: appCode, Err: err}
}

// OAuthInvalidClient responds with 401 + invalid_client.
func OAuthInvalidClient(description string, appCode ErrorCode, err error) *OAuthError {
	return &OAuthError{Status: http.StatusUnauthorized, OAuthCode: "invalid_client", Description: description, AppCode: appCode, Err: err}
}

// OAuthInvalidGrant responds with 400 + invalid_grant.
func OAuthInvalidGrant(description string, appCode ErrorCode, err error) *OAuthError {
	return &OAuthError{Status: http.StatusBadRequest, OAuthCode: "invalid_grant", Description: description, AppCode: appCode, Err: err}
}

// OAuthUnsupportedGrantType responds with 400 + unsupported_grant_type.
func OAuthUnsupportedGrantType(description string, appCode ErrorCode, err error) *OAuthError {
	return &OAuthError{Status: http.StatusBadRequest, OAuthCode: "unsupported_grant_type", Description: description, AppCode: appCode, Err: err}
}
