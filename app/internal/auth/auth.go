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
	"github.com/nickabs/signalsd/app/internal/database"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	secretKey   string
	environment string
	queries     *database.Queries
}

func NewAuthService(secretKey string, environment string, queries *database.Queries) *AuthService {
	return &AuthService{
		secretKey:   secretKey,
		environment: environment,
		queries:     queries,
	}
}

func (a AuthService) HashPassword(password string) (string, error) {
	dat, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", err
	}
	return string(dat), nil

}

func (a AuthService) CheckPasswordHash(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// create a JWT signed with HS256 using the supplied secret
func (a AuthService) GenerateAccessToken(userAccountID uuid.UUID, expiresIn time.Duration) (string, error) {
	issuedAt := time.Now()
	expiresAt := issuedAt.Add(expiresIn)

	claims := &jwt.RegisteredClaims{

		Issuer:    "SignalServer",
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
		Subject:   userAccountID.String(),
	}
	unsignedAccessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	signedAccessToken, err := unsignedAccessToken.SignedString([]byte(a.secretKey))
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

func (a AuthService) GenerateRefreshToken() (string, error) {

	tokenBytes := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, tokenBytes)
	if err != nil {
		return "", fmt.Errorf("error creating refresh token: %v", err)
	}
	return hex.EncodeToString(tokenBytes), nil
}
