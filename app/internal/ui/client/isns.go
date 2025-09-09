package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
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
