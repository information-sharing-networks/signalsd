//go:build integration

package integration

// Tests that service account client secrets are bound to the service account that owns them.
//
// If the secret lookup is not scoped to the account, any service account holding a valid secret can authenticate as any other service
// account by pairing its own secret with the target's client_id - and receives the target's
// role and ISN permissions in the resulting access token.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

// testServiceAccount holds the credentials for a service account created by a test
type testServiceAccount struct {
	accountID uuid.UUID
	clientID  string
	secret    string // plaintext
}

// createServiceAccountWithSecret creates a service account and issues it a client secret.
// A secretExpiry in the past produces an expired (but not revoked) secret, which the
// rotate-secret endpoint is required to accept.
func createServiceAccountWithSecret(t *testing.T, ctx context.Context, env *testEnv, email string, secretExpiry time.Duration) testServiceAccount {
	t.Helper()

	account := createTestAccount(t, ctx, env.queries, "member", "service_account", email)

	secret, err := env.authService.GenerateSecureToken(32)
	if err != nil {
		t.Fatalf("Failed to generate client secret for %s: %v", email, err)
	}

	_, err = env.queries.CreateClientSecret(ctx, database.CreateClientSecretParams{
		ServiceAccountAccountID: account.ID,
		HashedSecret:            env.authService.HashToken(secret),
		ExpiresAt:               time.Now().Add(secretExpiry),
	})
	if err != nil {
		t.Fatalf("Failed to create client secret for %s: %v", email, err)
	}

	details, err := env.queries.GetServiceAccountByAccountID(ctx, account.ID)
	if err != nil {
		t.Fatalf("Failed to get service account details for %s: %v", email, err)
	}

	return testServiceAccount{
		accountID: account.ID,
		clientID:  details.ClientID,
		secret:    secret,
	}
}

// makeRotateSecretRequest makes a POST request to /api/auth/service-accounts/rotate-secret
func makeRotateSecretRequest(t *testing.T, baseURL, clientID, clientSecret string) *http.Response {
	t.Helper()

	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/api/auth/service-accounts/rotate-secret", baseURL), strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}

	return resp
}

// TestClientSecretIsBoundToServiceAccount verifies that a client secret only authenticates the
// service account it was issued to, on both endpoints that accept client credentials.
func TestClientSecretIsBoundToServiceAccount(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	t.Log("Creating test data...")

	// two unrelated service accounts, each with its own valid secret
	accountA := createServiceAccountWithSecret(t, ctx, testEnv, "a@binding.test", time.Hour)
	accountB := createServiceAccountWithSecret(t, ctx, testEnv, "b@binding.test", time.Hour)

	if accountA.clientID == accountB.clientID {
		t.Fatalf("test setup error: both service accounts have client_id %q", accountA.clientID)
	}
	if accountA.secret == accountB.secret {
		t.Fatal("test setup error: both service accounts were issued the same secret")
	}

	t.Run("client_credentials grant", func(t *testing.T) {
		tests := []struct {
			name              string
			clientID          string
			clientSecret      string
			expectedStatus    int
			expectedAccountID uuid.UUID // only checked on a 200
		}{
			{
				name:              "account A's own credentials are accepted",
				clientID:          accountA.clientID,
				clientSecret:      accountA.secret,
				expectedStatus:    http.StatusOK,
				expectedAccountID: accountA.accountID,
			},
			{
				name:              "account B's own credentials are accepted",
				clientID:          accountB.clientID,
				clientSecret:      accountB.secret,
				expectedStatus:    http.StatusOK,
				expectedAccountID: accountB.accountID,
			},
			{
				name:           "account B's secret is rejected for account A's client_id",
				clientID:       accountA.clientID,
				clientSecret:   accountB.secret,
				expectedStatus: http.StatusUnauthorized,
			},
			{
				name:           "account A's secret is rejected for account B's client_id",
				clientID:       accountB.clientID,
				clientSecret:   accountA.secret,
				expectedStatus: http.StatusUnauthorized,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				payload := map[string]string{
					"client_id":     tt.clientID,
					"client_secret": tt.clientSecret,
				}

				response := makeOAuthTokenRequest(t, testEnv.baseURL, "client_credentials", payload, "")
				defer response.Body.Close()

				if response.StatusCode != tt.expectedStatus {
					t.Fatalf("Expected status %d, got %d", tt.expectedStatus, response.StatusCode)
				}

				var responseBody map[string]any
				if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if tt.expectedStatus != http.StatusOK {
					if _, hasToken := responseBody["access_token"]; hasToken {
						t.Error("mismatched credentials were issued an access token")
					}
					if _, hasError := responseBody["error"]; !hasError {
						t.Error("Expected error field in RFC 6749 error response")
					}
					return
				}

				// the token must be issued to the account that owns the secret
				accountID, ok := responseBody["account_id"].(string)
				if !ok {
					t.Fatal("Expected account_id in response")
				}
				if accountID != tt.expectedAccountID.String() {
					t.Errorf("Expected token issued to account %s, got %s", tt.expectedAccountID, accountID)
				}
			})
		}
	})

	t.Run("rotate-secret endpoint", func(t *testing.T) {
		// This endpoint deliberately accepts expired (but not revoked) secrets so a service
		// account that missed a rotation deadline can self-serve recovery. That leniency must
		// not extend to accepting a secret belonging to a different account.
		expiredAccount := createServiceAccountWithSecret(t, ctx, testEnv, "expired@binding.test", -time.Hour)

		t.Run("account B's secret is rejected for account A's client_id", func(t *testing.T) {
			response := makeRotateSecretRequest(t, testEnv.baseURL, accountA.clientID, accountB.secret)
			defer response.Body.Close()

			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("Expected status 401, got %d", response.StatusCode)
			}

			var responseBody map[string]any
			if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}
			if _, hasSecret := responseBody["client_secret"]; hasSecret {
				t.Error("mismatched credentials were issued a rotated secret")
			}
		})

		t.Run("expired secret is rejected for another account's client_id", func(t *testing.T) {
			response := makeRotateSecretRequest(t, testEnv.baseURL, accountA.clientID, expiredAccount.secret)
			defer response.Body.Close()

			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("Expected status 401, got %d", response.StatusCode)
			}
		})

		t.Run("expired secret is accepted for its own client_id", func(t *testing.T) {
			response := makeRotateSecretRequest(t, testEnv.baseURL, expiredAccount.clientID, expiredAccount.secret)
			defer response.Body.Close()

			if response.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 (expired secrets may self-serve rotation), got %d", response.StatusCode)
			}

			var responseBody map[string]any
			if err := json.NewDecoder(response.Body).Decode(&responseBody); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			// the rotation must apply to the caller, not to some other account
			if clientID, ok := responseBody["client_id"].(string); !ok || clientID != expiredAccount.clientID {
				t.Errorf("Expected rotation for client_id %q, got %v", expiredAccount.clientID, responseBody["client_id"])
			}

			newSecret, ok := responseBody["client_secret"].(string)
			if !ok || newSecret == "" {
				t.Fatal("Expected a new client_secret in the rotation response")
			}
			if newSecret == expiredAccount.secret {
				t.Error("rotation returned the same secret")
			}

			// the new secret must authenticate its own account and no other
			ownResponse := makeOAuthTokenRequest(t, testEnv.baseURL, "client_credentials", map[string]string{
				"client_id":     expiredAccount.clientID,
				"client_secret": newSecret,
			}, "")
			defer ownResponse.Body.Close()

			if ownResponse.StatusCode != http.StatusOK {
				t.Errorf("Expected the rotated secret to authenticate its own account, got %d", ownResponse.StatusCode)
			}

			crossResponse := makeOAuthTokenRequest(t, testEnv.baseURL, "client_credentials", map[string]string{
				"client_id":     accountA.clientID,
				"client_secret": newSecret,
			}, "")
			defer crossResponse.Body.Close()

			if crossResponse.StatusCode != http.StatusUnauthorized {
				t.Errorf("Expected the rotated secret to be rejected for another account's client_id, got %d", crossResponse.StatusCode)
			}
		})
	})
}
