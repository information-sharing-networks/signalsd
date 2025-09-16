// the client package is used by the ui-api handlers to call the signalsd API.
// The client handles the error responses from the API, translating them into user-friendly messages and renders the error message to the end user.
// The client also returns the detailed technical error to the caller for logging (see client/errors.go)
package client

import (
	"net/http"
	"time"
)

// Client handles communication with signalsd API
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}
