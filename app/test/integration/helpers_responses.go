//go:build integration

package integration

// Response checks shared by all the tests.
// Each function checks the status, closes the response body and fails the test if the response is not as expected.

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
)

// expectStatus checks the response status and closes the body
func expectStatus(t *testing.T, response *http.Response, expectedStatus int) {
	t.Helper()
	defer response.Body.Close()

	if response.StatusCode != expectedStatus {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("Expected status %d, got %d: %s", expectedStatus, response.StatusCode, body)
	}
}

// expectJSONResponse reads response's body.  Fails the test fatally if the status
// code doesn't match expectedStatus, otherwise returns the decoded value.
func expectJSONResponse(t *testing.T, response *http.Response, expectedStatus int) map[string]any {
	t.Helper()
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != expectedStatus {
		t.Fatalf("Expected status %d, got %d: %s", expectedStatus, response.StatusCode, body)
	}

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("Failed to decode response: %v: %s", err, body)
	}
	return decoded
}

// expectErrorCode checks the response status and error code and closes the body
func expectErrorCode(t *testing.T, response *http.Response, expectedStatus int, expectedCode apperrors.ErrorCode) {
	t.Helper()

	errorResponse := expectJSONResponse(t, response, expectedStatus)
	if errorResponse["error_code"] != expectedCode.String() {
		t.Errorf("Expected error_code %s, got %v (%v)", expectedCode, errorResponse["error_code"], errorResponse["message"])
	}
}
