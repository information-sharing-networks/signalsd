//go:build integration

package integration

// Test data: records created directly in the database (bypassing the API) to set up the state a test needs.

import (
	"context"
	"fmt"
	"testing"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/utils"
)

// createTestAccount creates entries in account and user/service_account tables
func createTestAccount(t *testing.T, ctx context.Context, queries *database.Queries, role, accountType string, email string) database.GetAccountByIDRow {
	t.Helper()

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
			ClientID:           fmt.Sprintf("test-client-%s", serviceAccount.ID),
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
	t.Helper()

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

// Test signal type - CreateTestSignalType will create a signal type with the same content as the github version in the url
// The setup inserts directly to the DB, rather than using the handler, to avoid going to github in the tests
const (
	testSignalTypeDetail = "Simple test signal type for integration tests"
	testSchemaURL        = "https://github.com/information-sharing-networks/signal-library/blob/main/signalsd-testing/simple.json"
	testReadmeURL        = "https://github.com/information-sharing-networks/signal-library/blob/main/signalsd-testing/README.md"
	testSchemaContent    = `{"type": "object", "properties": {"test": {"type": "string"}}, "required": ["test"], "additionalProperties": false }`

	// event signal types use a schema with the occurred_at field required for events and a certificate number, e.g. { "occurred_at": "2026-09-27T14:02:00Z", "certificate_no": "EHC-001" }
	testEventSchemaContent = `{"type": "object", "properties": {"occurred_at": {"type": "string"}, "certificate_no": {"type": "string"}}, "required": ["occurred_at", "certificate_no"]}`
)

// createTestSignalType creates a signal type with the supplied content kind (json, document, event) and associates it with an ISN.
// json signal types use the simple test schema, which expects content to have a single field called test, e.g "{ "test": "Hello, world!" }"
// event signal types use the event test schema (testEventSchemaContent).
// document signal types have no schema.
// signal types default to version 1.0.0 if no version is supplied
func createTestSignalType(t *testing.T, ctx context.Context, queries *database.Queries, isnID uuid.UUID, title string, version string, contentKind string) database.SignalType {
	t.Helper()

	slug, _ := utils.GenerateSlug(title)

	if version == "" {
		version = "1.0.0"
	}

	schemaURL := testSchemaURL
	schemaContent := testSchemaContent
	switch contentKind {
	case signalsd.ContentKindDocument:
		schemaURL = signalsd.SkipValidationURL
		schemaContent = "{}"
	case signalsd.ContentKindEvent:
		schemaContent = testEventSchemaContent
	}

	signalType, err := queries.CreateSignalType(ctx, database.CreateSignalTypeParams{
		Slug:          slug,
		SchemaURL:     schemaURL,
		ReadmeURL:     testReadmeURL,
		Title:         title,
		Detail:        testSignalTypeDetail,
		SemVer:        version,
		SchemaContent: schemaContent,
		ContentKind:   contentKind,
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

// addSignalTypeToIsn registers an existing signal type on an additional ISN.
// Use this after createTestSignalType when the same signal type needs to appear on more than one ISN.
func addSignalTypeToIsn(t *testing.T, ctx context.Context, queries *database.Queries, isnID, signalTypeID uuid.UUID) {
	t.Helper()
	if err := queries.AddSignalTypeToIsn(ctx, database.AddSignalTypeToIsnParams{
		IsnID:        isnID,
		SignalTypeID: signalTypeID,
	}); err != nil {
		t.Fatalf("Failed to add signal type %s to ISN %s: %v", signalTypeID, isnID, err)
	}
}

// grantPermission creates a permission grant between an account and ISN
// permission should be "read" (read-only), "write" (write-only), or "read-write" (both)
func grantPermission(t *testing.T, ctx context.Context, queries *database.Queries, isnID, accountID uuid.UUID, permission string) {
	t.Helper()
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

func enableAccount(t *testing.T, ctx context.Context, queries *database.Queries, accountID uuid.UUID) {
	_, err := queries.EnableAccount(ctx, accountID)
	if err != nil {
		t.Fatalf("failed to disable account %s: %v", accountID, err)
	}
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
