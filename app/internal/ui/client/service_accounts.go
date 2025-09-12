package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
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

type ReissueServiceAccountRequest struct {
	ClientOrganization string `json:"client_organization" example:"example org"`
	ClientContactEmail string `json:"client_contact_email" example:"example@example.com"`
}

type ReissueServiceAccountResponse struct {
	ClientID  string    `json:"client_id" example:"sa_example-org_k7j2m9x1"`
	AccountID uuid.UUID `json:"account_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	SetupURL  string    `json:"setup_url" example:"https://api.example.com/api/auth/service-accounts/setup/550e8400-e29b-41d4-a716-446655440000"`
	ExpiresAt time.Time `json:"expires_at" example:"2024-12-25T10:30:00Z"`
	ExpiresIn int       `json:"expires_in" example:"172800"`
}

func (c *Client) CreateServiceAccount(accessToken string, req CreateServiceAccountRequest) (*CreateServiceAccountResponse, error) {
	url := fmt.Sprintf("%s/api/auth/service-accounts/register", c.baseURL)

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

	res, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return nil, NewClientApiError(res)
	}

	var createServiceAccountResponse CreateServiceAccountResponse
	if err := json.NewDecoder(res.Body).Decode(&createServiceAccountResponse); err != nil {
		return nil, NewClientInternalError(err, "decoding create service account response")
	}

	return &createServiceAccountResponse, nil
}

// GetServiceAccounts returns a list of service accounts for use in a dropdown component
func (c *Client) GetServiceAccounts(accessToken string) ([]types.ServiceAccountOption, error) {
	url := fmt.Sprintf("%s/api/admin/service-accounts", c.baseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating service account request")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var serviceAccounts []types.ServiceAccountOption
	if err := json.NewDecoder(res.Body).Decode(&serviceAccounts); err != nil {
		return nil, NewClientInternalError(err, "decoding service accounts response")
	}

	return serviceAccounts, nil
}

// ReissueServiceAccount reissues credentials for an existing service account
func (c *Client) ReissueServiceAccount(accessToken string, req ReissueServiceAccountRequest) (*ReissueServiceAccountResponse, error) {
	url := fmt.Sprintf("%s/api/auth/service-accounts/reissue-credentials", c.baseURL)

	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, NewClientInternalError(err, "marshaling reissue service account request")
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, NewClientInternalError(err, "creating reissue service account request")
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

	var reissueResponse ReissueServiceAccountResponse
	if err := json.NewDecoder(res.Body).Decode(&reissueResponse); err != nil {
		return nil, NewClientInternalError(err, "decoding reissue service account response")
	}

	return &reissueResponse, nil
}
