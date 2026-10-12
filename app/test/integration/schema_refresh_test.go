//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// TestRefreshSignalTypeSchema checks the requests to refresh a schema that are rejected before the schema is fetched from GitHub
func TestRefreshSignalTypeSchema(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@refresh-schema.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@refresh-schema.com")

	isn := createTestISN(t, ctx, testEnv.queries, "refresh-schema-isn", "Refresh schema ISN", adminAccount.ID, "private")
	jsonSignalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "consignment", "", signalsd.ContentKindJSON)
	documentSignalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "bill of lading", "", signalsd.ContentKindDocument)

	// a json signal type registered without a schema
	unvalidatedSignalType, err := testEnv.queries.CreateSignalType(ctx, database.CreateSignalTypeParams{
		Slug:          "unvalidated-consignment",
		SchemaURL:     signalsd.SkipValidationURL,
		ReadmeURL:     testReadmeURL,
		Title:         "unvalidated consignment",
		Detail:        testSignalTypeDetail,
		SemVer:        "1.0.0",
		SchemaContent: "{}",
		ContentKind:   signalsd.ContentKindJSON,
	})
	if err != nil {
		t.Fatalf("Failed to create the signal type without a schema: %v", err)
	}

	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)

	t.Run("unknown signal type version is not found", func(t *testing.T) {
		response := refreshSignalTypeSchemaRequest(t, testEnv.baseURL, siteAdminToken, jsonSignalType.Slug, "9.9.9")
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
	})

	t.Run("schemas can't be refreshed for document signal types", func(t *testing.T) {
		response := refreshSignalTypeSchemaRequest(t, testEnv.baseURL, siteAdminToken, documentSignalType.Slug, documentSignalType.SemVer)
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})

	t.Run("signal types registered without a schema can't be refreshed", func(t *testing.T) {
		response := refreshSignalTypeSchemaRequest(t, testEnv.baseURL, siteAdminToken, unvalidatedSignalType.Slug, unvalidatedSignalType.SemVer)
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest)
	})
}

// TestCheckRecentSignals checks the recent versions of the signal type's signals are checked against the schema -
// including previous versions and withdrawn signals - and that the signals of other signal types are not
func TestCheckRecentSignals(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@recent.com")
	writerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "writer@recent.com")

	isn := createTestISN(t, ctx, testEnv.queries, "recent-isn", "Check recent signals ISN", adminAccount.ID, "private")
	signalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "consignment", "", signalsd.ContentKindJSON)
	grantPermission(t, ctx, testEnv.queries, isn.ID, writerAccount.ID, "write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	writerToken := testEnv.getAccessToken(t, writerAccount.ID)
	endpoint := newTestSignalEndpoint(isn, signalType)

	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayloadWithContent("ref-001", "one"), writerToken, endpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayloadWithContent("ref-002", "two"), writerToken, endpoint)
	submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayloadWithContent("ref-003", "three"), writerToken, endpoint)

	// schema works with both signals
	validRefreshSchema := `{"type": "object", "properties": {"test": {"type": "string", "enum": ["one", "two", "three"]}}, "required": ["test"], "additionalProperties": false}`

	// fails with the 3rd signal
	invalidRefreshSchema := `{"type": "object", "properties": {"test": {"type": "string", "enum": ["one", "two"]}}, "required": ["test"], "additionalProperties": false}`

	tests := []struct {
		name                 string
		schemaContent        string
		expectedFailureCount int
		expectedFailedRef    string
	}{
		{
			name:          "refresh works with a compatible schema",
			schemaContent: validRefreshSchema,
		},
		{
			name:                 "refresh fails with an incompatible schemea",
			schemaContent:        invalidRefreshSchema,
			expectedFailureCount: 1,
			expectedFailedRef:    "ref-003",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, err := schemas.ValidateAndCompileSchema("https://github.com/test/test/blob/main/updated.json", tt.schemaContent)
			if err != nil {
				t.Fatalf("Failed to compile schema: %v", err)
			}

			check, err := schemas.CheckRecentSignals(ctx, testEnv.queries, signalType.ID, schema)
			if err != nil {
				t.Fatalf("Failed to check recent signals: %v", err)
			}

			// 3 versions of the consignment signal type (the shipment signal is not checked)
			if check.SignalsChecked != 3 {
				t.Errorf("Expected 3 signal versions checked, got %d", check.SignalsChecked)
			}
			if check.FailureCount != tt.expectedFailureCount {
				t.Fatalf("Expected %d failures, got %d: %+v", tt.expectedFailureCount, check.FailureCount, check.Failures)
			}
			if len(check.Failures) != tt.expectedFailureCount {
				t.Errorf("Expected %d reported failures, got %d", tt.expectedFailureCount, len(check.Failures))
			}
			if tt.expectedFailedRef != "" {
				failure := check.Failures[0]
				if failure.LocalRef != tt.expectedFailedRef {
					t.Errorf("Expected %s to fail, got %s ", tt.expectedFailedRef, failure.LocalRef)
				}
			}
		})
	}

}

// refreshSignalTypeSchemaRequest calls the refresh schema endpoint: POST /api/admin/signal-types/{signal_type_slug}/v{sem_ver}/schema/refresh
func refreshSignalTypeSchemaRequest(t *testing.T, baseURL, token, slug, semVer string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/api/admin/signal-types/%s/v%s/schema/refresh", baseURL, slug, semVer)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to refresh signal type schema: %v", err)
	}
	return response
}
