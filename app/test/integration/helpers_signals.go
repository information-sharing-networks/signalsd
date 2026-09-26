//go:build integration

package integration

// Signal helpers shared by the signal tests: building payloads, submitting (directly and via the signal router),
// withdrawing and searching signals, reading the responses, getting signal types and setting up signal routing.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
)

// testSignalEndpoint identifies a signal type on an ISN: /api/isn/{isnSlug}/signal-types/{signalTypeSlug}/v{signalTypeSemVer}
type testSignalEndpoint struct {
	isnSlug          string
	signalTypeSlug   string
	signalTypeSemVer string
}

func newTestSignalEndpoint(isn database.Isn, signalType database.SignalType) testSignalEndpoint {
	return testSignalEndpoint{
		isnSlug:          isn.Slug,
		signalTypeSlug:   signalType.Slug,
		signalTypeSemVer: signalType.SemVer,
	}
}

// createValidSignalPayload creates a payload with one signal that is valid for the test schema
// (https://github.com/information-sharing-networks/signal-library/blob/main/signalsd-testing/simple.json)
func createValidSignalPayload(localRef string) map[string]any {
	return map[string]any{
		"batch_ref": "test-batch",
		"signals": []map[string]any{
			{
				"local_ref": localRef,
				"content":   map[string]any{"test": "valid content for simple schema"},
			},
		},
	}
}

// createValidSignalPayloadWithCorrelatedID creates a payload with one valid signal that is correlated with correlationID
func createValidSignalPayloadWithCorrelatedID(localRef string, correlationID string) map[string]any {
	return map[string]any{
		"batch_ref": "test-batch",
		"signals": []map[string]any{
			{
				"local_ref":      localRef,
				"correlation_id": correlationID,
				"content":        map[string]any{"test": "valid content for simple schema"},
			},
		},
	}
}

// submitCreateSignalRequest posts a signals payload to the endpoint (token can be empty to test unauthenticated requests)
func submitCreateSignalRequest(t *testing.T, baseURL string, payload map[string]any, token string, endpoint testSignalEndpoint) *http.Response {
	t.Helper()

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/signals",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to submit signals: %v", err)
	}
	return response
}

// expectSubmissionResponse checks the status of a create signals response, closes the body and returns the decoded response
func expectSubmissionResponse(t *testing.T, response *http.Response, expectedStatus int) handlers.SignalSubmissionResponse {
	t.Helper()
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != expectedStatus {
		t.Fatalf("Expected status %d, got %d: %s", expectedStatus, response.StatusCode, body)
	}

	var submission handlers.SignalSubmissionResponse
	if err := json.Unmarshal(body, &submission); err != nil {
		t.Fatalf("Failed to decode create signals response: %v: %s", err, body)
	}
	return submission
}

// submitSignalAndGetID submits a payload containing a single signal, checks it was stored and returns its signal ID
func submitSignalAndGetID(t *testing.T, baseURL string, payload map[string]any, token string, endpoint testSignalEndpoint) string {
	t.Helper()

	submission := expectSubmissionResponse(t, submitCreateSignalRequest(t, baseURL, payload, token, endpoint), http.StatusOK)
	if len(submission.Results) != 1 || len(submission.Results[0].StoredSignals) != 1 {
		t.Fatalf("Expected 1 stored signal, got %+v", submission)
	}
	return submission.Results[0].StoredSignals[0].SignalID.String()
}

// withdrawSignal withdraws a signal by local reference
func withdrawSignal(t *testing.T, baseURL string, endpoint testSignalEndpoint, token, localRef string) *http.Response {
	t.Helper()

	jsonData, err := json.Marshal(map[string]string{"local_ref": localRef})
	if err != nil {
		t.Fatalf("Failed to marshal withdrawal request: %v", err)
	}

	url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/signals/withdraw",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer)

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create withdrawal request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to withdraw signal: %v", err)
	}
	return response
}

// searchPrivateSignals calls the private ISN search endpoint with the supplied query params
func searchPrivateSignals(t *testing.T, baseURL string, endpoint testSignalEndpoint, token string, params map[string]string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/signals/search",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	q := req.URL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to search private signals: %v", err)
	}
	return response
}

// searchPublicSignals calls the public ISN search endpoint (no authentication) with the supplied query params
func searchPublicSignals(t *testing.T, baseURL string, endpoint testSignalEndpoint, params map[string]string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/api/public/isn/%s/signal-types/%s/v%s/signals/search",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	q := req.URL.Query()
	for k, v := range params {
		q.Add(k, v)
	}
	req.URL.RawQuery = q.Encode()

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to search public signals: %v", err)
	}
	return response
}

// lastHourSearchParams returns search params that match every signal created in the last hour.
// Use this when a test needs all the signals of a type (the search endpoints require at least one filter).
func lastHourSearchParams() map[string]string {
	now := time.Now()
	return map[string]string{
		"start_date": now.Add(-1 * time.Hour).Format(time.RFC3339),
		"end_date":   now.Add(1 * time.Hour).Format(time.RFC3339),
	}
}

// expectSearchResults checks the search response status is 200, closes the body and returns the decoded signals
func expectSearchResults(t *testing.T, response *http.Response) []map[string]any {
	t.Helper()
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Expected search status 200, got %d: %s", response.StatusCode, body)
	}

	var signals []map[string]any
	if err := json.Unmarshal(body, &signals); err != nil {
		t.Fatalf("Failed to decode search response: %v", err)
	}
	return signals
}

// findSignalByLocalRef returns the search result with the supplied local ref (nil if not found)
func findSignalByLocalRef(signals []map[string]any, localRef string) map[string]any {
	for _, signal := range signals {
		if signal["local_ref"] == localRef {
			return signal
		}
	}
	return nil
}

// submitRouteSignalsRequest posts to the signal router endpoint.
func submitRouteSignalsRequest(t *testing.T, baseURL string, payload map[string]any, token string, signalTypeSlug, semVer string) *http.Response {
	t.Helper()
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	url := fmt.Sprintf("%s/api/router/signal-types/%s/v%s/signals", baseURL, signalTypeSlug, semVer)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to submit routed signals: %v", err)
	}
	return resp
}

// setRoutingConfig replaces the routing config for the signal type via the admin API and fails the test if the update is rejected
// (the router cache must be reloaded afterwards for the config to take effect)
func setRoutingConfig(t *testing.T, env *testEnv, token string, signalType database.SignalType, config handlers.UpdateSignalRoutingConfigRequest) {
	t.Helper()

	jsonData, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal routing config: %v", err)
	}

	url := fmt.Sprintf("%s/api/admin/signal-types/%s/v%s/routes", env.baseURL, signalType.Slug, signalType.SemVer)

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to set routing config: %v", err)
	}
	expectStatus(t, response, http.StatusNoContent)
}

// getSignalTypeRequest gets the signal type details: GET /api/admin/signal-types/{slug}/v{sem_ver}
func getSignalTypeRequest(t *testing.T, baseURL, token, slug, semVer string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/api/admin/signal-types/%s/v%s", baseURL, slug, semVer)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get signal type: %v", err)
	}
	return response
}
