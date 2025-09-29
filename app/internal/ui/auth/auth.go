package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// AuthService provides authentication and authorization services for the UI
type AuthService struct {
	apiBaseURL  string
	httpClient  *http.Client
	environment string
}

// NewAuthService creates a new UI authentication service
func NewAuthService(apiBaseURL string, environment string) *AuthService {
	return &AuthService{
		apiBaseURL: apiBaseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		environment: environment,
	}
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AccessTokenStatus represents the status of an access token used in a UI request
type AccessTokenStatus int

const (
	TokenMissing AccessTokenStatus = iota // initial state immediately after login
	TokenInvalid
	TokenExpired
	TokenValid
)

var tokenStatusNames = []string{"TokenMissing", "TokenInvalid", "TokenExpired", "TokenValid"}

func (t AccessTokenStatus) String() string {
	if t < 0 || int(t) >= len(tokenStatusNames) {
		return fmt.Sprintf("TokenStatus(%d)", int(t))
	}
	return tokenStatusNames[t]
}

// RefreshToken uses the signalsd backend API to refresh an access token using the supplied refresh token.
// Returns a new refresh token cookie and access token.
func (a *AuthService) RefreshToken(refreshTokenCookie *http.Cookie) (*types.AccessTokenDetails, *http.Cookie, error) {
	url := fmt.Sprintf("%s/oauth/token?grant_type=refresh_token", a.apiBaseURL)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// add the refresh token cookie from the browser's request to the API request
	req.AddCookie(refreshTokenCookie)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var errorResp types.ErrorResponse
		if err := json.NewDecoder(res.Body).Decode(&errorResp); err != nil {
			return nil, nil, fmt.Errorf("token refresh failed with status %d", res.StatusCode)
		}
		return nil, nil, fmt.Errorf("token refresh failed: %s", errorResp.Message)
	}

	var accessTokenDetails types.AccessTokenDetails
	if err := json.NewDecoder(res.Body).Decode(&accessTokenDetails); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Extract the new refresh token cookie from the API response
	cookies := res.Cookies()
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

	return &accessTokenDetails, newRefreshTokenCookie, nil
}

// CheckAccessTokenStatus checks the status of an access token from context
func (a *AuthService) CheckAccessTokenStatus(accessTokenDetails *types.AccessTokenDetails) AccessTokenStatus {

	if accessTokenDetails == nil {

		return TokenMissing
	}

	if accessTokenDetails.AccessToken == "" {
		return TokenInvalid
	}

	// Parse token without validation to check expiry
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := &jwt.RegisteredClaims{}

	_, _, err := parser.ParseUnverified(accessTokenDetails.AccessToken, claims)
	if err != nil {
		return TokenInvalid
	}

	// Check if token is expired (in normal operations the browser will remove the expired cookie and this code will not be reached)
	if claims.ExpiresAt.Before(time.Now()) {
		return TokenExpired
	}

	return TokenValid
}

// SetAuthCookies sets the authentication-related cookies in the UI HTTP response after authentication
//
// The browser needs to maintain authentication state via cookies so that any signalsd instance can authenticate the user, regardless of which instance handles each request.
//
// The following cookies are set:
//   - refresh token cookie (forwarded from signalsd API)
//   - a cookie containing the access token and account information returned from the access token provided by the server,
func (a *AuthService) SetAuthCookies(w http.ResponseWriter, accessTokenDetails *types.AccessTokenDetails, refreshTokenCookie *http.Cookie) error {
	isProd := a.environment == "prod"

	accessTokenDetailsJSON, err := json.Marshal(accessTokenDetails)
	if err != nil {
		return fmt.Errorf("failed to marshal access token details: %w", err)
	}

	// Base64 encode to avoid cookie encoding issues
	encodedAccessTokenDetails := base64.StdEncoding.EncodeToString(accessTokenDetailsJSON)

	http.SetCookie(w, &http.Cookie{
		Name:     config.AccessTokenDetailsCookieName,
		Value:    encodedAccessTokenDetails,
		Path:     "/",
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   accessTokenDetails.ExpiresIn,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     config.RefreshTokenCookieName,
		Value:    refreshTokenCookie.Value,
		Path:     "/",
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   refreshTokenCookie.MaxAge,
	})

	return nil
}

// ClearAuthCookies clears all authentication-related cookies
func (a *AuthService) ClearAuthCookies(w http.ResponseWriter) {
	isProd := a.environment == "prod"

	http.SetCookie(w, &http.Cookie{
		Name:     config.AccessTokenDetailsCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteStrictMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     signalsd.RefreshTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteStrictMode,
	})

}

// SetLoginEventCookie sets a cookie to indicate a login event.
// This cookie is used by the RequireAuth middleware to determine if the auth cookies need to be added to the context
func (a *AuthService) SetLoginEventCookie(w http.ResponseWriter) {
	isProd := a.environment == "prod"
	http.SetCookie(w, &http.Cookie{
		Name:     config.LoginEventCookieName,
		Value:    "true",
		Path:     "/",
		MaxAge:   10,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteStrictMode,
	})
}

func (a *AuthService) ClearLoginEventCookie(w http.ResponseWriter) {
	isProd := a.environment == "prod"
	http.SetCookie(w, &http.Cookie{
		Name:     config.LoginEventCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteStrictMode,
	})
}
