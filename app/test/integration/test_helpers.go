//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
)

const (

	// Test signal type
	testSignalTypeDetail = "Simple test signal type for integration tests"
	testSchemaURL        = "https://github.com/information-sharing-networks/signalsd_test_schemas/blob/main/2025.05.13/integration-test-schema.json"
	testReadmeURL        = "https://github.com/information-sharing-networks/signalsd_test_schemas/blob/main/2025.05.13/README.md"
	testSchemaContent    = `{"type": "object", "properties": {"test": {"type": "string"}}, "required": ["test"], "additionalProperties": false }`
)

// createExpiredAccessToken creates an expired JWT access token for testing purposes
func createExpiredAccessToken(t *testing.T, accountID uuid.UUID, secretKey string) string {
	t.Helper()

	// Create JWT claims with expired timestamp
	issuedAt := time.Now().Add(-2 * time.Hour)  // 2 hours ago
	expiresAt := time.Now().Add(-1 * time.Hour) // 1 hour ago (expired)

	claims := auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   accountID.String(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    signalsd.TokenIssuerName,
		},
		AccountID:   accountID,
		AccountType: "user",
		Role:        "member",
		IsnPerms:    make(map[string]auth.IsnPerm),
	}

	// Create and sign the token using the same secret key as the auth service
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString([]byte(secretKey))
	if err != nil {
		t.Fatalf("Failed to create expired access token: %v", err)
	}

	return signedToken
}

// database helpers

// createTestAccount creates entries in account and user/service_account tables
func createTestAccount(t *testing.T, ctx context.Context, queries *database.Queries, role, accountType string, email string) database.GetAccountByIDRow {

	// Create account record
	account, err := queries.CreateUserAccount(ctx)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	switch accountType {
	case "user":
		if role == "siteadmin" { // first user is always a site admin
			_, err = queries.CreateSiteAdminUser(ctx, database.CreateSiteAdminUserParams{
				AccountID:      account.ID,
				Email:          email,
				HashedPassword: "hashed_password_placeholder",
			})
		} else {
			_, err = queries.CreateUser(ctx, database.CreateUserParams{
				AccountID:      account.ID,
				Email:          email,
				HashedPassword: "hashed_password_placeholder",
			})
		}
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}

		if role == "isnadmin" {
			_, err = queries.UpdateUserAccountToIsnAdmin(ctx, account.ID)
			if err != nil {
				t.Fatalf("Failed to update user to admin: %v", err)
			}
		}

	case "service_account":
		serviceAccount, err := queries.CreateServiceAccountAccount(ctx)
		if err != nil {
			t.Fatalf("Failed to create service account: %v", err)
		}

		_, err = queries.CreateServiceAccount(ctx, database.CreateServiceAccountParams{
			AccountID:          serviceAccount.ID,
			ClientID:           fmt.Sprintf("test-client-%s", serviceAccount.ID.String()[:8]),
			ClientContactEmail: email,
			ClientOrganization: "test client org",
		})
		if err != nil {
			t.Fatalf("Failed to create service account record: %v", err)
		}

		// Use the service account ID
		account = serviceAccount
	}

	return database.GetAccountByIDRow{
		ID:          account.ID,
		AccountType: account.AccountType,
		AccountRole: role,
	}
}

// createTestISN creates a test ISN with specified owner and visibility
func createTestISN(t *testing.T, ctx context.Context, queries *database.Queries, slug, title string, siteAdminID uuid.UUID, visibility string) database.Isn {

	result, err := queries.CreateIsn(ctx, database.CreateIsnParams{
		UserAccountID: siteAdminID,
		Title:         title,
		Slug:          slug,
		Detail:        fmt.Sprintf("Test ISN: %s", title),
		IsInUse:       true,
		Visibility:    visibility,
	})
	if err != nil {
		t.Fatalf("Failed to create ISN %s: %v", slug, err)
	}

	return database.Isn{
		ID:            result.ID,
		Slug:          result.Slug,
		UserAccountID: siteAdminID,
		Title:         title,
		IsInUse:       true,
		Visibility:    visibility,
	}
}

// getBatchByRef returns a batch by account and batch_ref, or nil if none exists
func getBatchByRef(t *testing.T, ctx context.Context, queries *database.Queries, accountID uuid.UUID, batchRef string) *database.SignalBatch {
	t.Helper()

	batch, err := queries.GetSignalBatchByRefAndAccountID(ctx, database.GetSignalBatchByRefAndAccountIDParams{
		AccountID: accountID,
		BatchRef:  batchRef,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		t.Fatalf("Failed to get batch %q: %v", batchRef, err)
	}
	return &batch
}

// createTestSignalType creates a signal type and associates it with an ISN
func createTestSignalType(t *testing.T, ctx context.Context, queries *database.Queries, isnID uuid.UUID, title string, version string) database.SignalType {

	slug, _ := utils.GenerateSlug(title)

	signalType, err := queries.CreateSignalType(ctx, database.CreateSignalTypeParams{
		Slug:          slug,
		SchemaURL:     testSchemaURL,
		ReadmeURL:     testReadmeURL,
		Title:         title,
		Detail:        testSignalTypeDetail,
		SemVer:        version,
		SchemaContent: testSchemaContent,
	})
	if err != nil {
		t.Fatalf("Failed to create signal type %s/%s: %v", slug, version, err)
	}

	// add the signal type to the ISN
	err = queries.AddSignalTypeToIsn(ctx, database.AddSignalTypeToIsnParams{
		IsnID:        isnID,
		SignalTypeID: signalType.ID,
	})
	if err != nil {
		t.Fatalf("Failed to add signal type %s/%s with ISN: %v", slug, version, err)
	}

	return signalType
}

// grantPermission creates a permission grant between an account and ISN
// permission should be "read" (read-only), "write" (write-only), or "read-write" (both)
func grantPermission(t *testing.T, ctx context.Context, queries *database.Queries, isnID, accountID uuid.UUID, permission string) {
	canRead := permission == "read" || permission == "read-write"
	canWrite := permission == "write" || permission == "read-write"

	_, err := queries.UpsertIsnAccount(ctx, database.UpsertIsnAccountParams{
		IsnID:     isnID,
		AccountID: accountID,
		CanRead:   canRead,
		CanWrite:  canWrite,
	})
	if err != nil {
		t.Fatalf("Failed to grant %s permission for ISN %s to account %s: %v",
			permission, isnID, accountID, err)
	}
}

func disableAccount(t *testing.T, ctx context.Context, queries *database.Queries, accountID uuid.UUID) {
	_, err := queries.DisableAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("failed to disable account %s: %v", accountID, err)
	}
}

func enableAccount(t *testing.T, ctx context.Context, queries *database.Queries, accountID uuid.UUID) {
	_, err := queries.EnableAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("failed to disable account %s: %v", accountID, err)
	}
}

// createTestSignalBatch creates or retrieves a signal batch for an account using the given batch_ref.
// In production this happens automatically when a signal is written.
func createTestSignalBatch(t *testing.T, ctx context.Context, queries *database.Queries, accountID uuid.UUID, batchRef string) database.SignalBatch {
	t.Helper()

	batch, err := queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
		BatchRef:  batchRef,
		AccountID: accountID,
	})
	if err != nil {
		t.Fatalf("Failed to get or create signal batch (ref=%q, account=%s): %v", batchRef, accountID, err)
	}

	return batch
}

// createTestUserWithPassword creates a user account with a specific hashed password for login testing
func createTestUserWithPassword(t *testing.T, ctx context.Context, queries *database.Queries, authService *auth.AuthService, role, email, password string) database.GetAccountByIDRow {
	t.Helper()

	hashedPassword, err := authService.HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	// Create account record
	account, err := queries.CreateUserAccount(ctx)
	if err != nil {
		t.Fatalf("Failed to create account: %v", err)
	}

	if role == "siteadmin" { // first user is always site admin
		_, err = queries.CreateSiteAdminUser(ctx, database.CreateSiteAdminUserParams{
			AccountID:      account.ID,
			Email:          email,
			HashedPassword: hashedPassword,
		})
	} else {
		_, err = queries.CreateUser(ctx, database.CreateUserParams{
			AccountID:      account.ID,
			Email:          email,
			HashedPassword: hashedPassword,
		})
	}
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	if role == "isnadmin" {
		_, err = queries.UpdateUserAccountToIsnAdmin(ctx, account.ID)
		if err != nil {
			t.Fatalf("Failed to update user to admin: %v", err)
		}
	}

	return database.GetAccountByIDRow{
		ID:          account.ID,
		AccountType: account.AccountType,
		AccountRole: role,
	}
}
