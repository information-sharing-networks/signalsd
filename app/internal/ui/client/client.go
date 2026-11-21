package client

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// Client handles communication with signalsd API
type Client struct {
	baseURL        string
	httpClient     *http.Client
	downloadClient *http.Client
}

func NewClient(baseURL string) *Client {
	downloadTransport := http.DefaultTransport.(*http.Transport).Clone()
	downloadTransport.ResponseHeaderTimeout = 30 * time.Second

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		// Document downloads can take up to DOCUMENT_TRANSFER_TIMEOUT, so no overall Timeout is set here;
		// the transfer is bounded by the incoming request context (ExtendRequestTimeout).
		downloadClient: &http.Client{
			Transport: downloadTransport,
		},
	}
}

// setRequestID forwards the incoming request ID from ctx to the outgoing request header
// so that the API logs the same request ID as the UI.
func setRequestID(req *http.Request, ctx context.Context) {
	if id := middleware.GetReqID(ctx); id != "" {
		req.Header.Set(middleware.RequestIDHeader, id)
	}
}
