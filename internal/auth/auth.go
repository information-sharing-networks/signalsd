package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nickabs/signals"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	cfg *signals.ServiceConfig
}

func NewAuthService(cfg *signals.ServiceConfig) *AuthService {
	return &AuthService{cfg: cfg}
}

// get the bearer token from http header, validate the token.
// if the token is a valid JWT, parse the claims and return the user id
func (a AuthService) CheckAuthorization(headers http.Header) (uuid.UUID, error) {

	bearerToken, err := a.BearerTokenFromHeader(headers)
	if err != nil {
		return uuid.Nil, fmt.Errorf("problem with bearer token")
	}
	claims, err := a.ValidateJWT(bearerToken, a.cfg.SecretKey)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid access token")
	}

	rawID := claims.Subject
	userID, err := uuid.Parse(rawID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not parse ID in token")
	}
	return userID, nil
}

func (a AuthService) HashPassword(password string) (string, error) {
	dat, err := bcrypt.GenerateFromPassword([]byte(password), 1)
	if err != nil {
		return "", err
	}
	return string(dat), nil

}
func (a AuthService) CheckPasswordHash(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// create a JWT signed with HS256 using the supplied secret
func (a AuthService) GenerateAccessToken(userID uuid.UUID, secret string, expiresIn time.Duration) (string, error) {
	issuedAt := time.Now()
	expiresAt := issuedAt.Add(expiresIn)

	claims := &jwt.RegisteredClaims{

		Issuer:    "SignalServer",
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		Subject:   userID.String(),
	}
	unsignedAccessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signedAccessToken, err := unsignedAccessToken.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("could not sign JWT: %v", err)
	}
	return signedAccessToken, nil
}

// rerturn the string from Authorization Bearer {string} - note the string can be a JWT accsss token or a refresh token
func (a AuthService) BearerTokenFromHeader(headers http.Header) (string, error) {
	authorizationHeaderValue := headers.Get("Authorization")
	if authorizationHeaderValue == "" {
		return "", fmt.Errorf("authorization header is missing")
	}

	re := regexp.MustCompile(`^\s*(?i)\bbearer\b\s*([^\s]+)\s*$`)
	bearerToken := re.ReplaceAllString(authorizationHeaderValue, "$1")

	if bearerToken == authorizationHeaderValue {
		return "", fmt.Errorf(`authorization header format must be Bearer {token}`)
	}

	return bearerToken, nil
}

// validate a JWT token using the supplied secret, extract and return the claims
func (a AuthService) ValidateJWT(tokenString, secret string) (jwt.RegisteredClaims, error) {
	claims := jwt.RegisteredClaims{}

	_, err := jwt.ParseWithClaims(tokenString, &claims, func(token *jwt.Token) (interface{}, error) {
		// key used to sign the token
		return []byte(secret), nil
	})
	if err != nil {
		return claims, fmt.Errorf("invalid or expired token: %v", err)
	}

	return claims, nil
}

func (a AuthService) GenerateRefreshToken() (string, error) {

	tokenBytes := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, tokenBytes)
	if err != nil {
		return "", fmt.Errorf("error creating refresh token: %v", err)
	}
	return hex.EncodeToString(tokenBytes), nil
}
