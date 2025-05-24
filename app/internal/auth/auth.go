package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
	Role        string            `json:"role" enums:"owner,admin,member" example:"admin"`
	IsnPerms    map[string]string `json:"isn_perms,omitempty"`
}

type AccessTokenClaims struct {
	jwt.RegisteredClaims
	Role     string            `json:"role" enums:"owner,admin,member" example:"admin"`
	IsnPerms map[string]string `json:"isn_perms,omitempty"`
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
// Roles and ISN read/write permissions (todo) are read from the database and included in the token claims.
// The function returns the token inside a AccessTokenResponse that can be returned to the client.
func (a AuthService) BuildAccessTokenResponse(ctx context.Context) (AccessTokenResponse, error) {

	issuedAt := time.Now()
	expiresAt := issuedAt.Add(signalsd.AccessTokenExpiry)

	userAccountID, ok := ContextUserAccountID(ctx)
	if !ok {
		return AccessTokenResponse{}, fmt.Errorf("unexpected error - userAccountID not in context")
	}

	//get user role
	user, err := a.queries.GetUserByID(ctx, userAccountID)
	if err != nil {
		return AccessTokenResponse{}, fmt.Errorf("unexpected error - could not find userAccountID %v: %v", userAccountID, err)
	}

	if !signalsd.ValidRoles[user.UserRole] {
		return AccessTokenResponse{}, fmt.Errorf("unexpected error - userAccountID %v role {%v} not recognised ", userAccountID, user.UserRole)
	}

	// claims
	claims := AccessTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userAccountID.String(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    signalsd.TokenIssuerName,
		},
		Role: user.UserRole,
	}

	// create a new signed token
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signedAccessToken, err := accessToken.SignedString([]byte(a.secretKey))
	if err != nil {
		return AccessTokenResponse{}, fmt.Errorf("could not sign JWT: %v", err)
	}
	return AccessTokenResponse{
		AccessToken: signedAccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(signalsd.AccessTokenExpiry.Seconds()),
		Role:        user.UserRole,
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
	userAccountID, ok := ContextUserAccountID(ctx)
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
