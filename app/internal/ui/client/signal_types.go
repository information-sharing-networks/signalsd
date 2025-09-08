package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// CreateSignalTypeRequest represents the request body for creating a signal type
type CreateSignalTypeRequest struct {
	SchemaURL string  `json:"schema_url"`
	Title     string  `json:"title"`
	BumpType  string  `json:"bump_type"`
	ReadmeURL *string `json:"readme_url"`
	Detail    *string `json:"detail"`
}

// CreateSignalTypeResponse represents the response from creating a signal type
type CreateSignalTypeResponse struct {
	Slug        string `json:"slug"`
	SemVer      string `json:"sem_ver"`
	ResourceURL string `json:"resource_url"`
}

// CreateSignalType creates a new signal type using the signalsd API
func (c *Client) CreateSignalType(accessToken, isnSlug string, req CreateSignalTypeRequest) (*CreateSignalTypeResponse, error) {
	url := fmt.Sprintf("%s/api/isn/%s/signal_types", c.baseURL, isnSlug)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, NewClientInternalError(err, "marshaling create signal type request")
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, NewClientInternalError(err, "creating signal type request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return nil, NewClientApiError(res)
	}

	var createResp CreateSignalTypeResponse
	if err := json.NewDecoder(res.Body).Decode(&createResp); err != nil {
		return nil, NewClientInternalError(err, "decoding create signal type response")
	}

	return &createResp, nil
}
