package apperrors

// ErrorCode represents a specific error condition - used in the backend API
type ErrorCode string

const (
	// ErrCodeAccessTokenExpired used when token validation fails on expiry
	ErrCodeAccessTokenExpired ErrorCode = "access_token_expired"

	// ErrCodeAllSignalsFailedProcessing used when every signal in a batch fails to process
	ErrCodeAllSignalsFailedProcessing ErrorCode = "all_signals_failed_processing"

	// ErrCodeAuthenticationFailure used when credential verification fails (wrong password, unknown email)
	ErrCodeAuthenticationFailure ErrorCode = "authentication_error"

	// ErrCodeAuthorizationFailure used in OAuth responses when a valid client is not permitted (e.g. account disabled)
	ErrCodeAuthorizationFailure ErrorCode = "authorization_error"

	// ErrCodeClientClosed used when the client disconnects before the response is sent (499)
	ErrCodeClientClosed ErrorCode = "client_closed"

	// ErrCodeDatabaseError used when a database operation fails
	ErrCodeDatabaseError ErrorCode = "database_error"

	// ErrCodeForbidden used when the authenticated account lacks the required role or permission (403)
	ErrCodeForbidden ErrorCode = "forbidden"

	// ErrCodeInvalidCorrelationID used when a signal's correlation_id can't be found in the specified ISN
	ErrCodeInvalidCorrelationID ErrorCode = "invalid_correlation_id"

	// ErrCodeInternalError used for unclassified server-side failures
	ErrCodeInternalError ErrorCode = "internal_error"

	// ErrCodeInvalidRequest used when request parameters are present but semantically invalid
	ErrCodeInvalidRequest ErrorCode = "invalid_request"

	// ErrCodeInvalidURLParam used when a URL path parameter cannot be parsed (e.g. malformed UUID)
	ErrCodeInvalidURLParam ErrorCode = "invalid_url_param"

	// ErrCodeMalformedBody used when the request body cannot be decoded (invalid JSON or missing required fields)
	ErrCodeMalformedBody ErrorCode = "malformed_body"

	// ErrCodeNotImplemented used for handlers that are defined but not yet implemented
	ErrCodeNotImplemented ErrorCode = "not_implemented"

	// ErrCodeNetworkError used when an outbound network call fails
	ErrCodeNetworkError ErrorCode = "network_error"

	// ErrCodePasswordTooShort used when the supplied password is below the minimum length
	ErrCodePasswordTooShort ErrorCode = "password_too_short"

	// ErrCodeRefreshTokenInvalid used when the client supplied refresh token is not found, expired, or revoked
	ErrCodeRefreshTokenInvalid ErrorCode = "refresh_token_invalid"

	// ErrCodeRequestTooLarge used when the request body exceeds the configured size limit
	ErrCodeRequestTooLarge ErrorCode = "request_too_large"

	// ErrCodeRateLimitExceeded used when the server can't handle a request because the rate limit was reached
	ErrCodeRateLimitExceeded ErrorCode = "rate_limit_exceeded"

	// ErrCodeResourceAlreadyExists used when a create request conflicts with an existing resource (409)
	ErrCodeResourceAlreadyExists ErrorCode = "resource_already_exists"

	// ErrCodeResourceExpired used when a time-limited resource (e.g. password reset token) is past its expiry (410)
	ErrCodeResourceExpired ErrorCode = "resource_expired"

	// ErrCodeResourceInUse used when a resource cannot be deleted because it is referenced by other data (409)
	ErrCodeResourceInUse ErrorCode = "resource_in_use"

	// ErrCodeResourceNotFound used when the requested resource does not exist (404)
	ErrCodeResourceNotFound ErrorCode = "resource_not_found"

	// ErrCodeTimeout used when a request exceeds its deadline (504)
	ErrCodeTimeout ErrorCode = "timeout"

	// ErrCodeFailedToCreateToken used when token generation fails during login or token refresh
	ErrCodeFailedToCreateToken ErrorCode = "token_creation_failed"

	// OAuth 2.0 error codes (RFC 6749 §5.2)

	// OAuthErrInvalidRequest is returned when the request is missing a required parameter,
	// includes an unsupported parameter value, repeats a parameter, or is otherwise malformed.
	OAuthErrInvalidRequest = "invalid_request"

	// OAuthErrInvalidClient is returned when client authentication failed, such as an unknown
	// client, no client authentication included, or unsupported authentication method.
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

func (e ErrorCode) String() string {
	return string(e)
}
