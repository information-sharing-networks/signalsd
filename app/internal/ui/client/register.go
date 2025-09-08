package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// RegisterUser creates a new user account using the signalsd API
func (c *Client) RegisterUser(email, password string) error {
	registerReq := struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}{
		Email:    email,
		Password: password,
	}

	jsonData, err := json.Marshal(registerReq)
	if err != nil {
		return NewClientInternalError(err, "marshaling registration request")
	}

	url := fmt.Sprintf("%s/api/auth/register", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return NewClientInternalError(err, "creating registration request")
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusCreated {
		return NewClientApiError(res)
	}

	return nil
}

type UserLookupResponse struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
}

// LookupUserByEmail looks up a user by email address using the admin endpoint
// Note: This requires admin/owner permissions
func (c *Client) LookupUserByEmail(accessToken, email string) (*UserLookupResponse, error) {
	// Use the combined admin users endpoint with email query parameter
	url := fmt.Sprintf("%s/api/admin/users?email=%s", c.baseURL, email)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating user lookup request")
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, NewClientConnectionError(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusNotFound {
			return nil, &ClientError{
				StatusCode:  http.StatusNotFound,
				UserMessage: "Email address not found.",
				LogMessage:  fmt.Sprintf("user lookup failed: email %s not found", email),
			}
		}
		return nil, NewClientApiError(res)
	}

	// Parse the single user response
	var user UserLookupResponse
	if err := json.NewDecoder(res.Body).Decode(&user); err != nil {
		return nil, NewClientInternalError(err, "decoding user lookup response")
	}

	return &user, nil
}
