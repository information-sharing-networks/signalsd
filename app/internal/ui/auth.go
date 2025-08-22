package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
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

// ISN represents an Information Sharing Network
type ISN struct {
	ID         uuid.UUID `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Title      string    `json:"title"`
	Slug       string    `json:"slug"`
	Detail     string    `json:"detail"`
	IsInUse    bool      `json:"is_in_use"`
	Visibility string    `json:"visibility"`
}

// SignalType represents a signal type definition
type SignalType struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Slug      string    `json:"slug"`
	SchemaURL string    `json:"schema_url"`
	ReadmeURL string    `json:"readme_url"`
	Title     string    `json:"title"`
	Detail    string    `json:"detail"`
	SemVer    string    `json:"sem_ver"`
	IsInUse   bool      `json:"is_in_use"`
}

// SignalSearchParams represents search parameters for signals
type SignalSearchParams struct {
	ISNSlug                 string
	SignalTypeSlug          string
	SemVer                  string
	IsPublic                bool
	StartDate               string
	EndDate                 string
	AccountID               string
	SignalID                string
	LocalRef                string
	IncludeWithdrawn        bool
	IncludeCorrelated       bool
	IncludePreviousVersions bool
}

// SearchSignal represents a signal in search results
type SearchSignal struct {
	AccountID            string          `json:"account_id"`
	AccountType          string          `json:"account_type"`
	Email                string          `json:"email,omitempty"`
	SignalID             string          `json:"signal_id"`
	LocalRef             string          `json:"local_ref"`
	SignalCreatedAt      string          `json:"signal_created_at"`
	SignalVersionID      string          `json:"signal_version_id"`
	VersionNumber        int32           `json:"version_number"`
	VersionCreatedAt     string          `json:"version_created_at"`
	CorrelatedToSignalID string          `json:"correlated_to_signal_id"`
	IsWithdrawn          bool            `json:"is_withdrawn"`
	Content              json.RawMessage `json:"content"`
}

// PreviousSignalVersion represents a previous version of a signal
type PreviousSignalVersion struct {
	SignalVersionID string          `json:"signal_version_id"`
	CreatedAt       string          `json:"created_at"`
	VersionNumber   int32           `json:"version_number"`
	Content         json.RawMessage `json:"content"`
}

// SearchSignalWithCorrelationsAndVersions represents a signal with optional correlations and versions
type SearchSignalWithCorrelationsAndVersions struct {
	SearchSignal
	CorrelatedSignals      []SearchSignal          `json:"correlated_signals,omitempty"`
	PreviousSignalVersions []PreviousSignalVersion `json:"previous_signal_versions,omitempty"`
}

// SignalSearchResponse represents the response from signal search (direct array)
type SignalSearchResponse []SearchSignalWithCorrelationsAndVersions

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

// RefreshToken attempts to refresh an access token using the refresh token
func (a *AuthService) RefreshToken(currentAccessToken string, refreshTokenCookie *http.Cookie) (*LoginResponse, error) {
	url := fmt.Sprintf("%s/oauth/token?grant_type=refresh_token", a.apiBaseURL)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set the current access token as bearer token (required for CSRF protection)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", currentAccessToken))
	req.Header.Set("Content-Type", "application/json")

	// Add the refresh token cookie
	req.AddCookie(refreshTokenCookie)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return nil, fmt.Errorf("token refresh failed with status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("token refresh failed: %s", errorResp.Message)
	}

	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &loginResp, nil
}

// GetISNs retrieves the list of ISNs from the signalsd API
func (a *AuthService) GetISNs(accessToken string) ([]ISN, error) {
	url := fmt.Sprintf("%s/api/isn", a.apiBaseURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get ISNs: status %d", resp.StatusCode)
	}

	var isns []ISN
	if err := json.NewDecoder(resp.Body).Decode(&isns); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return isns, nil
}

// GetSignalTypes retrieves signal types for a specific ISN
func (a *AuthService) GetSignalTypes(accessToken, isnSlug string) ([]SignalType, error) {
	url := fmt.Sprintf("%s/api/isn/%s/signal_types", a.apiBaseURL, isnSlug)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get signal types: status %d", resp.StatusCode)
	}

	var signalTypes []SignalType
	if err := json.NewDecoder(resp.Body).Decode(&signalTypes); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return signalTypes, nil
}

// SearchSignals searches for signals using the signalsd API
func (a *AuthService) SearchSignals(accessToken string, params SignalSearchParams) (*SignalSearchResponse, error) {
	// Build URL based on whether it's a public or private ISN search
	var url string
	if params.IsPublic {
		url = fmt.Sprintf("%s/api/public/isn/%s/signal_types/%s/v%s/signals/search",
			a.apiBaseURL, params.ISNSlug, params.SignalTypeSlug, params.SemVer)
	} else {
		url = fmt.Sprintf("%s/api/isn/%s/signal_types/%s/v%s/signals/search",
			a.apiBaseURL, params.ISNSlug, params.SignalTypeSlug, params.SemVer)
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

	// Set authorization header for private ISNs
	if !params.IsPublic {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return nil, fmt.Errorf("search failed with status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("search failed: %s", errorResp.Message)
	}

	var searchResp SignalSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &searchResp, nil
}
