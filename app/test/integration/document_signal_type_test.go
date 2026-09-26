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
	"fmt"
	"io"
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
	siteAdminToken := testEnv.createAuthToken(t, siteAdminAccount.ID)

	t.Run("create document signal type", func(t *testing.T) {
		response := postAdminJSON(t, testEnv.baseURL+"/api/admin/signal-types", siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:       "Bill of Lading",
			BumpType:    "major",
			ReadmeURL:   signalsd.SkipReadmeURL,
			Detail:      "bill of lading documents",
			ContentKind: signalsd.ContentKindDocument,
		})
		expectStatus(t, response, http.StatusCreated)

		signalType := getAdminSignalType(t, testEnv.baseURL, siteAdminToken, "bill-of-lading", "1.0.0")
		if signalType.ContentKind != signalsd.ContentKindDocument {
			t.Errorf("Expected content_kind %s, got %s", signalsd.ContentKindDocument, signalType.ContentKind)
		}
		if signalType.SchemaURL != "" {
			t.Errorf("Expected no schema_url for a document signal type, got %s", signalType.SchemaURL)
		}
	})

	t.Run("content kind defaults to json", func(t *testing.T) {
		response := postAdminJSON(t, testEnv.baseURL+"/api/admin/signal-types", siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:     "Consignment",
			BumpType:  "major",
			SchemaURL: signalsd.SkipValidationURL,
			ReadmeURL: signalsd.SkipReadmeURL,
			Detail:    "consignment records",
		})
		expectStatus(t, response, http.StatusCreated)

		signalType := getAdminSignalType(t, testEnv.baseURL, siteAdminToken, "consignment", "1.0.0")
		if signalType.ContentKind != signalsd.ContentKindJSON {
			t.Errorf("Expected content_kind %s, got %s", signalsd.ContentKindJSON, signalType.ContentKind)
		}
	})

	t.Run("json signal type requires a schema_url", func(t *testing.T) {
		response := postAdminJSON(t, testEnv.baseURL+"/api/admin/signal-types", siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:       "No schema",
			BumpType:    "major",
			ReadmeURL:   signalsd.SkipReadmeURL,
			Detail:      "json signal type without a schema",
			ContentKind: signalsd.ContentKindJSON,
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})

	t.Run("document signal type rejects a schema_url", func(t *testing.T) {
		response := postAdminJSON(t, testEnv.baseURL+"/api/admin/signal-types", siteAdminToken, handlers.CreateSignalTypeRequest{
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
		response := postAdminJSON(t, testEnv.baseURL+"/api/admin/signal-types", siteAdminToken, handlers.CreateSignalTypeRequest{
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
		response := postAdminJSON(t, testEnv.baseURL+"/api/admin/signal-types/bill-of-lading/schemas", siteAdminToken, handlers.RegisterNewSignalTypeSchemaRequest{
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

	writerToken := testEnv.createAuthToken(t, writerAccount.ID)

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
		signals := searchSignalsWithParams(t, testEnv.baseURL, endpoint, false, writerToken, http.StatusOK, map[string]string{
			"local_ref": "json-to-document-001",
		})
		if len(signals) != 0 {
			t.Errorf("Expected 0 signals, got %d", len(signals))
		}
	})
}

// postAdminJSON posts a JSON body with authentication and returns the response
func postAdminJSON(t *testing.T, url string, token string, body any) *http.Response {
	t.Helper()

	jsonData, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	return response
}

// getAdminSignalType finds a signal type in the admin signal types list
func getAdminSignalType(t *testing.T, baseURL, token, slug, semVer string) handlers.SignalTypeDetail {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/admin/signal-types", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 getting signal types, got %d", response.StatusCode)
	}

	var signalTypes []handlers.SignalTypeDetail
	if err := json.NewDecoder(response.Body).Decode(&signalTypes); err != nil {
		t.Fatalf("Failed to decode signal types: %v", err)
	}
	for _, signalType := range signalTypes {
		if signalType.Slug == slug && signalType.SemVer == semVer {
			return signalType
		}
	}
	t.Fatalf("signal type %s/v%s not found", slug, semVer)
	return handlers.SignalTypeDetail{}
}

// expectStatus checks the response status and closes the body
func expectStatus(t *testing.T, response *http.Response, expectedStatus int) {
	t.Helper()
	defer response.Body.Close()

	if response.StatusCode != expectedStatus {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("Expected status %d, got %d: %s", expectedStatus, response.StatusCode, body)
	}
}

// expectErrorCode checks the response status and error code and closes the body
func expectErrorCode(t *testing.T, response *http.Response, expectedStatus int, expectedCode apperrors.ErrorCode) {
	t.Helper()
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != expectedStatus {
		t.Fatalf("Expected status %d, got %d: %s", expectedStatus, response.StatusCode, body)
	}

	var errorResponse map[string]any
	if err := json.Unmarshal(body, &errorResponse); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}
	if errorResponse["error_code"] != string(expectedCode) {
		t.Errorf("Expected error_code %s, got %v (%s)", expectedCode, errorResponse["error_code"], fmt.Sprint(errorResponse["message"]))
	}
}
