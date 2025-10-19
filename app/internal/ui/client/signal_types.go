package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// CreateSignalTypeRequest represents the request body for creating a signal type
type CreateSignalTypeRequest struct {
	IsnSlug   string `json:"isn_slug"`
	SchemaURL string `json:"schema_url"`
	Title     string `json:"title"`
	BumpType  string `json:"bump_type"`
	ReadmeURL string `json:"readme_url"`
	Detail    string `json:"detail"`
}

// CreateSignalTypeRequest represents the request body for creating a signal type
type RegisterNewSignalTypeSchemaRequest struct {
	IsnSlug   string `json:"isn_slug"`
	SchemaURL string `json:"schema_url"`
	Slug      string `json:"slug"`
	BumpType  string `json:"bump_type"`
	ReadmeURL string `json:"readme_url"`
	Detail    string `json:"detail"`
}

// NewSignalTypeResponse represents the response from creating a signal type
type NewSignalTypeResponse struct {
	Slug   string `json:"slug"`
	SemVer string `json:"sem_ver"`
}

type SignalTypeDetail struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Slug      string `json:"slug"`
	SchemaURL string `json:"schema_url"`
	ReadmeURL string `json:"readme_url"`
	Title     string `json:"title"`
	Detail    string `json:"detail"`
	SemVer    string `json:"sem_ver"`
}

// UpdateSignalTypeStatusRequest represents the request body for updating signal type status
type UpdateSignalTypeStatusRequest struct {
	IsInUse bool `json:"is_in_use"`
}

// CreateSignalType creates a new signal type using the signalsd API
func (c *Client) CreateSignalType(accessToken string, req CreateSignalTypeRequest) (*NewSignalTypeResponse, error) {
	url := fmt.Sprintf("%s/api/isn/%s/signal_types", c.baseURL, req.IsnSlug)

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

	var createResp NewSignalTypeResponse
	if err := json.NewDecoder(res.Body).Decode(&createResp); err != nil {
		return nil, NewClientInternalError(err, "decoding create signal type response")
	}

	return &createResp, nil
}

// CreateSignalType creates a new signal type using the signalsd API
func (c *Client) RegisterNewSignalTypeSchema(accessToken string, req RegisterNewSignalTypeSchemaRequest) (*NewSignalTypeResponse, error) {
	url := fmt.Sprintf("%s/api/isn/%s/signal_types/%s/schemas", c.baseURL, req.IsnSlug, req.Slug)

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

	var createResp NewSignalTypeResponse
	if err := json.NewDecoder(res.Body).Decode(&createResp); err != nil {
		return nil, NewClientInternalError(err, "decoding create signal type response")
	}

	return &createResp, nil
}

// GetSignalTypes gets the signal types for the specified ISN using the signalsd API
func (c *Client) GetSignalTypes(accessToken, isnSlug string, includeInactive bool) ([]SignalTypeDetail, error) {
	url := fmt.Sprintf("%s/api/isn/%s/signal_types", c.baseURL, isnSlug)

	if includeInactive {
		url += "?include_inactive=true"
	}

	httpReq, err := http.NewRequest("GET", url, nil)
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

	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var signalTypes []SignalTypeDetail
	if err := json.NewDecoder(res.Body).Decode(&signalTypes); err != nil {
		return nil, NewClientInternalError(err, "decoding get signal types response")
	}

	return signalTypes, nil
}

// UpdateSignalTypeStatus updates the is_in_use status of a signal type
func (c *Client) UpdateSignalTypeStatus(accessToken, isnSlug, slug, semVer string, isInUse bool) error {
	url := fmt.Sprintf("%s/api/isn/%s/signal_types/%s/v%s", c.baseURL, isnSlug, slug, semVer)

	req := UpdateSignalTypeStatusRequest{
		IsInUse: isInUse,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return NewClientInternalError(err, "marshaling update signal type status request")
	}

	httpReq, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return NewClientInternalError(err, "creating update signal type status request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		return NewClientApiError(res)
	}

	return nil
}
