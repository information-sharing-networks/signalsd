package auth

// AuthService provides authentication and authorization services for the UI (sets browser cookies, refreshes tokens)

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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
	form := url.Values{"grant_type": {"refresh_token"}}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/oauth/token", a.apiBaseURL), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// add the refresh token cookie from the browser's request to the API request
	req.AddCookie(refreshTokenCookie)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var errorResp types.OAuthErrorResponse
		if err := json.NewDecoder(res.Body).Decode(&errorResp); err != nil {
			return nil, nil, fmt.Errorf("token refresh failed with status %d", res.StatusCode)
		}
		return nil, nil, fmt.Errorf("token refresh failed: %s (%s)", errorResp.Error, errorResp.ErrorDescription)
	}

	var accessTokenDetails types.AccessTokenDetails
	if err := json.NewDecoder(res.Body).Decode(&accessTokenDetails); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %v", err)
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

// RevokeToken calls the signalsd backend API to revoke the supplied refresh token (logout).
// Errors are returned so the caller can log them, but logout should proceed regardless.
func (a *AuthService) RevokeToken(refreshTokenCookie *http.Cookie) error {
	form := url.Values{"grant_type": {"refresh_token"}}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/oauth/revoke", a.apiBaseURL), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(refreshTokenCookie)

	res, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		var errorResp types.OAuthErrorResponse
		if err := json.NewDecoder(res.Body).Decode(&errorResp); err != nil {
			return fmt.Errorf("token revocation failed with status %d", res.StatusCode)
		}
		return fmt.Errorf("token revocation failed: %s (%s)", errorResp.Error, errorResp.ErrorDescription)
	}

	return nil
}

// authCookiePayload is the minimal payload stored in the access_details_token cookie.
// Account identity, role, and ISN permissions are derived from the JWT claims on read
// rather than duplicated in the cookie, keeping it well under the 4KB browser cookie limit.
type authCookiePayload struct {
	AccessToken string `json:"access_token"`
	Email       string `json:"email"`
}

// accessTokenClaims mirrors the claims emitted by the signalsd API in JWT access tokens.
type accessTokenClaims struct {
	jwt.RegisteredClaims
	AccountID   string                   `json:"account_id"`
	AccountType string                   `json:"account_type"`
	Role        string                   `json:"role"`
	IsnPerms    map[string]types.IsnPerm `json:"isn_perms,omitempty"`
}

// ParseAuthCookie decodes the access_details_token cookie, extracts account identity and ISN permissions
// from the embedded JWT claims (without signature verification — the signalsd API verifies the token
// on every API call), and reports the token's status.
func (a *AuthService) ParseAuthCookie(cookieValue string) (*types.AccessTokenDetails, AccessTokenStatus, error) {
	decoded, err := base64.StdEncoding.DecodeString(cookieValue)
	if err != nil {
		return nil, TokenInvalid, fmt.Errorf("could not decode access token detail cookie: %w", err)
	}

	var payload authCookiePayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, TokenInvalid, fmt.Errorf("could not unmarshal access token detail cookie: %w", err)
	}

	if payload.AccessToken == "" {
		return nil, TokenInvalid, nil
	}

	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := &accessTokenClaims{}

	if _, _, err := parser.ParseUnverified(payload.AccessToken, claims); err != nil || claims.ExpiresAt == nil {
		return nil, TokenInvalid, nil
	}

	if claims.ExpiresAt.Before(time.Now()) {
		return nil, TokenExpired, nil
	}

	// Reconstruct each signal type's path, slug, and sem_ver from the map key ("{slug}/v{sem_ver}").
	// The JWT stores only {in_use} per key to keep tokens compact.
	for isnSlug, perm := range claims.IsnPerms {
		for path, st := range perm.SignalTypes {
			st.Path = path
			if slug, semVer, ok := strings.Cut(path, "/v"); ok {
				st.Slug = slug
				st.SemVer = semVer
			}
			perm.SignalTypes[path] = st
		}
		claims.IsnPerms[isnSlug] = perm
	}

	details := &types.AccessTokenDetails{
		AccessToken: payload.AccessToken,
		AccountID:   claims.AccountID,
		Email:       payload.Email,
		AccountType: claims.AccountType,
		Role:        claims.Role,
		IsnPerms:    claims.IsnPerms,
	}

	return details, TokenValid, nil
}

// SetAuthCookies sets the authentication-related cookies in the UI HTTP response after authentication
//
// The browser needs to maintain authentication state via cookies so that any signalsd instance can authenticate the user, regardless of which instance handles each request.
//
// The following cookies are set:
//   - refresh token cookie (forwarded from signalsd API)
//   - a compact cookie containing the JWT access token and email (role and ISN permissions are read from the JWT claims)
func (a *AuthService) SetAuthCookies(w http.ResponseWriter, accessTokenDetails *types.AccessTokenDetails, refreshTokenCookie *http.Cookie) error {
	isProdOrStaging := a.environment == "prod" || a.environment == "staging" //secure flag only true on prod and staging

	cookiePayload := authCookiePayload{
		AccessToken: accessTokenDetails.AccessToken,
		Email:       accessTokenDetails.Email,
	}

	accessTokenDetailsJSON, err := json.Marshal(cookiePayload) // #nosec G117 - storing access token in httpOnly cookie, not logging secrets
	if err != nil {
		return fmt.Errorf("failed to marshal access token details: %v", err)
	}

	// Base64 encode to avoid cookie encoding issues
	encodedAccessTokenDetails := base64.StdEncoding.EncodeToString(accessTokenDetailsJSON)

	// #nosec G124 - Secure flag is conditionally true on prod/staging
	http.SetCookie(w, &http.Cookie{
		Name:     config.AccessTokenDetailsCookieName,
		Value:    encodedAccessTokenDetails,
		Path:     "/",
		HttpOnly: true,
		Secure:   isProdOrStaging,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   accessTokenDetails.ExpiresIn,
	})

	// #nosec G124 - Secure flag is conditionally true on prod/staging
	http.SetCookie(w, &http.Cookie{
		Name:     config.RefreshTokenCookieName,
		Value:    refreshTokenCookie.Value,
		Path:     "/",
		HttpOnly: true,
		Secure:   isProdOrStaging,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   refreshTokenCookie.MaxAge,
	})

	return nil
}

// ClearAuthCookies clears all authentication-related cookies
func (a *AuthService) ClearAuthCookies(w http.ResponseWriter) {
	isProdOrStaging := a.environment == "prod" || a.environment == "staging" //secure flag only true on prod and staging

	// #nosec G124 - Secure flag is conditionally true on prod/staging
	http.SetCookie(w, &http.Cookie{
		Name:     config.AccessTokenDetailsCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProdOrStaging,
		SameSite: http.SameSiteStrictMode,
	})

	// #nosec G124 - Secure flag is conditionally true on prod/staging
	http.SetCookie(w, &http.Cookie{
		Name:     config.RefreshTokenCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isProdOrStaging,
		SameSite: http.SameSiteStrictMode,
	})

}
