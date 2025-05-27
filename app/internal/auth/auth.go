package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	signalsd "github.com/nickabs/signalsd/app"
	"github.com/nickabs/signalsd/app/internal/database"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	secretKey   string
	environment string
	queries     *database.Queries
}

type AccessTokenResponse struct {
	AccessToken string            `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJTaWduYWxTZXJ2ZXIiLCJzdWIiOiI2OGZiNWY1Yi1lM2Y1LTRhOTYtOGQzNS1jZDIyMDNhMDZmNzMiLCJleHAiOjE3NDY3NzA2MzQsImlhdCI6MTc0Njc2NzAzNH0.3OdnUNgrvt1Zxs9AlLeaC9DVT6Xwc6uGvFQHb6nDfZs"`
	TokenType   string            `json:"token_type" example:"Bearer"`
	ExpiresIn   int               `json:"expires_in" example:"1800"` //seconds
	AccountID   uuid.UUID         `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	AccountType string            `json:"account_type" enums:"user,service_identity"`
	Role        string            `json:"role" enums:"owner,admin,member" example:"admin"`
	IsnPerms    map[string]string `json:"isn_perms,omitempty"`
}

type AccessTokenClaims struct {
	jwt.RegisteredClaims
	AccountID   uuid.UUID         `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	AccountType string            `json:"account_type" enums:"user,service_identity"`
	Role        string            `json:"role" enums:"owner,admin,member" example:"admin"`
	IsnPerms    map[string]string `json:"isn_perms,omitempty"`
}

func NewAuthService(secretKey string, environment string, queries *database.Queries) *AuthService {
	return &AuthService{
		secretKey:   secretKey,
		environment: environment,
		queries:     queries,
	}
}

func (a AuthService) HashPassword(password string) (string, error) {
	dat, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(dat), nil
}

func (a AuthService) CheckPasswordHash(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// hash a token using sha512
func (a AuthService) HashToken(token string) string {
	hasher := sha512.New()
	hasher.Write([]byte(token))
	return base64.URLEncoding.EncodeToString(hasher.Sum(nil))
}

// check that the hashed value of a token is the same as the supplied hash
func (a AuthService) CheckTokenHash(hash string, token string) bool {
	return hash == a.HashToken(token)
}

// create a JWT signed with HS256 using the app's secret key.
//
// Roles and ISN read/write permissions are retreived from the database and included in the token claims.
//
// The function returns the token inside a AccessTokenResponse that can be returned to the client.
//
// if this function generates an error, it is unexpected and the calling handler should produce a 500 status code
//
// Note that since the tokens last 30 mins, there is the potential for the permissions to become stale.
// if there are particular requests that *must* have the latest permissions the handler should check the db rather than using the claims info.
func (a AuthService) BuildAccessTokenResponse(ctx context.Context) (AccessTokenResponse, error) {

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(signalsd.AccessTokenExpiry)
	isnPerms := make(map[string]string)

	accountID, ok := ContextAccountID(ctx)
	if !ok {
		return AccessTokenResponse{}, fmt.Errorf("unexpected error - accountID not in context")
	}

	//get user role
	account, err := a.queries.GetAccountByID(ctx, accountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AccessTokenResponse{}, fmt.Errorf("user not found: %v", accountID)
		}
		return AccessTokenResponse{}, fmt.Errorf("database error getting user %v: %w", accountID, err)
	}

	if !signalsd.ValidRoles[account.AccountRole] {
		return AccessTokenResponse{}, fmt.Errorf("invalid user role %v for user %v", account.AccountRole, accountID)
	}

	// get the isns for this host
	isns, err := a.queries.GetIsnsWithIsnReceiver(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AccessTokenResponse{}, fmt.Errorf("database error getting ISNs: %w", err)

	}

	// get the isns this account has access to.
	isnAccounts, err := a.queries.GetIsnAccountsByAccountID(ctx, accountID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AccessTokenResponse{}, fmt.Errorf("database error getting ISN accounts: %w", err)
	}

	// set up claims isnPerms map
	switch account.AccountRole {
	case "owner":
		// owner can write to all ISNs
		for _, isn := range isns {
			isnPerms[isn.Slug] = "write"
		}
	case "admin":
		// Admin can write to owned ISNs + ISNs they have been granted permissions to.
		for _, isn := range isns {
			if account.ID == isn.UserAccountID {
				isnPerms[isn.Slug] = "write"
			}
		}
		for _, isnAccount := range isnAccounts {
			isnPerms[isnAccount.IsnSlug] = isnAccount.Permission
		}
	case "member":
		// Member only has granted permissions (not service identites are always treated as members)
		for _, isnAccount := range isnAccounts {
			isnPerms[isnAccount.IsnSlug] = isnAccount.Permission
		}
	default:
		return AccessTokenResponse{}, fmt.Errorf("unexpected role : %v", account.AccountRole)
	}

	// claims
	claims := AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accountID.String(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    signalsd.TokenIssuerName,
		},
		AccountID:   account.ID,
		AccountType: account.AccountType,
		Role:        account.AccountRole,
		IsnPerms:    isnPerms,
	}

	// todo add all signals to context
	// create a new signed token
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signedAccessToken, err := accessToken.SignedString([]byte(a.secretKey))
	if err != nil {
		return AccessTokenResponse{}, fmt.Errorf("failed to sign JWT: %w", err)
	}
	return AccessTokenResponse{
		AccessToken: signedAccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(signalsd.AccessTokenExpiry.Seconds()),
		AccountID:   account.ID,
		AccountType: account.AccountType,
		Role:        account.AccountRole,
		IsnPerms:    isnPerms,
	}, nil
}

// rerturn the string from Authorization Bearer {string} - note the string can be a JWT accsss token or a refresh token
func (a AuthService) GetAccessTokenFromHeader(headers http.Header) (string, error) {
	authorizationHeaderValue := headers.Get("Authorization")
	if authorizationHeaderValue == "" {
		return "", fmt.Errorf("authorization header is missing")
	}

	re := regexp.MustCompile(`^\s*(?i)\bbearer\b\s*([^\s]+)\s*$`)
	accessToken := re.ReplaceAllString(authorizationHeaderValue, "$1")

	if accessToken == authorizationHeaderValue {
		return "", fmt.Errorf(`authorization header format must be Bearer {token}`)
	}

	return accessToken, nil
}

// revoke any open refresh tokens for the user contained in the shared context
// stores the hashed token
// returns the new token as plain text
func (a AuthService) RotateRefreshToken(ctx context.Context) (string, error) {
	userAccountID, ok := ContextAccountID(ctx)
	if !ok {
		return "", fmt.Errorf("authservice: did not receive userAccountID from middleware")
	}

	_, err := a.queries.RevokeAllRefreshTokensForUser(ctx, userAccountID)
	if err != nil {
		return "", fmt.Errorf("authservice: could not revoke previous refresh tokens for user %v", userAccountID)
	}

	// Generate random bytes
	tokenBytes := make([]byte, 32)
	_, err = io.ReadFull(rand.Reader, tokenBytes)
	if err != nil {
		return "", fmt.Errorf("authservice: error creating refresh token: %v", err)
	}

	// Convert to base64 string for safe transmission/storage
	plainTextToken := base64.URLEncoding.EncodeToString(tokenBytes)

	// Hash the plain text token
	hashedToken := a.HashToken(plainTextToken)

	// store the hashed value
	_, err = a.queries.InsertRefreshToken(ctx, database.InsertRefreshTokenParams{
		HashedToken:   hashedToken,
		UserAccountID: userAccountID,
		ExpiresAt:     time.Now().Add(signalsd.RefreshTokenExpiry),
	})
	if err != nil {
		return "", fmt.Errorf("authservice: could not insert refresh token: %v", err)
	}

	return plainTextToken, nil
}
func (a AuthService) NewRefreshTokenCookie(environment string, refreshToken string) *http.Cookie {

	isProd := a.environment == "prod" //Secure flag is only true on prod

	newCookie := &http.Cookie{
		Name:     signalsd.RefreshTokenCookieName,
		Value:    refreshToken,
		Path:     "/auth",
		Expires:  time.Now().Add(signalsd.RefreshTokenExpiry),
		HttpOnly: true,
		Secure:   isProd,
		SameSite: http.SameSiteLaxMode,
	}

	return newCookie
}
