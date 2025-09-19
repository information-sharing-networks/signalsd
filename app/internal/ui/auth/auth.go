package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// TokenStatus represents the status of an access token used in a UI request
type TokenStatus int

const (
	TokenMissing TokenStatus = iota
	TokenInvalid
	TokenExpired
	TokenValid
)

var tokenStatusNames = []string{"TokenMissing", "TokenInvalid", "TokenExpired", "TokenValid"}

func (t TokenStatus) String() string {
	if t < 0 || int(t) >= len(tokenStatusNames) {
		return fmt.Sprintf("TokenStatus(%d)", int(t))
	}
	return tokenStatusNames[t]
}

// RefreshToken is a UI-specific method that uses the signalsd backend API to refresh an access token using the supplied refresh token.
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

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp types.ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return nil, nil, fmt.Errorf("token refresh failed with status %d", resp.StatusCode)
		}
		return nil, nil, fmt.Errorf("token refresh failed: %s", errorResp.Message)
	}

	var refreshResp types.AccessTokenDetails
	if err := json.NewDecoder(resp.Body).Decode(&refreshResp); err != nil {
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

	return &refreshResp, newRefreshTokenCookie, nil
}

// CheckTokenStatus checks the status of an access token from the request cookies
func (a *AuthService) CheckTokenStatus(r *http.Request) TokenStatus {
	accessTokenCookie, err := r.Cookie(config.AccessTokenCookieName)
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

// SetAuthCookies sets the authentication-related cookies in the UI HTTP response after authentication
//
// The browser needs to maintain authentication state via cookies so that any signalsd instance can authenticate the user, regardless of which instance handles each request.
//
// The following cookies are set:
//   - refresh token cookie (forwarded from signalsd API)
//   - a cookie containing the access token provided by the server,
//   - a cookie containg the isn permissions as JSON.
//   - a cookie containing the account information (ID, type, role) as JSON.
func (a *AuthService) SetAuthCookies(w http.ResponseWriter, accessTokenDetails *types.AccessTokenDetails, refreshTokenCookie *http.Cookie, environment string) error {
	isProd := environment == "prod"

	// Set refresh token cookie (from API response)
	http.SetCookie(w, refreshTokenCookie)

	// Set access token cookie
	http.SetCookie(w, &http.Cookie{
		Name:     config.AccessTokenCookieName,
		Value:    accessTokenDetails.AccessToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   accessTokenDetails.ExpiresIn,
	})

	// Set permissions cookie if permissions exist
	if len(accessTokenDetails.Perms) > 0 {
		permsJSON, err := json.Marshal(accessTokenDetails.Perms)
		if err != nil {
			return fmt.Errorf("failed to marshal permissions: %w", err)
		}

		// Base64 encode to avoid cookie encoding issues
		encodedPerms := base64.StdEncoding.EncodeToString(permsJSON)
		http.SetCookie(w, &http.Cookie{
			Name:     config.IsnPermsCookieName,
			Value:    encodedPerms,
			Path:     "/",
			HttpOnly: true,
			Secure:   isProd,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   accessTokenDetails.ExpiresIn,
		})
	}

	// Set account information cookie (base64 encoded JSON)
	accountInfo := types.AccountInfo{
		AccountID:   accessTokenDetails.AccountID,
		AccountType: accessTokenDetails.AccountType,
		Role:        accessTokenDetails.Role,
	}

	accountInfoJSON, err := json.Marshal(accountInfo)
	if err != nil {
		return fmt.Errorf("failed to marshal account information: %w", err)
	}

	accountInfoBase64 := base64.StdEncoding.EncodeToString(accountInfoJSON)
	http.SetCookie(w, &http.Cookie{
		Name:     config.AccountInfoCookieName,
		Value:    accountInfoBase64,
		Path:     "/",
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   accessTokenDetails.ExpiresIn,
	})

	return nil
}

// ClearAuthCookies clears all authentication-related cookies
func (a *AuthService) ClearAuthCookies(w http.ResponseWriter, environment string) {
	isProd := environment == "prod"

	http.SetCookie(w, &http.Cookie{
		Name:     config.AccessTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     signalsd.RefreshTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     config.IsnPermsCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     config.AccountInfoCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
	})
}

// addAuthDataToContext extracts authentication data from cookies and adds it to the context
// This is used by RequireAuth middleware to make auth data available to handlers via context
// The three cookies extracted are:
//   - access token (required)
//   - account info (required)
//   - ISN perms (optional - ISN permissions will not exist for accounts that have not been granted access to any ISNs)
func (a *AuthService) addAuthDataToContext(ctx context.Context, r *http.Request) (context.Context, error) {
	// add access token to context
	accessTokenCookie, err := r.Cookie(config.AccessTokenCookieName)
	if err != nil {
		return ctx, fmt.Errorf("access token cookie not found: %w", err)
	}

	ctx = ContextWithAccessToken(ctx, accessTokenCookie.Value)

	// add account info to context
	accountInfoCookie, err := r.Cookie(config.AccountInfoCookieName)
	if err != nil {
		return nil, fmt.Errorf("error getting account info cookie not found: %w", err)
	}

	decodedAccountInfo, err := base64.StdEncoding.DecodeString(accountInfoCookie.Value)
	if err != nil {
		return nil, fmt.Errorf("error decoding account info cookie: %w", err)
	}

	accountInfo := &types.AccountInfo{}
	if err := json.Unmarshal(decodedAccountInfo, accountInfo); err != nil {
		return nil, err
	}

	ctx = ContextWithAccountInfo(ctx, accountInfo)
	// Add isn perms to context
	permsCookie, err := r.Cookie(config.IsnPermsCookieName)
	if errors.Is(err, http.ErrNoCookie) {
		// this is expected - accounts without ISN access will not have this cookie
		return ctx, nil
	}
	if err != nil {
		return ctx, fmt.Errorf("error getting ISN permissions cookie: %w", err)
	}

	decodedPerms, err := base64.StdEncoding.DecodeString(permsCookie.Value)
	if err != nil {
		return ctx, fmt.Errorf("error decoding ISN permissions cookie: %w", err)
	}

	var isnPerms map[string]types.IsnPerm
	if err := json.Unmarshal(decodedPerms, &isnPerms); err != nil {
		return ctx, fmt.Errorf("error unmarshalling ISN permissions cookie: %w", err)
	}
	ctx = ContextWithIsnPerms(ctx, isnPerms)

	return ctx, nil
}
