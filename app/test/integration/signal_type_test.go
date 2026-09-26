//go:build integration

package integration

// Signal type API tests:
//
// - TestGetSignalType: GET /api/admin/signal-types/{signal_type_slug}/v{sem_ver} (site-wide signal type details)
// - TestIsnSignalTypes: the signal types that have been added to an ISN
//     GET /api/isn/{isn_slug}/signal-types
//     GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}
//     GET /api/isn/{isn_slug} (signal_types field)

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// TestGetSignalType checks site admins can get the details of a signal type and other accounts can't
func TestGetSignalType(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@get-signal-type.com")
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "isnadmin", "user", "admin@get-signal-type.com")

	isn := createTestISN(t, ctx, testEnv.queries, "get-signal-type-isn", "Get signal type ISN", adminAccount.ID, "private")
	jsonSignalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "json signal type", "", signalsd.ContentKindJSON)
	documentSignalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "document signal type", "", signalsd.ContentKindDocument)

	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)
	adminToken := testEnv.getAccessToken(t, adminAccount.ID)

	t.Run("site admin can get a json signal type", func(t *testing.T) {
		response := getSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, jsonSignalType.Slug, jsonSignalType.SemVer)
		signalType := expectJSONResponse(t, response, http.StatusOK)

		expectedFields := map[string]any{
			"slug":         jsonSignalType.Slug,
			"sem_ver":      jsonSignalType.SemVer,
			"title":        jsonSignalType.Title,
			"schema_url":   testSchemaURL,
			"content_kind": signalsd.ContentKindJSON,
		}
		for field, expected := range expectedFields {
			if signalType[field] != expected {
				t.Errorf("Expected %s %v, got %v", field, expected, signalType[field])
			}
		}
	})

	t.Run("site admin can get a document signal type", func(t *testing.T) {
		response := getSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, documentSignalType.Slug, documentSignalType.SemVer)
		signalType := expectJSONResponse(t, response, http.StatusOK)

		if signalType["content_kind"] != signalsd.ContentKindDocument {
			t.Errorf("Expected content_kind %s, got %v", signalsd.ContentKindDocument, signalType["content_kind"])
		}
		// document signal types have no schema - the skip validation URL is not shown
		if signalType["schema_url"] != "" {
			t.Errorf("Expected no schema_url, got %v", signalType["schema_url"])
		}
	})

	t.Run("unknown signal type version is not found", func(t *testing.T) {
		response := getSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, jsonSignalType.Slug, "9.9.9")
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
	})

	t.Run("accounts that are not site admins are forbidden", func(t *testing.T) {
		response := getSignalTypeRequest(t, testEnv.baseURL, adminToken, jsonSignalType.Slug, jsonSignalType.SemVer)
		expectErrorCode(t, response, http.StatusForbidden, apperrors.ErrCodeForbidden)
	})

	t.Run("unauthenticated requests are rejected", func(t *testing.T) {
		response := getSignalTypeRequest(t, testEnv.baseURL, "", jsonSignalType.Slug, jsonSignalType.SemVer)
		expectErrorCode(t, response, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure)
	})
}

// TestIsnSignalTypes checks any authenticated account can see the signal types that have been added to an ISN,
// and whether each one is enabled on the ISN
func TestIsnSignalTypes(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@isn-signal-types.com")
	// the member has not been granted access to any ISN
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@isn-signal-types.com")

	// the ISN has a json signal type, a document signal type and a signal type that is disabled on the ISN
	isn := createTestISN(t, ctx, testEnv.queries, "isn-signal-types-isn", "ISN signal types ISN", siteAdminAccount.ID, "private")
	jsonSignalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "consignment", "", signalsd.ContentKindJSON)
	documentSignalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "bill of lading", "", signalsd.ContentKindDocument)
	disabledSignalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "packing list", "", signalsd.ContentKindDocument)
	_, err := testEnv.queries.UpdateIsnSignalTypeStatus(ctx, database.UpdateIsnSignalTypeStatusParams{
		IsnID:        isn.ID,
		SignalTypeID: disabledSignalType.ID,
		IsInUse:      false,
	})
	if err != nil {
		t.Fatalf("Failed to disable the signal type on the ISN: %v", err)
	}

	// a signal type that has only been added to another ISN
	otherISN := createTestISN(t, ctx, testEnv.queries, "isn-signal-types-other-isn", "ISN signal types other ISN", siteAdminAccount.ID, "private")
	otherSignalType := createTestSignalType(t, ctx, testEnv.queries, otherISN.ID, "other ISN signal type", "", signalsd.ContentKindJSON)

	memberToken := testEnv.getAccessToken(t, memberAccount.ID)

	t.Run("list returns the signal types that are enabled on the ISN", func(t *testing.T) {
		response := getIsnSignalTypesRequest(t, testEnv.baseURL, memberToken, isn.Slug, "")
		signalTypes := expectSignalTypeList(t, response)

		if len(signalTypes) != 2 {
			t.Fatalf("Expected 2 signal types, got %d: %v", len(signalTypes), signalTypes)
		}
		// the content kind each signal type was created with
		expectedContentKinds := map[string]string{
			jsonSignalType.Slug:     jsonSignalType.ContentKind,
			documentSignalType.Slug: documentSignalType.ContentKind,
		}
		for _, signalType := range signalTypes {
			slug := signalType["slug"].(string)
			expectedContentKind, ok := expectedContentKinds[slug]
			if !ok {
				t.Errorf("Unexpected signal type %s", slug)
				continue
			}
			if signalType["content_kind"] != expectedContentKind {
				t.Errorf("Expected %s content_kind %s, got %v", slug, expectedContentKind, signalType["content_kind"])
			}
			if signalType["is_in_use"] != true {
				t.Errorf("Expected %s is_in_use true, got %v", slug, signalType["is_in_use"])
			}
		}
	})

	t.Run("list includes disabled signal types with include_inactive=true", func(t *testing.T) {
		response := getIsnSignalTypesRequest(t, testEnv.baseURL, memberToken, isn.Slug, "include_inactive=true")
		signalTypes := expectSignalTypeList(t, response)

		if len(signalTypes) != 3 {
			t.Fatalf("Expected 3 signal types, got %d: %v", len(signalTypes), signalTypes)
		}
		for _, signalType := range signalTypes {
			expectedInUse := signalType["slug"] != disabledSignalType.Slug
			if signalType["is_in_use"] != expectedInUse {
				t.Errorf("Expected %v is_in_use %v, got %v", signalType["slug"], expectedInUse, signalType["is_in_use"])
			}
		}
	})

	t.Run("list for an unknown ISN is not found", func(t *testing.T) {
		response := getIsnSignalTypesRequest(t, testEnv.baseURL, memberToken, "no-such-isn", "")
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
	})

	t.Run("list requires authentication", func(t *testing.T) {
		response := getIsnSignalTypesRequest(t, testEnv.baseURL, "", isn.Slug, "")
		expectErrorCode(t, response, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure)
	})

	t.Run("get returns a signal type that has been added to the ISN", func(t *testing.T) {
		response := getIsnSignalTypeRequest(t, testEnv.baseURL, memberToken, isn.Slug, documentSignalType.Slug, documentSignalType.SemVer)
		signalType := expectJSONResponse(t, response, http.StatusOK)

		if signalType["content_kind"] != signalsd.ContentKindDocument {
			t.Errorf("Expected content_kind %s, got %v", signalsd.ContentKindDocument, signalType["content_kind"])
		}
		if signalType["is_in_use"] != true {
			t.Errorf("Expected is_in_use true, got %v", signalType["is_in_use"])
		}
	})

	t.Run("get returns a signal type that is disabled on the ISN", func(t *testing.T) {
		response := getIsnSignalTypeRequest(t, testEnv.baseURL, memberToken, isn.Slug, disabledSignalType.Slug, disabledSignalType.SemVer)
		signalType := expectJSONResponse(t, response, http.StatusOK)

		if signalType["is_in_use"] != false {
			t.Errorf("Expected is_in_use false, got %v", signalType["is_in_use"])
		}
	})

	t.Run("get for a signal type that has not been added to the ISN is not found", func(t *testing.T) {
		response := getIsnSignalTypeRequest(t, testEnv.baseURL, memberToken, isn.Slug, otherSignalType.Slug, otherSignalType.SemVer)
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
	})

	t.Run("get for an unknown ISN is not found", func(t *testing.T) {
		response := getIsnSignalTypeRequest(t, testEnv.baseURL, memberToken, "no-such-isn", jsonSignalType.Slug, jsonSignalType.SemVer)
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
	})

	t.Run("ISN details include the content kind of each signal type and hide placeholder schema URLs", func(t *testing.T) {
		response := getIsnRequest(t, testEnv.baseURL, memberToken, isn.Slug)
		isnDetails := expectJSONResponse(t, response, http.StatusOK)

		signalTypes, _ := isnDetails["signal_types"].([]any)
		if len(signalTypes) != 3 {
			t.Fatalf("Expected 3 signal types, got %d", len(signalTypes))
		}
		for _, st := range signalTypes {
			signalType := st.(map[string]any)
			contentKind, _ := signalType["content_kind"].(string)
			if !signalsd.ValidContentKinds[contentKind] {
				t.Errorf("Expected a valid content_kind for %v, got %q", signalType["slug"], contentKind)
			}
			// document signal types have no schema - the skip validation URL is not shown
			if contentKind == signalsd.ContentKindDocument && signalType["schema_url"] != "" {
				t.Errorf("Expected no schema_url for %v, got %v", signalType["slug"], signalType["schema_url"])
			}
		}
	})
}

// getIsnSignalTypesRequest gets the signal types added to the ISN: GET /api/isn/{isn_slug}/signal-types?{query}
func getIsnSignalTypesRequest(t *testing.T, baseURL, token, isnSlug, query string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/api/isn/%s/signal-types", baseURL, isnSlug)
	if query != "" {
		url += "?" + query
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get ISN signal types: %v", err)
	}
	return response
}

// getIsnSignalTypeRequest gets a signal type added to the ISN: GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}
func getIsnSignalTypeRequest(t *testing.T, baseURL, token, isnSlug, signalTypeSlug, semVer string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s", baseURL, isnSlug, signalTypeSlug, semVer)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get ISN signal type: %v", err)
	}
	return response
}

// getIsnRequest gets the ISN details: GET /api/isn/{isn_slug}
func getIsnRequest(t *testing.T, baseURL, token, isnSlug string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/isn/%s", baseURL, isnSlug), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get ISN: %v", err)
	}
	return response
}

// expectSignalTypeList checks the response status is 200, closes the body and returns the decoded list of signal types
func expectSignalTypeList(t *testing.T, response *http.Response) []map[string]any {
	t.Helper()
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", response.StatusCode, body)
	}

	var signalTypes []map[string]any
	if err := json.Unmarshal(body, &signalTypes); err != nil {
		t.Fatalf("Failed to decode signal types: %v: %s", err, body)
	}
	return signalTypes
}
