//go:build integration

package integration

// Tests for document signal types (content_kind = 'document'):
// - creating document signal types via the admin API
// - schemas can't be registered for document signal types
// - document signal types are rejected by the json signal endpoints

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
)

func TestDocumentSignalTypes(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@document-types.com")
	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)

	t.Run("create document signal type", func(t *testing.T) {
		response := createSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:       "Bill of Lading",
			BumpType:    "major",
			ReadmeURL:   signalsd.SkipReadmeURL,
			Detail:      "bill of lading documents",
			ContentKind: signalsd.ContentKindDocument,
		})
		expectStatus(t, response, http.StatusCreated)

		signalType := expectJSONResponse(t, getSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, "bill-of-lading", "1.0.0"), http.StatusOK)
		if signalType["content_kind"] != signalsd.ContentKindDocument {
			t.Errorf("Expected content_kind %s, got %v", signalsd.ContentKindDocument, signalType["content_kind"])
		}
		if signalType["schema_url"] != "" {
			t.Errorf("Expected no schema_url for a document signal type, got %v", signalType["schema_url"])
		}
	})

	t.Run("content kind defaults to json", func(t *testing.T) {
		response := createSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:     "Consignment",
			BumpType:  "major",
			SchemaURL: signalsd.SkipValidationURL,
			ReadmeURL: signalsd.SkipReadmeURL,
			Detail:    "consignment records",
		})
		expectStatus(t, response, http.StatusCreated)

		signalType := expectJSONResponse(t, getSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, "consignment", "1.0.0"), http.StatusOK)
		if signalType["content_kind"] != signalsd.ContentKindJSON {
			t.Errorf("Expected content_kind %s, got %v", signalsd.ContentKindJSON, signalType["content_kind"])
		}
	})

	t.Run("json signal type requires a schema_url", func(t *testing.T) {
		response := createSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:       "No schema",
			BumpType:    "major",
			ReadmeURL:   signalsd.SkipReadmeURL,
			Detail:      "json signal type without a schema",
			ContentKind: signalsd.ContentKindJSON,
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})

	t.Run("document signal type rejects a schema_url", func(t *testing.T) {
		response := createSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:       "Commercial Invoice",
			BumpType:    "major",
			SchemaURL:   testSchemaURL,
			ReadmeURL:   signalsd.SkipReadmeURL,
			Detail:      "commercial invoice documents",
			ContentKind: signalsd.ContentKindDocument,
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})

	t.Run("unknown content kind is rejected", func(t *testing.T) {
		response := createSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:       "Packing List",
			BumpType:    "major",
			SchemaURL:   signalsd.SkipValidationURL,
			ReadmeURL:   signalsd.SkipReadmeURL,
			Detail:      "packing lists",
			ContentKind: "xml",
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})

	t.Run("schemas can't be registered for document signal types", func(t *testing.T) {
		response := registerSignalTypeSchemaRequest(t, testEnv.baseURL, siteAdminToken, "bill-of-lading", handlers.RegisterNewSignalTypeSchemaRequest{
			SchemaURL: signalsd.SkipValidationURL,
			BumpType:  "minor",
			ReadmeURL: signalsd.SkipReadmeURL,
			Detail:    "new version",
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})
}

func TestJSONEndpointsRejectDocumentSignalTypes(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "admin@document-rejection.com")
	writerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "writer@document-rejection.com")

	isn := createTestISN(t, ctx, testEnv.queries, "document-rejection-isn", "Document rejection ISN", adminAccount.ID, "private")
	documentType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "bill of lading", "", signalsd.ContentKindDocument)
	grantPermission(t, ctx, testEnv.queries, isn.ID, writerAccount.ID, "write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	writerToken := testEnv.getAccessToken(t, writerAccount.ID)

	endpoint := testSignalEndpoint{
		isnSlug:          isn.Slug,
		signalTypeSlug:   documentType.Slug,
		signalTypeSemVer: documentType.SemVer,
	}

	t.Run("create signals endpoint", func(t *testing.T) {
		response := submitCreateSignalRequest(t, testEnv.baseURL, createValidSignalPayload("json-to-document-001"), writerToken, endpoint)
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam)
	})

	t.Run("signal router endpoint", func(t *testing.T) {
		response := submitRouteSignalsRequest(t, testEnv.baseURL, createValidSignalPayload("json-to-document-002"), writerToken, documentType.Slug, documentType.SemVer)
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam)
	})

	t.Run("search works for document signal types", func(t *testing.T) {
		// no signals can be created yet, but the search should work for document signal types
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, endpoint, writerToken, map[string]string{
			"local_ref": "json-to-document-001",
		}))
		if len(signals) != 0 {
			t.Errorf("Expected 0 signals, got %d", len(signals))
		}
	})
}

// createSignalTypeRequest posts to the create signal type endpoint: POST /api/admin/signal-types
func createSignalTypeRequest(t *testing.T, baseURL, token string, request handlers.CreateSignalTypeRequest) *http.Response {
	t.Helper()

	jsonData, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/admin/signal-types", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to create signal type: %v", err)
	}
	return response
}

// registerSignalTypeSchemaRequest posts to the register new schema endpoint: POST /api/admin/signal-types/{slug}/schemas
func registerSignalTypeSchemaRequest(t *testing.T, baseURL, token, slug string, request handlers.RegisterNewSignalTypeSchemaRequest) *http.Response {
	t.Helper()

	jsonData, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/admin/signal-types/"+slug+"/schemas", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to register signal type schema: %v", err)
	}
	return response
}
