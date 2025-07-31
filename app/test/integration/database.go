//go:build integration

// set up the integration test db.
package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
)

// DATA HELPERS

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
func grantPermission(t *testing.T, ctx context.Context, queries *database.Queries, isnID, accountID uuid.UUID, permission string) {

	_, err := queries.CreateIsnAccount(ctx, database.CreateIsnAccountParams{
		IsnID:      isnID,
		AccountID:  accountID,
		Permission: permission,
	})
	if err != nil {
		t.Fatalf("Failed to grant %s permission for ISN %s to account %s: %v",
			permission, isnID, accountID, err)
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
