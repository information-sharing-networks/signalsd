package apperrors

// ErrorCode represents a specific error condition - used in the backend API
type ErrorCode string

const (
	ErrCodeAccessTokenExpired         ErrorCode = "access_token_expired"
	ErrCodeAllSignalsFailedProcessing ErrorCode = "all_signals_failed_processing"
	ErrCodeAuthenticationFailure      ErrorCode = "authentication_error"
	ErrCodeAuthorizationFailure       ErrorCode = "authorization_error"
	ErrCodeClientClosed               ErrorCode = "client_closed"
	ErrCodeDatabaseError              ErrorCode = "database_error"
	ErrCodeForbidden                  ErrorCode = "forbidden"
	ErrCodeInvalidCorrelationID       ErrorCode = "invalid_correlation_id"
	ErrCodeInternalError              ErrorCode = "internal_error"
	ErrCodeInvalidRequest             ErrorCode = "invalid_request"
	ErrCodeInvalidURLParam            ErrorCode = "invalid_url_param"
	ErrCodeMalformedBody              ErrorCode = "malformed_body"
	ErrCodeNotImplemented             ErrorCode = "not_implemented"
	ErrCodeNetworkError               ErrorCode = "network_error"
	ErrCodePasswordTooShort           ErrorCode = "password_too_short"
	ErrCodeRefreshTokenInvalid        ErrorCode = "refresh_token_invalid"
	ErrCodeRequestTooLarge            ErrorCode = "request_too_large"
	ErrCodeRateLimitExceeded          ErrorCode = "rate_limit_exceeded"
	ErrCodeResourceAlreadyExists      ErrorCode = "resource_already_exists"
	ErrCodeResourceExpired            ErrorCode = "resource_expired"
	ErrCodeResourceInUse              ErrorCode = "resource_in_use"
	ErrCodeResourceNotFound           ErrorCode = "resource_not_found"
	ErrCodeTimeout                    ErrorCode = "timeout"
	ErrCodeTokenInvalid               ErrorCode = "token_invalid"
)

func (e ErrorCode) String() string {
	return string(e)
}
