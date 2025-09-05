package ui

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"syscall"
)

type UIErrorType string

const (
	ErrorTypeValidation     UIErrorType = "validation"
	ErrorTypeAuthentication UIErrorType = "authentication"
	ErrorTypeNetwork        UIErrorType = "network"
	ErrorTypeSystem         UIErrorType = "system"
	ErrorTypePermission     UIErrorType = "permission"
)

type UIError struct {
	Type    UIErrorType
	Message string
}

// Error implements the error interface
func (e UIError) Error() string {
	return e.Message
}

// user-friendly error messages
var userErrorMessages = map[UIErrorType]string{
	ErrorTypeValidation:     "Please correct the errors and try again.",
	ErrorTypeAuthentication: "Login failed. Please check your email and password and try again.",
	ErrorTypeNetwork:        "Unable to connect. Please try again in a few moments.",
	ErrorTypeSystem:         "A system error occurred. Please try again later.",
	ErrorTypePermission:     "You don't have permission to perform this action.",
}

// isNetworkError uses error.As to check for network errors
func isNetworkError(err error) bool {
	// Unwrap the error to get to the root cause
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true // Any net.Error (timeout, temporary, etc.)
	}

	// Check for specific network error types
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true // Network operation errors (connection refused, etc.)
	}

	// Check for DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Check for URL errors (which often wrap network errors)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// Recursively check the wrapped error
		return isNetworkError(urlErr.Err)
	}

	// Check for syscall errors that indicate network issues
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) {
		return true
	}

	return false
}

// HTTPError represents an HTTP error with status code (defined here to avoid import cycles)
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return e.Message
}

// CategorizeError converts errors into user-friendly UIError instances
func CategorizeError(statusCode int, err error) UIError {
	// Handle network/connection errors
	if err != nil {
		if isNetworkError(err) {
			return UIError{
				Type:    ErrorTypeNetwork,
				Message: userErrorMessages[ErrorTypeNetwork],
			}
		}

		// Handle HTTPError (extract status code from error)
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			statusCode = httpErr.StatusCode
		}
	}

	// Handle errors based on HTTP status code (most reliable indicator)
	switch statusCode {
	case http.StatusUnauthorized: // 401
		return UIError{
			Type:    ErrorTypeAuthentication,
			Message: userErrorMessages[ErrorTypeAuthentication],
		}
	case http.StatusForbidden: // 403
		return UIError{
			Type:    ErrorTypePermission,
			Message: userErrorMessages[ErrorTypePermission],
		}
	case http.StatusBadRequest: // 400
		return UIError{
			Type:    ErrorTypeValidation,
			Message: userErrorMessages[ErrorTypeValidation],
		}
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout: // 5xx
		return UIError{
			Type:    ErrorTypeSystem,
			Message: userErrorMessages[ErrorTypeSystem],
		}
	default:
		// For unknown status codes or client-side errors, default to system error
		return UIError{
			Type:    ErrorTypeSystem,
			Message: userErrorMessages[ErrorTypeSystem],
		}
	}
}
