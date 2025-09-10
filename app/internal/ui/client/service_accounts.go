package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type CreateServiceAccountRequest struct {
	ClientOrganization string `json:"client_organization" example:"example org"`
	ClientContactEmail string `json:"client_contact_email" example:"example@example.com"`
}

type CreateServiceAccountResponse struct {
	ClientID  string    `json:"client_id" example:"sa_example-org_k7j2m9x1"`
	AccountID uuid.UUID `json:"account_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	SetupURL  string    `json:"setup_url" example:"https://api.example.com/api/auth/service-accounts/setup/550e8400-e29b-41d4-a716-446655440000"`
	ExpiresAt time.Time `json:"expires_at" example:"2024-12-25T10:30:00Z"`
	ExpiresIn int       `json:"expires_in" example:"172800"`
}

func (c *Client) CreateServiceAccount(accessToken string, req CreateServiceAccountRequest) (*CreateServiceAccountResponse, error) {
	url := fmt.Sprintf("%s/api/auth/register/service-accounts", c.baseURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, NewClientInternalError(err, "marshaling create service json ")
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, NewClientInternalError(err, "creating service account request")
	}

	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	httpReq.Header.Set("Content-type", "application/json")

	fmt.Printf("Debug !!! about to present request to api, accessToken %v", accessToken)
	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		fmt.Printf("Debug !! got connection error")
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		fmt.Printf("Debug !! got wrong status code %v", res.StatusCode)
		return nil, NewClientApiError(res)
	}

	var createServiceAccountResponse CreateServiceAccountResponse
	if err := json.NewDecoder(res.Body).Decode(&createServiceAccountResponse); err != nil {
		return nil, NewClientInternalError(err, "decoding create service account response")
	}

	return &createServiceAccountResponse, nil
}
