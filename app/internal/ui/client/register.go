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

type ServiceAccountLookupResponse struct {
	AccountID string `json:"account_id"`
	ClientID  string `json:"client_id"`
}

// LookupServiceAccountByClientID looks up a service account by client ID using the admin endpoint
// Note: This requires admin/owner permissions
func (c *Client) LookupServiceAccountByClientID(accessToken, clientID string) (*ServiceAccountLookupResponse, error) {
	// Use the admin service accounts endpoint to get all service accounts, then filter by client_id
	// This is similar to how the user lookup works but for service accounts

	url := fmt.Sprintf("%s/api/admin/service-accounts?client_id=%s", c.baseURL, clientID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, NewClientInternalError(err, "creating service account lookup request")
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
				UserMessage: "Client ID not found.",
				LogMessage:  fmt.Sprintf("service account lookup failed: client_id %s not found", clientID),
			}
		}
		return nil, NewClientApiError(res)
	}

	// Parse the service accounts list response
	var serviceAccount ServiceAccountLookupResponse

	if err := json.NewDecoder(res.Body).Decode(&serviceAccount); err != nil {
		return nil, NewClientInternalError(err, "decoding service accounts response")
	}

	return &serviceAccount, nil

}
