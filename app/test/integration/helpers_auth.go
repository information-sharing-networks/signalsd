//go:build integration

package integration

// Authentication helpers: access tokens and login requests.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// getAccessToken creates an access token for the account.
// The token claims include the account's ISN permissions, so create the token after granting permissions.
func (env *testEnv) getAccessToken(t *testing.T, accountID uuid.UUID) string {
	t.Helper()

	ctx := auth.ContextWithAccountID(context.Background(), accountID)
	tokenResponse, err := env.authService.CreateAccessToken(ctx)
	if err != nil {
		t.Fatalf("Failed to create access token: %v", err)
	}
	return tokenResponse.AccessToken
}

// createExpiredAccessToken creates an access token for the account that expired an hour ago
func createExpiredAccessToken(t *testing.T, accountID uuid.UUID, secretKey string) string {
	t.Helper()

	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accountID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			Issuer:    signalsd.TokenIssuerName,
		},
		AccountID:   accountID,
		AccountType: "user",
		Role:        "member",
		IsnPerms:    make(map[string]auth.IsnPerm),
	}

	// sign with the same secret key as the auth service
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(secretKey))
	if err != nil {
		t.Fatalf("Failed to create expired access token: %v", err)
	}
	return signedToken
}

// submitLoginRequest posts the request body (email and password) to the submitLoginRequest endpoint
func submitLoginRequest(t *testing.T, baseURL string, requestBody any) *http.Response {
	t.Helper()

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("Failed to marshal login request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/auth/login", baseURL), bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Login request failed: %v", err)
	}
	return response
}
