package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AuthService handles authentication with the signalsd API
type AuthService struct {
	apiBaseURL string
	httpClient *http.Client
}

// LoginRequest represents the login request payload
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse represents the login response from the API
type LoginResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// ErrorResponse represents an error response from the API
type ErrorResponse struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

func NewAuthService(apiBaseURL string) *AuthService {
	return &AuthService{
		apiBaseURL: apiBaseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// AuthenticateUser authenticates a user with the signalsd API
func (a *AuthService) AuthenticateUser(email, password string) (*LoginResponse, error) {
	loginReq := LoginRequest{
		Email:    email,
		Password: password,
	}

	jsonData, err := json.Marshal(loginReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal login request: %w", err)
	}

	url := fmt.Sprintf("%s/api/auth/login", a.apiBaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return nil, fmt.Errorf("authentication failed with status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("authentication failed: %s", errorResp.Message)
	}

	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &loginResp, nil
}

// ValidateToken validates a JWT token with the signalsd API
func (a *AuthService) ValidateToken(token string) error {
	url := fmt.Sprintf("%s/api/isn", a.apiBaseURL) // Use a simple endpoint to validate token
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("token is invalid or expired")
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("token validation failed with status %d", resp.StatusCode)
	}

	return nil
}
