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
	publicHost string // Public domain name for URL generation (e.g., "signalsd.btddemo.org")
	isHTTPS    bool   // Whether the public domain uses HTTPS
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NewClientWithPublicHost creates a client with public host information for URL generation
func NewClientWithPublicHost(baseURL, publicHost string, isHTTPS bool) *Client {
	return &Client{
		baseURL:    baseURL,
		publicHost: publicHost,
		isHTTPS:    isHTTPS,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}
