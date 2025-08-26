package ui

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

type AuthService struct {
	apiBaseURL string
	httpClient *http.Client
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// TokenStatus represents the status of an access token
type TokenStatus int

const (
	TokenMissing TokenStatus = iota
	TokenInvalid
	TokenExpired
	TokenValid
)

func NewAuthService(apiBaseURL string) *AuthService {
	return &AuthService{
		apiBaseURL: apiBaseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// AuthenticateUser authenticates a user with the signalsd API and returns response with refresh token cookie
func (a *AuthService) AuthenticateUser(email, password string) (*LoginResponse, *http.Cookie, error) {
	loginReq := LoginRequest{
		Email:    email,
		Password: password,
	}

	jsonData, err := json.Marshal(loginReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal login request: %w", err)
	}

	url := fmt.Sprintf("%s/api/auth/login", a.apiBaseURL)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp ErrorResponse
		if err := json.Unmarshal(bodyBytes, &errorResp); err != nil {
			return nil, nil, fmt.Errorf("authentication failed with status %d", resp.StatusCode)
		}
		return nil, nil, fmt.Errorf("authentication failed: %s", errorResp.Message)
	}

	var loginResp LoginResponse
	if err := json.Unmarshal(bodyBytes, &loginResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract the refresh token cookie from the API response
	cookies := resp.Cookies()
	var refreshTokenCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == signalsd.RefreshTokenCookieName {
			refreshTokenCookie = cookie
			break
		}
	}

	if refreshTokenCookie == nil {
		return nil, nil, fmt.Errorf("refresh token cookie not found in API response")
	}

	return &loginResp, refreshTokenCookie, nil
}

// RefreshToken attempts to refresh an access token using the refresh token and returns new refresh token cookie
func (a *AuthService) RefreshToken(currentAccessToken *http.Cookie, refreshTokenCookie *http.Cookie) (*LoginResponse, *http.Cookie, error) {
	url := fmt.Sprintf("%s/oauth/token?grant_type=refresh_token", a.apiBaseURL)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set the current access token as bearer token
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", currentAccessToken.Value))
	req.Header.Set("Content-Type", "application/json")

	// add the refresh token cookie from the browser's request to the API request
	req.AddCookie(refreshTokenCookie)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return nil, nil, fmt.Errorf("token refresh failed with status %d", resp.StatusCode)
		}
		return nil, nil, fmt.Errorf("token refresh failed: %s", errorResp.Message)
	}

	var loginResp LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract the new refresh token cookie from the API response
	cookies := resp.Cookies()
	var newRefreshTokenCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == signalsd.RefreshTokenCookieName {
			newRefreshTokenCookie = cookie
			break
		}
	}

	if newRefreshTokenCookie == nil {
		return nil, nil, fmt.Errorf("new refresh token cookie not found in API response")
	}

	return &loginResp, newRefreshTokenCookie, nil
}

// CheckTokenStatus checks the status of an access token from the request cookies
func (a *AuthService) CheckTokenStatus(r *http.Request) TokenStatus {
	accessTokenCookie, err := r.Cookie(accessTokenCookieName)
	if err != nil {
		return TokenMissing
	}

	// Parse token without validation to check expiry
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := &jwt.RegisteredClaims{}

	_, _, err = parser.ParseUnverified(accessTokenCookie.Value, claims)
	if err != nil {
		return TokenInvalid
	}

	if claims.ExpiresAt == nil {
		return TokenInvalid
	}

	// Check if token is expired
	if claims.ExpiresAt.After(time.Now()) {
		return TokenValid
	}

	return TokenExpired
}

// SetAuthCookies sets the authentication-related cookies:
//
//   - refresh token cookie (forwarded directly from signalsd API)
//   - a cookie containing the access token provided by the server,
//   - a cookie containg the isn permissions as JSON.
func (a *AuthService) SetAuthCookies(w http.ResponseWriter, loginResp *LoginResponse, refreshTokenCookie *http.Cookie, environment string) error {
	isProd := environment == "prod"

	// Set refresh token cookie (from API response)
	http.SetCookie(w, refreshTokenCookie)

	// Set access token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     accessTokenCookieName,
		Value:    loginResp.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   isProd,
		MaxAge:   loginResp.ExpiresIn + 60, // JWT expiry + 1 minute buffer
	})

	// Set permissions cookie if permissions exist
	if len(loginResp.Perms) > 0 {
		permsJSON, err := json.Marshal(loginResp.Perms)
		if err != nil {
			return fmt.Errorf("failed to marshal permissions: %w", err)
		}

		// Base64 encode to avoid cookie encoding issues
		encodedPerms := base64.StdEncoding.EncodeToString(permsJSON)
		http.SetCookie(w, &http.Cookie{
			Name:     isnPermsCookieName,
			Value:    encodedPerms,
			Path:     "/",
			HttpOnly: true,
			Secure:   isProd,
			MaxAge:   loginResp.ExpiresIn + 60,
		})
	}

	return nil
}

// ClearAuthCookies clears all authentication-related cookies
func (a *AuthService) ClearAuthCookies(w http.ResponseWriter, environment string) {
	isProd := environment == "prod"

	// Clear access token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     accessTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
	})

	// Clear refresh token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     signalsd.RefreshTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
	})

	// Clear permissions cookie
	http.SetCookie(w, &http.Cookie{
		Name:     isnPermsCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
	})
}
