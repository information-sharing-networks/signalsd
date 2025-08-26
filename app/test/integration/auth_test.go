//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// TestPermissions tests the authentication and authorization flow including:
// - Auth database queries
// - Permission logic
// - JWT token creation
//
// Each test creates an empty temporary database and applies all the migrations so the schema reflects the latest code. The database is dropped after each test.
func TestPermissions(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)

	queries := database.New(testDB)

	authService := auth.NewAuthService(secretKey, "test", queries)

	// Create test accounts
	ownerAccount := createTestAccount(t, ctx, queries, "owner", "user", "owner@gmail.com")
	adminAccount := createTestAccount(t, ctx, queries, "admin", "user", "admin@gmail.com")
	memberAccount := createTestAccount(t, ctx, queries, "member", "user", "member@gmail.com")
	serviceAccount := createTestAccount(t, ctx, queries, "member", "service_account", "service@gmail.com")

	// Create ISNs
	ownerISN := createTestISN(t, ctx, queries, "owner-isn", "Owner ISN", ownerAccount.ID, "private")
	adminISN := createTestISN(t, ctx, queries, "admin-isn", "Admin ISN", adminAccount.ID, "private")
	publicISN := createTestISN(t, ctx, queries, "public-isn", "Public ISN", adminAccount.ID, "public")

	// Create signal types
	_ = createTestSignalType(t, ctx, queries, ownerISN.ID, "owner ISN signal", "1.0.0")
	_ = createTestSignalType(t, ctx, queries, adminISN.ID, "admin ISN signal", "1.0.0")
	_ = createTestSignalType(t, ctx, queries, publicISN.ID, "public ISN signal", "1.0.0")

	// Grant permission to ISNs
	// note there is no need to  grant permissions to owners (automatically get write access to all isns)
	// ... or admins (automatically get write access to their own isns)
	grantPermission(t, ctx, queries, adminISN.ID, memberAccount.ID, "read")
	grantPermission(t, ctx, queries, publicISN.ID, serviceAccount.ID, "write")

	// Create signal batches
	// note only service accounts need to create batches explicitly - user accounts automatically create batches when they write to an isn
	createTestSignalBatch(t, ctx, queries, publicISN.ID, serviceAccount.ID)

	var validSignalTypePaths = make(map[string]string)

	validSignalTypePaths["owner-isn"] = "owner-isn-signal/v1.0.0"
	validSignalTypePaths["admin-isn"] = "admin-isn-signal/v1.0.0"
	validSignalTypePaths["public-isn"] = "public-isn-signal/v1.0.0"

	t.Run("owner role permissions", func(t *testing.T) {
		// owner should have write access to all ISNs
		expectedPerms := map[string]string{
			"owner-isn":  "write",
			"admin-isn":  "write",
			"public-isn": "write",
		}
		checkPermissions(t, authService, ownerAccount.ID, "user", "owner", expectedPerms, validSignalTypePaths)
	})

	t.Run("admin role permissions", func(t *testing.T) {
		// admin should have write access to their own ISN plus the granted write permission on public-isn
		expectedPerms := map[string]string{
			"admin-isn":  "write",
			"public-isn": "write",
		}
		checkPermissions(t, authService, adminAccount.ID, "user", "admin", expectedPerms, validSignalTypePaths)
	})

	t.Run("member role permissions", func(t *testing.T) {
		// member was granted read access to admin-isn
		expectedPerms := map[string]string{
			"admin-isn": "read",
		}
		checkPermissions(t, authService, memberAccount.ID, "user", "member", expectedPerms, validSignalTypePaths)
	})

	t.Run("service account permissions", func(t *testing.T) {
		// service account was granted write to the public isn
		expectedPerms := map[string]string{
			"public-isn": "write",
		}
		checkPermissions(t, authService, serviceAccount.ID, "service_account", "member", expectedPerms, validSignalTypePaths)
	})

	t.Run("error handling ", func(t *testing.T) {

		t.Run("nonexistent account", func(t *testing.T) {
			ctx := auth.ContextWithAccountID(context.Background(), uuid.New())
			_, err := authService.CreateAccessToken(ctx)
			if err == nil {
				t.Fatal("expected error for nonexistent account")
			}
			expectedError := "user not found"
			if !strings.Contains(err.Error(), expectedError) {
				t.Errorf("expected error containing %q, got %q", expectedError, err.Error())
			}
		})

		t.Run("missing account in context", func(t *testing.T) {
			ctx := context.Background() // no account ID
			_, err := authService.CreateAccessToken(ctx)
			if err == nil {
				t.Fatal("expected error for missing account ID")
			}
		})
	})
}

// checkPermissions is a helper that tests the permissions for a given account
// tests are done on the struct returned from CreateAccessToken and the parsed JWT token
func checkPermissions(t *testing.T, authService *auth.AuthService, accountID uuid.UUID, accountType string, expectedRole string, expectedPerms map[string]string, validSignalTypePaths map[string]string) {
	t.Helper()

	ctx := auth.ContextWithAccountID(context.Background(), accountID)
	response, err := authService.CreateAccessToken(ctx)
	if err != nil {
		t.Fatalf("CreateAccessToken failed: %v", err)
	}

	if response.AccountID != accountID {
		t.Errorf("expected account ID %s, got %s", accountID, response.AccountID)
	}

	if response.Role != expectedRole {
		t.Errorf("expected role %s, got %s", expectedRole, response.Role)
	}

	// checks that the response contains expected ISN permissions

	if len(response.Perms) != len(expectedPerms) {
		t.Fatalf("expected %d ISN permissions, got %d", len(expectedPerms), len(response.Perms))
	}

	for isnSlug, expectedPermission := range expectedPerms {
		perm, exists := response.Perms[isnSlug]
		if !exists {
			t.Errorf("missing permission for ISN %s", isnSlug)
			continue
		}
		if perm.Permission != expectedPermission {
			t.Errorf("expected %s permission for %s, got %s", expectedPermission, isnSlug, perm.Permission)
		}

		// service account was set up with a batch for the public ISN (which it has write access to)
		if perm.Permission == "write" && accountType == "service_account" && perm.SignalBatchID == nil {
			t.Errorf("expected signal batch ID for %s, got nil", isnSlug)
		}

		if perm.Permission == "read" && perm.SignalBatchID != nil {
			t.Errorf("expected signal batch to be nil for %s, got %s", isnSlug, perm.SignalBatchID)
		}

		// Verify signal type paths are correctly populated
		if len(perm.SignalTypePaths) != 1 {
			t.Errorf("expected 1 signal type path for %s, got %d", isnSlug, len(perm.SignalTypePaths))
			continue
		}
		signalTypePath := perm.SignalTypePaths[0]
		if expectedPath, exists := validSignalTypePaths[isnSlug]; exists {
			if signalTypePath != expectedPath {
				t.Errorf("expected signal type path %s for %s, got %s", expectedPath, isnSlug, signalTypePath)
			}
		}
	}

	// validate JWT Token parses and validates the JWT token structure
	if response.AccessToken == "" {
		t.Fatalf("expected access token to be generated")
		return
	}

	// parse the claims (confirms signature and structure)
	claims := &auth.AccessTokenClaims{}
	_, err = jwt.ParseWithClaims(response.AccessToken, claims, func(token *jwt.Token) (any, error) {
		return []byte(secretKey), nil
	})
	if err != nil {
		t.Fatalf("Failed to parse JWT token: %v", err)
	}

	// Check expiration - token should not be expired
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		t.Error("JWT token is expired")
	}

	// Check issued at time - should be recent (within last minute)
	if claims.IssuedAt != nil {
		issuedAt := claims.IssuedAt.Time
		if time.Since(issuedAt) > time.Minute {
			t.Errorf("JWT issued at time %v is too old", issuedAt)
		}
	}

	// Verify claims match expected values
	if claims.AccountID != accountID {
		t.Errorf("expected account ID %s in JWT, got %s", accountID, claims.AccountID)
	}
	if claims.Role != expectedRole {
		t.Errorf("expected role %s in JWT, got %s", expectedRole, claims.Role)
	}

	// Verify standard JWT claims
	if claims.Subject != accountID.String() {
		t.Errorf("expected subject %s in JWT, got %s", accountID.String(), claims.Subject)
	}
	if claims.Issuer != signalsd.TokenIssuerName {
		t.Errorf("expected issuer 'Signalsd' in JWT, got %s", claims.Issuer)
	}

	// Verify JWT claims match the response data
	if claims.AccountType != response.AccountType {
		t.Errorf("JWT account type %s doesn't match response account type %s", claims.AccountType, response.AccountType)
	}

	if len(claims.IsnPerms) != len(response.Perms) {
		t.Errorf("JWT has %d ISN permissions, response has %d", len(claims.IsnPerms), len(response.Perms))
	}

	for isnSlug, responsePerm := range response.Perms {
		jwtPerm, exists := claims.IsnPerms[isnSlug]
		if !exists {
			t.Errorf("ISN %s missing from JWT claims but present in response", isnSlug)
			continue
		}

		if jwtPerm.Permission != responsePerm.Permission {
			t.Errorf("ISN %s: JWT permission %s doesn't match response permission %s",
				isnSlug, jwtPerm.Permission, responsePerm.Permission)
		}

		// Compare signal batch IDs (both should be nil for read permissions, non-nil for write)
		if (jwtPerm.SignalBatchID == nil) != (responsePerm.SignalBatchID == nil) {
			t.Errorf("ISN %s: JWT SignalBatchID nil=%v doesn't match response SignalBatchID nil=%v",
				isnSlug, jwtPerm.SignalBatchID == nil, responsePerm.SignalBatchID == nil)
		}

		// Compare signal type paths
		if len(jwtPerm.SignalTypePaths) != len(responsePerm.SignalTypePaths) {
			t.Errorf("ISN %s: JWT has %d signal type paths, response has %d",
				isnSlug, len(jwtPerm.SignalTypePaths), len(responsePerm.SignalTypePaths))
		}
	}
}

// returns a random hashed client secret
func createClientSecret(t *testing.T, ctx context.Context, queries *database.Queries, authService *auth.AuthService, serviceAccountID uuid.UUID, expiresAt time.Time) (string, error) {
	t.Helper()
	clientSecret, err := authService.GenerateSecureToken(32)
	if err != nil {
		return "", fmt.Errorf("Failed to generate client secret: %v", err)
	}
	hashedClientSecret := authService.HashToken(clientSecret)
	_, err = queries.CreateClientSecret(ctx, database.CreateClientSecretParams{
		HashedSecret:            hashedClientSecret,
		ServiceAccountAccountID: serviceAccountID,
		ExpiresAt:               expiresAt,
	})
	if err != nil {
		return "", fmt.Errorf("Failed to create client secret: %v", err)
	}
	return hashedClientSecret, nil
}

// TestLoginAuth tests:
// - Successful login with valid credentials
// - Failed login with invalid credentials
// - that account ID/role/accountType on the database are correctly returned in the JWT claims
// - that refresh tokens are correctly created and rotated
//
// note this is a very slow test as it has to hash the passwords for each sub-test
func TestLoginAuth(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)

	queries := database.New(testDB)
	authService := auth.NewAuthService(secretKey, "test", queries)

	// Create test accounts with real hashed passwords

	type testLogin struct {
		email       string
		password    string
		role        string
		accountType string
		accountID   uuid.UUID
	}

	ownerTestLogin := testLogin{
		email:       "owner@login.com",
		password:    "ownerpassword",
		role:        "owner",
		accountType: "user",
	}
	memberTestLogin := testLogin{
		email:       "member@login.com",
		password:    "memberpassword",
		role:        "member",
		accountType: "user",
	}
	adminTestLogin := testLogin{
		email:       "admin@login.com",
		password:    "adminpassword",
		role:        "member",
		accountType: "user",
	}

	// Create test users
	ownerAccount := createTestUserWithPassword(t, ctx, queries, authService, ownerTestLogin.role, ownerTestLogin.email, ownerTestLogin.password)
	memberAccount := createTestUserWithPassword(t, ctx, queries, authService, memberTestLogin.role, memberTestLogin.email, memberTestLogin.password)
	adminAccount := createTestUserWithPassword(t, ctx, queries, authService, adminTestLogin.role, adminTestLogin.email, adminTestLogin.password)

	t.Run("login attempts", func(t *testing.T) {
		tests := []struct {
			name                string
			email               string
			password            string
			expectedRole        string
			expectedAccountID   uuid.UUID
			expectedAccountType string
			expectError         bool
		}{
			{
				name:                "owner can login with correct password",
				email:               ownerTestLogin.email,
				password:            ownerTestLogin.password,
				expectedRole:        ownerTestLogin.role,
				expectedAccountID:   ownerAccount.ID,
				expectedAccountType: ownerTestLogin.accountType,
				expectError:         false,
			},
			{
				name:                "owner can't login with incorrect password",
				email:               ownerTestLogin.email,
				password:            "wrongpassword",
				expectedRole:        ownerTestLogin.role,
				expectedAccountID:   ownerAccount.ID,
				expectedAccountType: ownerTestLogin.accountType,
				expectError:         true,
			},
			{
				name:                "member can login with correct password",
				email:               memberTestLogin.email,
				password:            memberTestLogin.password,
				expectedRole:        memberTestLogin.role,
				expectedAccountID:   memberAccount.ID,
				expectedAccountType: memberTestLogin.accountType,
				expectError:         false,
			},
			{
				name:                "member can't login with incorrect password",
				email:               memberTestLogin.email,
				password:            "wrongpassword",
				expectedRole:        memberTestLogin.role,
				expectedAccountID:   memberAccount.ID,
				expectedAccountType: memberTestLogin.accountType,
				expectError:         true,
			},
			{
				name:                "admin can login with correct password",
				email:               adminTestLogin.email,
				password:            adminTestLogin.password,
				expectedRole:        adminTestLogin.role,
				expectedAccountID:   adminAccount.ID,
				expectedAccountType: adminTestLogin.accountType,
				expectError:         false,
			},
			{
				name:                "admin can't login with incorrect password",
				email:               adminTestLogin.email,
				password:            "wrongpassword",
				expectedRole:        adminTestLogin.role,
				expectedAccountID:   adminAccount.ID,
				expectedAccountType: adminTestLogin.accountType,
				expectError:         true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {

				ctx = auth.ContextWithAccountID(ctx, tt.expectedAccountID)

				user, err := queries.GetUserByEmail(ctx, tt.email)
				if err != nil {
					t.Fatalf("Failed to get user by email: %v", err)
				}

				// Test password validation
				err = authService.CheckPasswordHash(user.HashedPassword, tt.password)
				if err != nil {
					if tt.expectError {
						return
					}
					t.Fatalf("Password hash check failed: %v", err)
				}

				// confirm the test login id matches the accountID on the database and access token
				accessTokenResponse, err := authService.CreateAccessToken(ctx)
				if err != nil {
					t.Fatalf("CreateAccessToken failed: %v", err)
				}

				if user.AccountID != tt.expectedAccountID {
					t.Errorf("Expected account ID %s, got %s from database", user.AccountID, tt.expectedAccountID)
				}

				if user.UserRole != tt.expectedRole {
					t.Errorf("Expected role %s, got %s from database", tt.expectedRole, user.UserRole)
				}

				if accessTokenResponse.AccountID != tt.expectedAccountID {
					t.Errorf("Expected account ID %s, got %s from access token response", user.AccountID, accessTokenResponse.AccountID)
				}

				if accessTokenResponse.Role != tt.expectedRole {
					t.Errorf("Expected role %s, got %s", tt.expectedRole, accessTokenResponse.Role)
				}
				if accessTokenResponse.AccountType != tt.expectedAccountType {
					t.Errorf("Expected account type %s, got %s", tt.expectedAccountType, accessTokenResponse.AccountType)
				}

				// rotate refresh token
				refreshToken, err := authService.RotateRefreshToken(ctx)
				if err != nil {
					t.Fatalf("RotateRefreshToken failed: %v", err)
				}

				// check the refresh token is in the database
				hashedToken := authService.HashToken(refreshToken)
				refreshTokenRow, err := queries.GetRefreshToken(ctx, hashedToken)
				if err != nil {
					t.Fatalf("GetRefreshToken failed: %v", err)
				}
				if refreshTokenRow.UserAccountID != user.AccountID {
					t.Errorf("Expected user account ID %s, got %s from refresh token row", user.AccountID, refreshTokenRow.UserAccountID)
				}
				if refreshTokenRow.RevokedAt != nil {
					t.Errorf("Expected refresh token not to be revoked, got %v", refreshTokenRow.RevokedAt)
				}
				if refreshTokenRow.ExpiresAt.Before(time.Now()) {
					t.Errorf("Expected refresh token not to be unexpired, got %v", refreshTokenRow.ExpiresAt)
				}
			})

		}
	})

}

// TestClientCredentialsAuth tests the client credentials authentication flow including:
// - Successful authentication with valid credentials
// - Failed authentication with invalid credentials, expired credentials, revoked credentials
func TestClientCredentialsAuth(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)
	queries := database.New(testDB)
	authService := auth.NewAuthService(secretKey, "test", queries)

	// Create test service account
	serviceAccount := createTestAccount(t, ctx, queries, "member", "service_account", "service@client.com")

	// Create client secret for service account
	hashedClientSecret, err := createClientSecret(t, ctx, queries, authService, serviceAccount.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Failed to create client secret: %v", err)
	}

	// Test successful auth
	ctx = auth.ContextWithAccountID(ctx, serviceAccount.ID)
	accessTokenResponse, err := authService.CreateAccessToken(ctx)
	if err != nil {
		t.Fatalf("CreateAccessToken failed: %v", err)
	}

	t.Run("valid client credentials should authenticate", func(t *testing.T) {
		if accessTokenResponse.AccountID != serviceAccount.ID {
			t.Errorf("Expected account ID %s, got %s from access token response", serviceAccount.ID, accessTokenResponse.AccountID)
		}
		dbClientSecret, err := queries.GetValidClientSecretByServiceAccountAccountId(ctx, serviceAccount.ID)
		if err != nil {
			t.Fatalf("Failed to get client secret: %v", err)
		}
		if hashedClientSecret != dbClientSecret.HashedSecret {
			t.Errorf("Expected client secret %s, got %s from database", hashedClientSecret, dbClientSecret.HashedSecret)
		}
	})

	t.Run("revoked client secret should not authenticate", func(t *testing.T) {
		_, err := queries.RevokeClientSecret(ctx, hashedClientSecret)
		if err != nil {
			t.Fatalf("Failed to revoke client secret: %v", err)
		}
		_, err = queries.GetValidClientSecretByServiceAccountAccountId(ctx, serviceAccount.ID)
		if err == nil {
			t.Errorf("Expected error when trying to retrieve revoked client secret, got none")
		}
	})

	t.Run("expired client secret fails to authenticate", func(t *testing.T) {
		_, err := createClientSecret(t, ctx, queries, authService, serviceAccount.ID, time.Now().Add(-time.Hour))
		if err != nil {
			t.Fatalf("Failed to create client secret: %v", err)
		}
		_, err = queries.GetValidClientSecretByServiceAccountAccountId(ctx, serviceAccount.ID)
		if err == nil {
			t.Error("Expected error when trying to retrieve expired client secret, got none")
		}
	})
}

// TestDisabledAccountAuth checks the behaviour of account.is_active = false
//
//  1. confirms the primary control in CreateAccessToken prevents new access tokens being created by disabled accounts, thereby preventing them using any of the proteced endpoints (note there are secondary controls in the middleware and login handler)
//  2. That the following items are revoked
//     - client secrets/one time secrets (service accounts)
//     - refresh tokens (users)
func TestDisabledAccountAuth(t *testing.T) {
	ctx := context.Background()
	testDB := setupTestDatabase(t, ctx)

	queries := database.New(testDB)

	authService := auth.NewAuthService(secretKey, "test", queries)

	// create test data
	t.Log("Creating test data...")

	ownerAccount := createTestAccount(t, ctx, queries, "owner", "user", "owner@caniuse.com")

	ownerISN := createTestISN(t, ctx, queries, "owner-correlated-isn", "Owner caniuse ISN", ownerAccount.ID, "private")

	memberAccount := createTestAccount(t, ctx, queries, "member", "user", "member@caniuse.com")

	serviceAccount := createTestAccount(t, ctx, queries, "member", "service_account", "serviceaccount@caniuse.com")

	// Create client secret for service account
	_, err := createClientSecret(t, ctx, queries, authService, serviceAccount.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Failed to create client secret: %v", err)
	}

	// grant members access to the isn
	grantPermission(t, ctx, queries, ownerISN.ID, memberAccount.ID, "write")
	grantPermission(t, ctx, queries, ownerISN.ID, serviceAccount.ID, "write")

	// note the authservice uses the accountID in the context to determine the account being accessed
	ctx = auth.ContextWithAccountID(context.Background(), memberAccount.ID)
	t.Run("disabled web user account access denied", func(t *testing.T) {
		disableAccount(t, ctx, queries, memberAccount.ID)
		_, err := authService.CreateAccessToken(ctx)
		if err == nil {
			t.Errorf("disabled web user account %v was allowed to create an access token ", memberAccount.ID)
		}

		// check the user's refresh token was revoked
		count, err := queries.CountActiveRefreshTokens(ctx, memberAccount.ID)
		if err != nil {
			t.Fatalf("failed to query refresh_tokens for web user: %v", err)
		}

		if count != 0 {
			t.Errorf("found %v unrevoked tokens for disabled web user account %v", count, memberAccount.ID)
		}
	})

	t.Run("reinstated web user account allowed to create access token", func(t *testing.T) {

		enableAccount(t, ctx, queries, memberAccount.ID)
		_, err := authService.CreateAccessToken(ctx)
		if err != nil {
			t.Errorf("reinstated web user account %v was not allowed to create an access token ", memberAccount.ID)
		}
		// note enabling web accounts does not automatically create a refresh token (this is done when they next log in)
	})

	ctx = auth.ContextWithAccountID(context.Background(), serviceAccount.ID)

	t.Run("disabled service account access denied", func(t *testing.T) {
		disableAccount(t, ctx, queries, serviceAccount.ID)
		_, err := authService.CreateAccessToken(ctx)
		if err == nil {
			t.Errorf("disabled service account %v was allowed to create an access token ", serviceAccount.ID)
		}

		// check the client secret was revoked
		count, err := queries.CountActiveClientSecrets(ctx, serviceAccount.ID)
		if err != nil {
			t.Fatalf("failed to query client_secrets for web user: %v", err)
		}

		if count != 0 {
			t.Errorf("found %v active client secrets for disabled service account %v", count, memberAccount.ID)
		}

	})
}
