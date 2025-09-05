package client

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// ClientError represents an error encountered when communicating with the signalsd API
// StatusCode 0 = network/connection error, >0 = HTTP response received
type ClientError struct {
	StatusCode  int    `json:"status_code"`
	UserMessage string `json:"user_message"`
	LogMessage  string `json:"log_message"`
}

func (e *ClientError) Error() string {
	return e.LogMessage
}

// UserError returns the user-friendly message
func (e *ClientError) UserError() string {
	return e.UserMessage
}

// NewConnectionError creates a ClientError for network/connection issues
func NewClientConnectionError(err error) *ClientError {
	return &ClientError{
		StatusCode:  0,
		UserMessage: "Unable to connect. Please check your internet connection and try again.",
		LogMessage:  fmt.Sprintf("network error: %v", err),
	}
}

// NewClientInternalError creates a ClientError for internal errors, supply the error and an explanation of what was being done when the error occurred
func NewClientInternalError(err error, while string) *ClientError {
	return &ClientError{
		StatusCode:  0,
		UserMessage: "An error occurred. Please try again later.",
		LogMessage:  fmt.Sprintf("internal error: %v while %v", err, while),
	}
}

// NewClientApiError creates a ClientError from an HTTP response sent by the signalsd server
func NewClientApiError(res *http.Response) *ClientError {
	var serverErr struct {
		ErrorCode string `json:"error_code"`
		Message   string `json:"message"`
	}

	if res.Body == nil {
		return &ClientError{
			StatusCode:  0,
			UserMessage: "An error occurred. Please try again.",
			LogMessage:  "signaslsd error response body is nil",
		}
	}
	json.NewDecoder(res.Body).Decode(&serverErr)

	var userMsg string
	switch res.StatusCode {
	case http.StatusUnauthorized:
		userMsg = "Login failed. Please check your email and password and try again."
	case http.StatusForbidden:
		userMsg = "You don't have permission to access this resource."
	case http.StatusBadRequest:
		// Use server message for validation errors if available
		if serverErr.Message != "" {
			userMsg = serverErr.Message
		} else {
			userMsg = "Invalid request. Please check your input and try again."
		}
	case http.StatusTooManyRequests:
		userMsg = "Too many requests. Please try again in a few moments."
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
		userMsg = "The service is temporarily unavailable. Please try again later."
	default:
		userMsg = "An error occurred. Please try again."
	}

	logMsg := fmt.Sprintf("signaslsd status %d", res.StatusCode)
	if serverErr.Message != "" {
		logMsg += fmt.Sprintf(" - %s", serverErr.Message)
	}

	return &ClientError{
		StatusCode:  res.StatusCode,
		UserMessage: userMsg,
		LogMessage:  logMsg,
	}
}
