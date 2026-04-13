//go:build integration

package integration

// Integration tests for the ISN routing config handler.
//
// PUT /api/admin/signal-types/{slug}/v{semver}/routes  - create/replace routing config
// GET /api/admin/signal-types/{slug}/v{semver}/routes  - retrieve routing config
// DELETE /api/admin/signal-types/{slug}/v{semver}/routes - remove routing config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
)

// setRoutingConfig updates routing config via the admin API and fatals on failure.
// Use this for test setup where you just need the config in place.
func setRoutingConfig(t *testing.T, env *testEnv, token string, st database.SignalType, body handlers.UpdateSignalRoutingConfigRequest) {
	t.Helper()
	resp := updateSignalRoutingConfig(t, env.baseURL, token, st.Slug, st.SemVer, body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("setRoutingConfig: want 204, got %d", resp.StatusCode)
	}
}

// updateSignalRoutingConfig sends a PUT to the routing config endpoint and returns the raw response.
// Use this for tests that need to assert specific status codes or error bodies.
func updateSignalRoutingConfig(t *testing.T, baseURL, token, slug, semVer string, body any) *http.Response {
	t.Helper()
	jsonData, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, isnRoutesURL(baseURL, slug, semVer), bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	return resp
}

func getSignalRoutingConfig(t *testing.T, baseURL, token, slug, semVer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, isnRoutesURL(baseURL, slug, semVer), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	return resp
}

func deleteSignalRoutingConfig(t *testing.T, baseURL, token, slug, semVer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, isnRoutesURL(baseURL, slug, semVer), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	return resp
}

func isnRoutesURL(baseURL, slug, semVer string) string {
	return fmt.Sprintf("%s/api/admin/signal-types/%s/v%s/routes", baseURL, slug, semVer)
}

func TestSignalRoutingConfig(t *testing.T) {
	ctx := context.Background()
	testEnv := startInProcessServer(t, "")

	// create test accounts
	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@isn-routes-test.com")
	memberAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "member@isn-routes-test.com")

	siteAdminToken := getAccessToken(t, testEnv.authService, siteAdminAccount.ID)
	memberToken := getAccessToken(t, testEnv.authService, memberAccount.ID)

	// isn and signal type
	isn := createTestISN(t, ctx, testEnv.queries, "test-isn", "Test ISN", siteAdminAccount.ID, "private")
	signalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "test signal type", "")

	slug := signalType.Slug
	semVer := signalType.SemVer

	// testSchemaContent only has a "test" string field at the root level
	validRequest := handlers.UpdateSignalRoutingConfigRequest{
		RoutingField: "test",
		RoutingRules: []handlers.SignalRoutingRule{
			{
				MatchPattern: "*value*",
				Operator:     "matches",
				IsnSlug:      isn.Slug,
				Sequence:     1,
			},
		},
	}

	t.Run("Update", func(t *testing.T) {
		testCases := []struct {
			name           string
			token          string
			slug           string
			semVer         string
			body           any
			expectedStatus int
		}{
			{
				name:           "siteadmin_creates_routing_config",
				token:          siteAdminToken,
				slug:           slug,
				semVer:         semVer,
				body:           validRequest,
				expectedStatus: http.StatusNoContent,
			},
			{
				name:           "siteadmin_replaces_existing_config",
				token:          siteAdminToken,
				slug:           slug,
				semVer:         semVer,
				body:           validRequest,
				expectedStatus: http.StatusNoContent,
			},
			{
				name:           "member_cannot_update",
				token:          memberToken,
				slug:           slug,
				semVer:         semVer,
				body:           validRequest,
				expectedStatus: http.StatusForbidden,
			},
			{
				name:   "empty_routing_field",
				token:  siteAdminToken,
				slug:   slug,
				semVer: semVer,
				body: handlers.UpdateSignalRoutingConfigRequest{
					RoutingField: "",
					RoutingRules: validRequest.RoutingRules,
				},
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:   "routing_field_with_gjson_wildcard",
				token:  siteAdminToken,
				slug:   slug,
				semVer: semVer,
				body: handlers.UpdateSignalRoutingConfigRequest{
					RoutingField: "test*",
					RoutingRules: validRequest.RoutingRules,
				},
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:   "empty_routes",
				token:  siteAdminToken,
				slug:   slug,
				semVer: semVer,
				body: handlers.UpdateSignalRoutingConfigRequest{
					RoutingField: "test",
					RoutingRules: []handlers.SignalRoutingRule{},
				},
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:   "invalid_operator",
				token:  siteAdminToken,
				slug:   slug,
				semVer: semVer,
				body: handlers.UpdateSignalRoutingConfigRequest{
					RoutingField: "test",
					RoutingRules: []handlers.SignalRoutingRule{
						{MatchPattern: "*Felixstowe*", Operator: "like", IsnSlug: isn.Slug, Sequence: 1},
					},
				},
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:   "missing_match_pattern",
				token:  siteAdminToken,
				slug:   slug,
				semVer: semVer,
				body: handlers.UpdateSignalRoutingConfigRequest{
					RoutingField: "test",
					RoutingRules: []handlers.SignalRoutingRule{
						{MatchPattern: "", Operator: "matches", IsnSlug: isn.Slug, Sequence: 1},
					},
				},
				expectedStatus: http.StatusBadRequest,
			},
			{
				name:   "unknown_isn_slug",
				token:  siteAdminToken,
				slug:   slug,
				semVer: semVer,
				body: handlers.UpdateSignalRoutingConfigRequest{
					RoutingField: "test",
					RoutingRules: []handlers.SignalRoutingRule{
						{MatchPattern: "*Felixstowe*", Operator: "matches", IsnSlug: "no-such-isn", Sequence: 1},
					},
				},
				expectedStatus: http.StatusNotFound,
			},
			{
				name:           "unknown_signal_type",
				token:          siteAdminToken,
				slug:           "no-such-type",
				semVer:         "",
				body:           validRequest,
				expectedStatus: http.StatusNotFound,
			},
		}

		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				resp := updateSignalRoutingConfig(t, testEnv.baseURL, tt.token, tt.slug, tt.semVer, tt.body)
				defer resp.Body.Close()
				if resp.StatusCode != tt.expectedStatus {
					t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
				}
			})
		}
	})

	t.Run("Get", func(t *testing.T) {
		// ensure a config exists
		setRoutingConfig(t, testEnv, siteAdminToken, signalType, validRequest)

		testCases := []struct {
			name           string
			token          string
			slug           string
			semVer         string
			expectedStatus int
		}{
			{
				name:           "siteadmin_gets_routing_config",
				token:          siteAdminToken,
				slug:           slug,
				semVer:         semVer,
				expectedStatus: http.StatusOK,
			},
			{
				name:           "member_cannot_get",
				token:          memberToken,
				slug:           slug,
				semVer:         semVer,
				expectedStatus: http.StatusForbidden,
			},
			{
				name:           "unknown_signal_type",
				token:          siteAdminToken,
				slug:           "no-such-type",
				semVer:         "",
				expectedStatus: http.StatusNotFound,
			},
		}

		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				resp := getSignalRoutingConfig(t, testEnv.baseURL, tt.token, tt.slug, tt.semVer)
				defer resp.Body.Close()
				if resp.StatusCode != tt.expectedStatus {
					t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
				}
				if tt.expectedStatus != http.StatusOK {
					return
				}
				var result handlers.SignalRoutingConfigResponse
				if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if result.RoutingField != validRequest.RoutingField {
					t.Errorf("routing_field: got %q, want %q", result.RoutingField, validRequest.RoutingField)
				}
				if len(result.RoutingRules) != 1 {
					t.Fatalf("Expected 1 route, got %d", len(result.RoutingRules))
				}
				if result.RoutingRules[0].MatchPattern != validRequest.RoutingRules[0].MatchPattern {
					t.Errorf("match_pattern: got %q, want %q", result.RoutingRules[0].MatchPattern, validRequest.RoutingRules[0].MatchPattern)
				}
			})
		}
	})

	t.Run("Delete", func(t *testing.T) {
		t.Run("removes_routing_config", func(t *testing.T) {
			setRoutingConfig(t, testEnv, siteAdminToken, signalType, validRequest)

			resp := deleteSignalRoutingConfig(t, testEnv.baseURL, siteAdminToken, slug, semVer)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("Expected status %d, got %d", http.StatusNoContent, resp.StatusCode)
			}

			getResp := getSignalRoutingConfig(t, testEnv.baseURL, siteAdminToken, slug, semVer)
			defer getResp.Body.Close()
			if getResp.StatusCode != http.StatusNotFound {
				t.Errorf("Expected 404 after delete, got %d", getResp.StatusCode)
			}
		})

		testCases := []struct {
			name           string
			token          string
			slug           string
			semVer         string
			expectedStatus int
		}{
			{
				name:           "deleting_non_existent_config",
				token:          siteAdminToken,
				slug:           "no-such-type",
				semVer:         "",
				expectedStatus: http.StatusNotFound,
			},
			{
				name:           "member_cannot_delete",
				token:          memberToken,
				slug:           slug,
				semVer:         semVer,
				expectedStatus: http.StatusForbidden,
			},
		}

		for _, tt := range testCases {
			t.Run(tt.name, func(t *testing.T) {
				resp := deleteSignalRoutingConfig(t, testEnv.baseURL, tt.token, tt.slug, tt.semVer)
				defer resp.Body.Close()
				if resp.StatusCode != tt.expectedStatus {
					t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
				}
			})
		}
	})
}
