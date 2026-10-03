package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// SignalSearchParams represents search parameters for signals
type SignalSearchParams struct {
	IsnSlug                 string
	SignalTypeSlug          string
	SemVer                  string
	StartDate               string
	EndDate                 string
	AccountID               string
	SignalID                string
	LocalRef                string
	IncludeWithdrawn        bool
	IncludeCorrelated       bool
	IncludePreviousVersions bool
}

// SearchSignal represents a signal in search results
type SearchSignal struct {
	AccountID            string          `json:"account_id"`
	AccountType          string          `json:"account_type"`
	Email                string          `json:"email,omitempty"`
	SignalID             string          `json:"signal_id"`
	LocalRef             string          `json:"local_ref"`
	SignalTypeSlug       string          `json:"signal_type_slug"`
	SemVer               string          `json:"sem_ver"`
	ContentKind          string          `json:"content_kind"`
	SignalCreatedAt      string          `json:"signal_created_at"`
	SignalVersionID      string          `json:"signal_version_id"`
	VersionNumber        int32           `json:"version_number"`
	VersionCreatedAt     string          `json:"version_created_at"`
	CorrelatedToSignalID string          `json:"correlated_to_signal_id"`
	IsWithdrawn          bool            `json:"is_withdrawn"`
	Content              json.RawMessage `json:"content"`
}

// DocumentMetadata is the content of a document signal in search results (the document itself is downloaded separately)
type DocumentMetadata struct {
	Name      string `json:"name"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// PreviousSignalVersion represents a previous version of a signal
type PreviousSignalVersion struct {
	SignalVersionID string          `json:"signal_version_id"`
	CreatedAt       string          `json:"created_at"`
	VersionNumber   int32           `json:"version_number"`
	Content         json.RawMessage `json:"content"`
}

// SearchSignalWithCorrelationsAndVersions represents a signal with optional correlations and versions
type SearchSignalWithCorrelationsAndVersions struct {
	SearchSignal
	CorrelatedSignals      []SearchSignal          `json:"correlated_signals,omitempty"`
	PreviousSignalVersions []PreviousSignalVersion `json:"previous_signal_versions,omitempty"`
}

// SignalSearchResponse represents the response from signal search (direct array)
type SignalSearchResponse []SearchSignalWithCorrelationsAndVersions

// SearchSignals use the signalsd API to search for signals
func (c *Client) SearchSignals(ctx context.Context, accessToken string, params SignalSearchParams, visibility string) (*SignalSearchResponse, error) {
	// Build URL based on ISN visibility (public ISNs use /api/public/, private use /api/)
	var url string
	if visibility == "public" {
		url = fmt.Sprintf("%s/api/public/isn/%s/signal-types/%s/v%s/signals/search",
			c.baseURL, params.IsnSlug, params.SignalTypeSlug, params.SemVer)
	} else {
		url = fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/signals/search",
			c.baseURL, params.IsnSlug, params.SignalTypeSlug, params.SemVer)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating search request")
	}

	// Add query parameters
	q := req.URL.Query()
	if params.StartDate != "" {
		q.Add("start_date", params.StartDate)
	}
	if params.EndDate != "" {
		q.Add("end_date", params.EndDate)
	}
	if params.AccountID != "" {
		q.Add("account_id", params.AccountID)
	}
	if params.SignalID != "" {
		q.Add("signal_id", params.SignalID)
	}
	if params.LocalRef != "" {
		q.Add("local_ref", params.LocalRef)
	}
	if params.IncludeWithdrawn {
		q.Add("include_withdrawn", "true")
	}
	if params.IncludeCorrelated {
		q.Add("include_correlated", "true")
	}
	if params.IncludePreviousVersions {
		q.Add("include_previous_versions", "true")
	}
	req.URL.RawQuery = q.Encode()

	// Set authorization header for private ISNs (public ISNs don't need auth)
	if visibility == "private" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	}
	setRequestID(req, ctx)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var searchResp SignalSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&searchResp); err != nil {
		return nil, NewClientInternalError(err, "decoding search response")
	}

	return &searchResp, nil
}

// DownloadDocumentParams identifies the document signal version to download
type DownloadDocumentParams struct {
	IsnSlug          string
	SignalTypeSlug   string
	SemVer           string
	SignalID         string
	VersionNumber    string
	IncludeWithdrawn bool
}

// DownloadDocument requests a document from the signalsd API.
// The response is returned unread so the caller can stream the document to the browser - the caller must close the response body.
func (c *Client) DownloadDocument(ctx context.Context, accessToken string, params DownloadDocumentParams) (*http.Response, error) {
	url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/signals/%s/content",
		c.baseURL, params.IsnSlug, params.SignalTypeSlug, params.SemVer, params.SignalID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating download document request")
	}

	q := req.URL.Query()
	if params.VersionNumber != "" {
		q.Add("version", params.VersionNumber)
	}
	if params.IncludeWithdrawn {
		q.Add("include_withdrawn", "true")
	}
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	setRequestID(req, ctx)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}

	if res.StatusCode != http.StatusOK {
		defer res.Body.Close()
		return nil, NewClientApiError(res)
	}

	return res, nil
}
