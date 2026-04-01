package client

// these functions call the signalsd API to manage signal types (create, register new schema, add to ISN, update Signal Type status)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// CreateSignalTypeRequest represents the request body for creating a signal type
type CreateSignalTypeRequest struct {
	SchemaURL string `json:"schema_url"`
	Title     string `json:"title"`
	BumpType  string `json:"bump_type"`
	ReadmeURL string `json:"readme_url"`
	Detail    string `json:"detail"`
}

// NewSignalTypeResponse represents the response from creating a signal type
type NewSignalTypeResponse struct {
	Slug   string `json:"slug"`
	SemVer string `json:"sem_ver"`
}

// CreateSignalType creates a new signal type globally using the signalsd API
func (c *Client) CreateSignalType(accessToken string, req CreateSignalTypeRequest) (*NewSignalTypeResponse, error) {
	url := fmt.Sprintf("%s/api/admin/signal-types", c.baseURL)

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

// CreateSignalTypeRequest represents the request body for creating a signal type
type RegisterNewSignalTypeSchemaRequest struct {
	SchemaURL string `json:"schema_url"`
	Slug      string `json:"slug"`
	BumpType  string `json:"bump_type"`
	ReadmeURL string `json:"readme_url"`
	Detail    string `json:"detail"`
}

// RegisterNewSignalTypeSchema creates a new version of a signal type globally using the signalsd API
func (c *Client) RegisterNewSignalTypeSchema(accessToken string, req RegisterNewSignalTypeSchemaRequest) (*NewSignalTypeResponse, error) {
	url := fmt.Sprintf("%s/api/admin/signal-types/%s/schemas", c.baseURL, req.Slug)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, NewClientInternalError(err, "marshaling register new schema request")
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, NewClientInternalError(err, "creating register new schema request")
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
		return nil, NewClientInternalError(err, "decoding register new schema response")
	}

	return &createResp, nil
}

// UpdateSignalTypeStatusRequest represents the request body for updating signal type status
type UpdateSignalTypeStatusRequest struct {
	IsInUse bool `json:"is_in_use"`
}

// UpdateIsnSignalTypeStatus updates the is_in_use status of a signal type for a specific ISN
func (c *Client) UpdateIsnSignalTypeStatus(accessToken, isnSlug, signalTypeSlug, semVer string, isInUse bool) error {
	url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s", c.baseURL, isnSlug, signalTypeSlug, semVer)

	req := UpdateSignalTypeStatusRequest{
		IsInUse: isInUse,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return NewClientInternalError(err, "marshaling update isn signal type status request")
	}

	httpReq, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return NewClientInternalError(err, "creating update isn signal type status request")
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

// GetSignalTypes gets all signal types using the signalsd API
func (c *Client) GetSignalTypes(accessToken string, includeInactive bool) ([]SignalTypeDetail, error) {
	url := fmt.Sprintf("%s/api/admin/signal-types", c.baseURL)

	if includeInactive {
		url += "?include_inactive=true"
	}

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating get signal types request")
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

// GetSignalTypesForISN gets signal types available with a specific ISN
func (c *Client) GetSignalTypesForISN(accessToken, isnSlug string, includeInactive bool) ([]SignalTypeDetail, error) {
	url := fmt.Sprintf("%s/api/isn/%s/signal-types", c.baseURL, isnSlug)

	if includeInactive {
		url += "?include_inactive=true"
	}

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating get signal types for ISN request")
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
		return nil, NewClientInternalError(err, "decoding get signal types for ISN response")
	}

	return signalTypes, nil
}

// AddSignalTypeToIsnRequest represents the request body for associating a signal type with an ISN
type AddSignalTypeToIsnRequest struct {
	SignalTypeSlug string `json:"signal_type_slug"`
	SemVer         string `json:"sem_ver"`
}

// AddSignalTypeToIsn adds a signal type to an ISN
func (c *Client) AddSignalTypeToIsn(accessToken, isnSlug string, req AddSignalTypeToIsnRequest) error {
	url := fmt.Sprintf("%s/api/isn/%s/signal-types/add", c.baseURL, isnSlug)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return NewClientInternalError(err, "marshaling add signal type request")
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return NewClientInternalError(err, "creating add signal type request")
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
