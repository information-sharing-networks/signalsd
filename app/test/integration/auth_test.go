//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
)

// TestAuth tests the authentication and authorization flow including:
// - Auth database queries
// - Permission logic
// - JWT token creation
//
// Each test creates an empty temporary database and applies all the migrations so the schema reflects the latest code. The database is dropped after each test.
func TestAuth(t *testing.T) {
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
		testRolePermissions(t, authService, ownerAccount.ID, "user", "owner", expectedPerms, validSignalTypePaths)
	})

	t.Run("admin role permissions", func(t *testing.T) {
		// admin should have write access to their own ISN plus the granted write permission on public-isn
		expectedPerms := map[string]string{
			"admin-isn":  "write",
			"public-isn": "write",
		}
		testRolePermissions(t, authService, adminAccount.ID, "user", "admin", expectedPerms, validSignalTypePaths)
	})

	t.Run("member role permissions", func(t *testing.T) {
		// member was granted read access to admin-isn
		expectedPerms := map[string]string{
			"admin-isn": "read",
		}
		testRolePermissions(t, authService, memberAccount.ID, "user", "member", expectedPerms, validSignalTypePaths)
	})

	t.Run("service account permissions", func(t *testing.T) {
		// service account was granted write to the public isn
		expectedPerms := map[string]string{
			"public-isn": "write",
		}
		testRolePermissions(t, authService, serviceAccount.ID, "service_account", "member", expectedPerms, validSignalTypePaths)
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

// testRolePermissions is a helper that tests the permissions for a given account
// tests are done on the returned struct from CreateAccessToken and the parsed JWT token
func testRolePermissions(t *testing.T, authService *auth.AuthService, accountID uuid.UUID, accountType string, expectedRole string, expectedPerms map[string]string, validSignalTypePaths map[string]string) {
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

		// service account was set up with a batch for the public ISN
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
	if claims.Issuer != "Signalsd" { // This should match signalsd.TokenIssuerName
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
