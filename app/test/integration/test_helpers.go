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
		IsnPerms:    make(map[string]auth.IsnPerms),
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
		if role == "owner" { // first user is always the owner
			_, err = queries.CreateOwnerUser(ctx, database.CreateOwnerUserParams{
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

		if role == "admin" {
			_, err = queries.UpdateUserAccountToAdmin(ctx, account.ID)
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
func createTestISN(t *testing.T, ctx context.Context, queries *database.Queries, slug, title string, ownerID uuid.UUID, visibility string) database.Isn {

	result, err := queries.CreateIsn(ctx, database.CreateIsnParams{
		UserAccountID: ownerID,
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
		UserAccountID: ownerID,
		Title:         title,
		IsInUse:       true,
		Visibility:    visibility,
	}
}

// getLatestBatchForAccountAndISN returns the latest batch for a specific account and ISN, or nil if none exists
func getLatestBatchForAccountAndISN(t *testing.T, ctx context.Context, queries *database.Queries, accountID uuid.UUID, isnSlug string) *database.GetLatestBatchByAccountAndIsnSlugRow {
	t.Helper()

	// Use the dedicated query that returns exactly what we need: 0 or 1 batch
	batch, err := queries.GetLatestBatchByAccountAndIsnSlug(ctx, database.GetLatestBatchByAccountAndIsnSlugParams{
		AccountID: accountID,
		Slug:      isnSlug,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // No batch found
		}
		t.Fatalf("Failed to get latest batch: %v", err)
	}

	return &batch
}

// assertBatchState verifies the state of a batch
func assertBatchState(t *testing.T, ctx context.Context, queries *database.Queries, batchID uuid.UUID, expectedIsLatest bool) {
	t.Helper()

	batch, err := queries.GetSignalBatchByID(ctx, batchID)
	if err != nil {
		t.Fatalf("Failed to get batch %v: %v", batchID, err)
	}

	if batch.IsLatest != expectedIsLatest {
		t.Errorf("Expected batch %v is_latest to be %v, got %v", batchID, expectedIsLatest, batch.IsLatest)
	}
}

// createTestSignalType creates a signal type associated with an ISN
func createTestSignalType(t *testing.T, ctx context.Context, queries *database.Queries, isnID uuid.UUID, title string, version string) database.SignalType {

	slug, _ := utils.GenerateSlug(title)

	signalType, err := queries.CreateSignalType(ctx, database.CreateSignalTypeParams{
		IsnID:         isnID,
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

	return signalType
}

// grantPermission creates a permission grant between an account and ISN
// permission should be "read" (read-only), "write" (write-only), or "read-write" (both)
func grantPermission(t *testing.T, ctx context.Context, queries *database.Queries, isnID, accountID uuid.UUID, permission string) {
	canRead := permission == "read" || permission == "read-write"
	canWrite := permission == "write" || permission == "read-write"

	_, err := queries.CreateIsnAccount(ctx, database.CreateIsnAccountParams{
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

// createTestSignalBatch creates a signal batch for an account and ISN
func createTestSignalBatch(t *testing.T, ctx context.Context, queries *database.Queries, isnID, accountID uuid.UUID) database.SignalBatch {

	batch, err := queries.CreateSignalBatch(ctx, database.CreateSignalBatchParams{
		IsnID:     isnID,
		AccountID: accountID,
	})
	if err != nil {
		t.Fatalf("Failed to create signal batch for ISN %s and account %s: %v",
			isnID, accountID, err)
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

	if role == "owner" { // first user is always the owner
		_, err = queries.CreateOwnerUser(ctx, database.CreateOwnerUserParams{
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

	if role == "admin" {
		_, err = queries.UpdateUserAccountToAdmin(ctx, account.ID)
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
