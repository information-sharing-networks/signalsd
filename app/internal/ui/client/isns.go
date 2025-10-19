package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

type CreateIsnRequest struct {
	Title      string `json:"title" example:"Sample ISN @example.org"`
	Detail     string `json:"detail" example:"Sample ISN description"`
	IsInUse    bool   `json:"is_in_use" example:"true"`
	Visibility string `json:"visibility" example:"private" enums:"public,private"`
}

type CreateIsnResponse struct {
	ID          string `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	Slug        string `json:"slug" example:"sample-isn--example-org"`
	ResourceURL string `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn--example-org"`
}

// UpdateIsnStatusRequest represents the request body for updating ISN status
type UpdateIsnStatusRequest struct {
	IsInUse bool `json:"is_in_use"`
}

// CreateIsn creates a new ISN using the signalsd API
func (c *Client) CreateIsn(accessToken string, req CreateIsnRequest) (*CreateIsnResponse, error) {

	url := fmt.Sprintf("%s/api/isn", c.baseURL)

	jsonData, err := json.Marshal(req)

	if err != nil {
		return nil, NewClientInternalError(err, "marshaling create isn request")
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, NewClientInternalError(err, "create isn request")
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

	var createIsnResp CreateIsnResponse
	if err := json.NewDecoder(res.Body).Decode(&createIsnResp); err != nil {

		return nil, NewClientInternalError(err, "decoding create isn response")
	}
	return &createIsnResp, nil
}

// GetIsns fetches all ISNs, optionally including inactive ones
func (c *Client) GetIsns(accessToken string, includeInactive bool) ([]types.IsnOption, error) {
	url := fmt.Sprintf("%s/api/isn", c.baseURL)

	if includeInactive {
		url += "?include_inactive=true"
	}

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating get isns request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var isns []types.IsnOption
	if err := json.NewDecoder(res.Body).Decode(&isns); err != nil {
		return nil, NewClientInternalError(err, "decoding get isns response")
	}

	return isns, nil
}

// UpdateIsnStatus updates the is_in_use status of an ISN
func (c *Client) UpdateIsnStatus(accessToken, isnSlug string, isInUse bool) error {
	url := fmt.Sprintf("%s/api/isn/%s", c.baseURL, isnSlug)

	req := UpdateIsnStatusRequest{
		IsInUse: isInUse,
	}

	jsonData, err := json.Marshal(req)
	if err != nil {
		return NewClientInternalError(err, "marshaling update isn status request")
	}

	httpReq, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return NewClientInternalError(err, "creating update isn status request")
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
