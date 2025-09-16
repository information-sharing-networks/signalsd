package client

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

type GeneratePasswordResetLinkResponse struct {
	UserEmail string `json:"user_email" example:"user@example.com"`
	AccountID string `json:"account_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	ResetURL  string `json:"reset_url" example:"https://api.example.com/api/auth/password-reset/0ce71234-34d5-4fb5-beb8-ad50d8b40c7d"`
	ExpiresAt string `json:"expires_at" example:"2024-12-25T10:30:00Z"`
	ExpiresIn int    `json:"expires_in" example:"1800"`
}

// GetUserOptionsList returns a list of users for use in a dropdown component
func (c *Client) GetUserOptionsList(accessToken string) ([]types.UserOption, error) {

	url := fmt.Sprintf("%s/api/admin/users", c.baseURL)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating get users request")
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

	var users []types.UserOption
	if err := json.NewDecoder(res.Body).Decode(&users); err != nil {
		return nil, NewClientInternalError(err, "decoding get users response")
	}

	return users, nil

}

// GeneratePasswordResetLink generates a password reset link for the user associated witht the supplied email
func (c *Client) GeneratePasswordResetLink(accessToken, email string) (*GeneratePasswordResetLinkResponse, error) {

	user, err := c.LookupUserByEmail(accessToken, email)
	if err != nil {
		return nil, NewClientInternalError(err, "looking up user by email")
	}

	url := fmt.Sprintf("%s/api/admin/users/%s/generate-password-reset-link", c.baseURL, user.AccountID)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating generate password reset link request")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, NewClientApiError(res)
	}

	var generatePasswordResetLinkResponse *GeneratePasswordResetLinkResponse
	if err := json.NewDecoder(res.Body).Decode(&generatePasswordResetLinkResponse); err != nil {
		return nil, NewClientInternalError(err, "decoding generate password reset link response")
	}

	return generatePasswordResetLinkResponse, nil
}
