//go:build integration

package integration

// TestRequestSizeLimits checks requests with bodies over the configured size limits are rejected with 413 request_too_large.
//
// The RequestSizeLimit middleware rejects requests with a Content-Length over the limit before they reach the handler.
// Requests without a Content-Length (chunked requests) only reach the limit when the handler reads the body -
// these must also be rejected with 413 (rather than being reported as malformed JSON).

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

func TestRequestSizeLimits(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@request-size.com")
	isn := createTestISN(t, ctx, testEnv.queries, "request-size-isn", "Request size ISN", siteAdminAccount.ID, "private")
	signalType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "request size signal", "", signalsd.ContentKindJSON)

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)

	createSignalsURL := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/signals", testEnv.baseURL, isn.Slug, signalType.Slug, signalType.SemVer)
	routeSignalsURL := fmt.Sprintf("%s/api/router/signal-types/%s/v%s/signals", testEnv.baseURL, signalType.Slug, signalType.SemVer)
	routingConfigURL := fmt.Sprintf("%s/api/admin/signal-types/%s/v%s/routes", testEnv.baseURL, signalType.Slug, signalType.SemVer)

	t.Run("create signals: oversized body with Content-Length", func(t *testing.T) {
		body := jsonBodyLargerThan(testEnv.cfg.MaxSignalPayloadSize)
		response := sendJSONWithContentLength(t, http.MethodPost, createSignalsURL, siteAdminToken, body)
		expectErrorCode(t, response, http.StatusRequestEntityTooLarge, apperrors.ErrCodeRequestTooLarge)
	})

	t.Run("create signals: oversized body without Content-Length", func(t *testing.T) {
		body := jsonBodyLargerThan(testEnv.cfg.MaxSignalPayloadSize)
		response := sendJSONWithoutContentLength(t, http.MethodPost, createSignalsURL, siteAdminToken, body)
		expectErrorCode(t, response, http.StatusRequestEntityTooLarge, apperrors.ErrCodeRequestTooLarge)
	})

	t.Run("signal router: oversized body without Content-Length", func(t *testing.T) {
		body := jsonBodyLargerThan(testEnv.cfg.MaxSignalPayloadSize)
		response := sendJSONWithoutContentLength(t, http.MethodPost, routeSignalsURL, siteAdminToken, body)
		expectErrorCode(t, response, http.StatusRequestEntityTooLarge, apperrors.ErrCodeRequestTooLarge)
	})

	t.Run("admin API: oversized body without Content-Length", func(t *testing.T) {
		body := jsonBodyLargerThan(testEnv.cfg.MaxAPIRequestSize)
		response := sendJSONWithoutContentLength(t, http.MethodPut, routingConfigURL, siteAdminToken, body)
		expectErrorCode(t, response, http.StatusRequestEntityTooLarge, apperrors.ErrCodeRequestTooLarge)
	})
}

// jsonBodyLargerThan returns a valid JSON signals payload that is larger than the limit
// (the body is valid so that the only thing wrong with it is its size)
func jsonBodyLargerThan(limit int64) []byte {
	padding := strings.Repeat("a", int(limit))
	return fmt.Appendf(nil, `{"batch_ref": "test-batch", "signals": [{"local_ref": "oversized-001", "content": {"test": "%s"}}]}`, padding)
}

// sendJSONWithContentLength sends the body with a Content-Length header
func sendJSONWithContentLength(t *testing.T, method, url, token string, body []byte) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
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

// sendJSONWithoutContentLength sends the body without a Content-Length header (the request is sent chunked).
// http.NewRequest only sets the Content-Length for a small set of reader types, so wrapping the body hides its length.
func sendJSONWithoutContentLength(t *testing.T, method, url, token string, body []byte) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, url, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if response.Request.ContentLength > 0 {
		t.Fatalf("Expected the request to be sent without a Content-Length, got %d", response.Request.ContentLength)
	}
	return response
}
