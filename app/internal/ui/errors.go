package ui

import (
	"encoding/json"
	"errors"
	"fmt"
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
		return true
	}

	// Check for specific network error types
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	// Check for DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Check for URL errors (which often wrap network errors)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
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

// NewUIError converts errors into user-friendly UIError instances
func NewUIError(statusCode int, err error) UIError {
	// Handle network/connection errors
	if err != nil {
		if isNetworkError(err) {
			return UIError{
				Type:    ErrorTypeNetwork,
				Message: userErrorMessages[ErrorTypeNetwork],
			}
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
	case http.StatusBadRequest: // 400 - prefer the error message from the server for bad request errors as these should explain the problem in plain English
		return UIError{
			Type:    ErrorTypeValidation,
			Message: err.Error(),
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

// CategorizeErrorFromResponse creates a UIError from a signalsd http errorResponse
func CategorizeErrorFromResponse(resp *http.Response, fallbackMessage string) UIError {
	var message string

	// Try to extract detailed error message from response
	if resp != nil && resp.Body != nil {
		var errorResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil {
			if errorResp.Message != "" {
				message = errorResp.Message
			}
		}
	}

	// Fall back to generic message if no specific message found
	if message == "" {
		message = fallbackMessage
	}

	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}

	return NewUIError(statusCode, fmt.Errorf("%s", message))
}
