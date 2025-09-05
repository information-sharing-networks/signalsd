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
	Type             UIErrorType
	ClientStatusCode int
	UserMessage      string
}

// Error implements the error interface
func (e UIError) Error() string {
	return e.UserMessage
}

// user-friendly error messages
var userErrorMessages = map[UIErrorType]string{
	ErrorTypeValidation:     "Please check your input and try again.",
	ErrorTypeAuthentication: "Login failed. Please check your email and password.",
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
		return true // DNS lookup failures
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

// categorizeError todo
func categorizeError(statusCode int, apiErrorCode string, err error) UIError {
	// Handle network/connection errors
	if err != nil {
		if isNetworkError(err) {
			return UIError{
				Type:             ErrorTypeNetwork,
				ClientStatusCode: http.StatusServiceUnavailable,
				UserMessage:      userErrorMessages[ErrorTypeNetwork],
			}
		}
	}

	// Handle errors based on HTTP status code (most reliable indicator)
	switch statusCode {
	case http.StatusUnauthorized: // 401
		return UIError{
			Type:             ErrorTypeAuthentication,
			ClientStatusCode: statusCode,
			UserMessage:      userErrorMessages[ErrorTypeAuthentication],
		}
	case http.StatusForbidden: // 403
		return UIError{
			Type:             ErrorTypePermission,
			ClientStatusCode: statusCode,
			UserMessage:      userErrorMessages[ErrorTypePermission],
		}
	case http.StatusBadRequest: // 400
		if apiErrorCode == "resource_not_found" || apiErrorCode == "authentication_failure" {
			return UIError{
				Type:             ErrorTypeAuthentication,
				ClientStatusCode: http.StatusUnauthorized, // Normalize to 401 for UI
				UserMessage:      userErrorMessages[ErrorTypeAuthentication],
			}
		}
		return UIError{
			Type:             ErrorTypeValidation,
			ClientStatusCode: statusCode,
			UserMessage:      userErrorMessages[ErrorTypeValidation],
		}
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout: // 5xx
		return UIError{
			Type:             ErrorTypeSystem,
			ClientStatusCode: statusCode,
			UserMessage:      userErrorMessages[ErrorTypeSystem],
		}
	default:
		// For unknown status codes or client-side errors, default to system error
		return UIError{
			Type:             ErrorTypeSystem,
			ClientStatusCode: http.StatusInternalServerError,
			UserMessage:      userErrorMessages[ErrorTypeSystem],
		}
	}
}
