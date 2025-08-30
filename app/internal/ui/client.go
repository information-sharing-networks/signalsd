package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// getErrorMessage extracts error message from API response or provides fallback
func (c *Client) getErrorMessage(resp *http.Response, fallback string) string {
	var errorResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errorResp); err == nil && errorResp.Message != "" {
		return errorResp.Message
	}
	return fallback
}

// Client handles communication with signalsd API
type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// SearchSignals use the signalsd API to search for signals
func (c *Client) SearchSignals(accessToken string, params SignalSearchParams, visibility string) (*SignalSearchResponse, error) {
	// Build URL based on ISN visibility (public ISNs use /api/public/, private use /api/)
	var url string
	if visibility == "public" {
		url = fmt.Sprintf("%s/api/public/isn/%s/signal_types/%s/v%s/signals/search",
			c.baseURL, params.ISNSlug, params.SignalTypeSlug, params.SemVer)
	} else {
		url = fmt.Sprintf("%s/api/isn/%s/signal_types/%s/v%s/signals/search",
			c.baseURL, params.ISNSlug, params.SignalTypeSlug, params.SemVer)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	if params.StartDate != "" {
		q.Add("start_date", params.StartDate)
	}
	if params.EndDate != "" {
		q.Add("end_date", params.EndDate)
	}
	if params.AccountID != "" {
		q.Add("account_id", params.AccountID)
	}
	if params.SignalID != "" {
		q.Add("signal_id", params.SignalID)
	}
	if params.LocalRef != "" {
		q.Add("local_ref", params.LocalRef)
	}
	if params.IncludeWithdrawn {
		q.Add("include_withdrawn", "true")
	}
	if params.IncludeCorrelated {
		q.Add("include_correlated", "true")
	}
	if params.IncludePreviousVersions {
		q.Add("include_previous_versions", "true")
	}
	req.URL.RawQuery = q.Encode()

	// Set authorization header for private ISNs (public ISNs don't need auth)
	if visibility == "private" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Check for timeout to provide more specific message
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, fmt.Errorf("request timed out - please check your connection and try again")
		}
		return nil, fmt.Errorf("network error - please check your connection and try again")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		message := c.getErrorMessage(resp, "Search request failed")
		return nil, fmt.Errorf("%s", message)
	}

	var searchResp SignalSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &searchResp, nil
}

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
		return fmt.Errorf("failed to marshal registration request: %w", err)
	}

	url := fmt.Sprintf("%s/api/auth/register", c.baseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Check for timeout to provide more specific message
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return fmt.Errorf("request timed out - please check your connection and try again")
		}
		return fmt.Errorf("network error - please check your connection and try again")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		message := c.getErrorMessage(resp, "Registration failed")
		return fmt.Errorf("%s", message)
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
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Check for timeout to provide more specific message
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, fmt.Errorf("request timed out - please check your connection and try again")
		}
		return nil, fmt.Errorf("network error - please check your connection and try again")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		message := c.getErrorMessage(resp, "User lookup failed")
		return nil, fmt.Errorf("%s", message)
	}

	// Parse the single user response
	var user UserLookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	return &user, nil
}

// AddAccountToIsn adds an account to an ISN with the specified permission
func (c *Client) AddAccountToIsn(accessToken, isnSlug, accountEmail, permission string) error {
	user, err := c.LookupUserByEmail(accessToken, accountEmail)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/api/isn/%s/accounts/%s", c.baseURL, isnSlug, user.AccountID)

	requestBody := map[string]string{
		"permission": permission,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Check for timeout to provide more specific message
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return fmt.Errorf("request timed out - please check your connection and try again")
		}
		return fmt.Errorf("network error - please check your connection and try again")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		message := c.getErrorMessage(resp, "Failed to add account to ISN")
		return fmt.Errorf("%s", message)
	}

	return nil
}
