//go:build integration

package integration

// Tests for event signal types (content_kind = 'event'):
//
// - TestEventSubmission: events are stored with the json signal endpoints, must be correlated and have an occurred_at timestamp, and are immutable
// - TestEventSignalTypes: creating event signal types via the admin API, and the document endpoints reject event signal types

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
)

func TestEventSubmission(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	// the forwarder sends consignments and events about them
	adminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "admin@events.com")
	forwarderAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "forwarder@events.com")

	isn := createTestISN(t, ctx, testEnv.queries, "events-isn", "Events ISN", adminAccount.ID, "private")
	otherISN := createTestISN(t, ctx, testEnv.queries, "events-other-isn", "Events other ISN", adminAccount.ID, "private")
	consignmentType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "consignment", "", signalsd.ContentKindJSON)
	eventType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "ehc approved", "", signalsd.ContentKindEvent)
	otherConsignmentType := createTestSignalType(t, ctx, testEnv.queries, otherISN.ID, "other consignment", "", signalsd.ContentKindJSON)

	grantPermission(t, ctx, testEnv.queries, isn.ID, forwarderAccount.ID, "read-write")
	grantPermission(t, ctx, testEnv.queries, otherISN.ID, forwarderAccount.ID, "read-write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	forwarderToken := testEnv.getAccessToken(t, forwarderAccount.ID)

	consignmentEndpoint := newTestSignalEndpoint(isn, consignmentType)
	eventEndpoint := newTestSignalEndpoint(isn, eventType)

	consignmentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-001"), forwarderToken, consignmentEndpoint)
	otherConsignmentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-002"), forwarderToken, consignmentEndpoint)
	otherISNConsignmentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-other-isn"), forwarderToken, newTestSignalEndpoint(otherISN, otherConsignmentType))

	approved := map[string]any{"occurred_at": "2026-09-27T14:02:00Z", "certificate_no": "EHC-001"}

	var stored handlers.StoredSignal

	t.Run("an event correlated to a signal is stored", func(t *testing.T) {
		stored = expectStoredEvent(t, submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-001", consignmentID, approved), forwarderToken, eventEndpoint))
		if stored.VersionNumber != 1 || stored.Unchanged {
			t.Errorf("Expected version 1 (not unchanged), got %+v", stored)
		}

		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, eventEndpoint, forwarderToken, map[string]string{
			"correlation_id": consignmentID,
		}))
		if len(signals) != 1 || signals[0]["content_kind"] != signalsd.ContentKindEvent {
			t.Errorf("Expected 1 event correlated to the consignment, got %v", signals)
		}
	})

	t.Run("an identical resubmission returns the existing version as unchanged", func(t *testing.T) {
		// the same content with the keys in a different order and different whitespace is still the same content
		content := json.RawMessage(`{"certificate_no": "EHC-001",    "occurred_at": "2026-09-27T14:02:00Z"}`)
		resubmitted := expectStoredEvent(t, submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-001", consignmentID, content), forwarderToken, eventEndpoint))
		if !resubmitted.Unchanged || resubmitted.SignalVersionID != stored.SignalVersionID || resubmitted.VersionNumber != 1 {
			t.Errorf("Expected the existing version %s with unchanged=true, got %+v", stored.SignalVersionID, resubmitted)
		}
	})

	t.Run("resubmissions that change the event are rejected", func(t *testing.T) {
		tests := []struct {
			name          string
			correlationID string
			content       map[string]any
		}{
			{"different content", consignmentID, map[string]any{"occurred_at": "2026-09-27T14:02:00Z", "certificate_no": "EHC-002"}},
			{"different occurred_at", consignmentID, map[string]any{"occurred_at": "2026-09-27T15:02:00Z", "certificate_no": "EHC-001"}},
			{"different correlation_id", otherConsignmentID, approved},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				response := submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-001", tt.correlationID, tt.content), forwarderToken, eventEndpoint)
				expectFailedEvent(t, response, apperrors.ErrCodeResourceAlreadyExists)
			})
		}

		// the event is unchanged
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, eventEndpoint, forwarderToken, map[string]string{
			"local_ref":                 "ehc-approved-001",
			"include_previous_versions": "true",
		}))
		if len(signals) != 1 || signals[0]["version_number"] != float64(1) || signals[0]["previous_signal_versions"] != nil {
			t.Errorf("Expected only version 1 of the event, got %v", signals)
		}
	})

	t.Run("a withdrawn event can't be resubmitted", func(t *testing.T) {
		expectStoredEvent(t, submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-withdrawn", consignmentID, approved), forwarderToken, eventEndpoint))
		expectStatus(t, withdrawSignal(t, testEnv.baseURL, eventEndpoint, forwarderToken, "ehc-approved-withdrawn"), http.StatusNoContent)

		response := submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-withdrawn", consignmentID, approved), forwarderToken, eventEndpoint)
		expectFailedEvent(t, response, apperrors.ErrCodeResourceAlreadyExists)
	})

	t.Run("events without a correlation_id are rejected", func(t *testing.T) {
		payload := createEventPayload("ehc-approved-uncorrelated", consignmentID, approved)
		delete(payload["signals"].([]map[string]any)[0], "correlation_id")

		response := submitCreateSignalRequest(t, testEnv.baseURL, payload, forwarderToken, eventEndpoint)
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})

	t.Run("events must have a valid occurred_at timestamp", func(t *testing.T) {
		tests := []struct {
			name    string
			content any
		}{
			{"missing occurred_at", map[string]any{"certificate_no": "EHC-001"}},
			{"occurred_at is not a string", map[string]any{"occurred_at": 1758981720, "certificate_no": "EHC-001"}},
			{"occurred_at has no time zone offset", map[string]any{"occurred_at": "2026-09-27T14:02:00", "certificate_no": "EHC-001"}},
			{"occurred_at is not a timestamp", map[string]any{"occurred_at": "yesterday", "certificate_no": "EHC-001"}},
			{"content is not an object", []any{"2026-09-27T14:02:00Z"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				response := submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-invalid", consignmentID, tt.content), forwarderToken, eventEndpoint)
				expectFailedEvent(t, response, apperrors.ErrCodeMalformedBody)
			})
		}

		t.Run("occurred_at with fractional seconds and an offset is accepted", func(t *testing.T) {
			expectStoredEvent(t, submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-offset", consignmentID, map[string]any{
				"occurred_at": "2026-09-27T15:02:00.123+01:00", "certificate_no": "EHC-001",
			}), forwarderToken, eventEndpoint))
		})
	})

	t.Run("events are validated against the signal type schema", func(t *testing.T) {
		response := submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-no-certificate", consignmentID, map[string]any{
			"occurred_at": "2026-09-27T14:02:00Z",
		}), forwarderToken, eventEndpoint)
		expectFailedEvent(t, response, apperrors.ErrCodeMalformedBody)
	})

	t.Run("events must be correlated to a signal in the same ISN", func(t *testing.T) {
		for name, correlationID := range map[string]string{
			"unknown signal":            uuid.NewV7().String(),
			"signal in a different ISN": otherISNConsignmentID,
		} {
			t.Run(name, func(t *testing.T) {
				response := submitCreateSignalRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-bad-correlation", correlationID, approved), forwarderToken, eventEndpoint)
				expectFailedEvent(t, response, apperrors.ErrCodeInvalidCorrelationID)
			})
		}
	})

	t.Run("events are routed to the ISN of the correlated signal", func(t *testing.T) {
		response := submitRouteSignalsRequest(t, testEnv.baseURL, createEventPayload("ehc-approved-routed", consignmentID, approved), forwarderToken, eventType.Slug, eventType.SemVer)
		submission := expectSubmissionResponse(t, response, http.StatusOK)
		if len(submission.Results) != 1 || submission.Results[0].IsnSlug != isn.Slug || len(submission.Results[0].StoredSignals) != 1 {
			t.Errorf("Expected the event to be stored on %s, got %+v", isn.Slug, submission)
		}
	})

	t.Run("routed events without a correlation_id are rejected", func(t *testing.T) {
		payload := createEventPayload("ehc-approved-routed-uncorrelated", consignmentID, approved)
		delete(payload["signals"].([]map[string]any)[0], "correlation_id")

		response := submitRouteSignalsRequest(t, testEnv.baseURL, payload, forwarderToken, eventType.Slug, eventType.SemVer)
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})
}

func TestEventSignalTypes(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@event-types.com")
	isn := createTestISN(t, ctx, testEnv.queries, "event-types-isn", "Event types ISN", siteAdminAccount.ID, "private")
	eventType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "ehc approved", "", signalsd.ContentKindEvent)
	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	// the token is created after the ISN so the claims include it
	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)

	t.Run("create event signal type", func(t *testing.T) {
		response := createSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:       "Departed Origin",
			BumpType:    "major",
			SchemaURL:   signalsd.SkipValidationURL,
			ReadmeURL:   signalsd.SkipReadmeURL,
			Detail:      "the consignment has left the point of origin",
			ContentKind: signalsd.ContentKindEvent,
		})
		expectStatus(t, response, http.StatusCreated)

		signalType := expectJSONResponse(t, getSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, "departed-origin", "1.0.0"), http.StatusOK)
		if signalType["content_kind"] != signalsd.ContentKindEvent {
			t.Errorf("Expected content_kind %s, got %v", signalsd.ContentKindEvent, signalType["content_kind"])
		}
	})

	t.Run("event signal types require a schema_url", func(t *testing.T) {
		response := createSignalTypeRequest(t, testEnv.baseURL, siteAdminToken, handlers.CreateSignalTypeRequest{
			Title:       "Arrived Destination",
			BumpType:    "major",
			ReadmeURL:   signalsd.SkipReadmeURL,
			Detail:      "the consignment has arrived",
			ContentKind: signalsd.ContentKindEvent,
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})

	t.Run("the document endpoints reject event signal types", func(t *testing.T) {
		response := uploadDocumentRequest(t, testEnv.baseURL, siteAdminToken, newTestSignalEndpoint(isn, eventType), documentUpload{
			batchRef: "event-batch", localRef: "ehc-approved-upload", fileName: "ehc.pdf", content: []byte("%PDF-1.7 not an event"),
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam)
	})
}

// createEventPayload creates a payload with one event correlated to correlationID
func createEventPayload(localRef string, correlationID string, content any) map[string]any {
	return map[string]any{
		"batch_ref": "event-batch",
		"signals": []map[string]any{
			{
				"local_ref":      localRef,
				"correlation_id": correlationID,
				"content":        content,
			},
		},
	}
}

// expectStoredEvent checks the event submission succeeded and returns the stored event
func expectStoredEvent(t *testing.T, response *http.Response) handlers.StoredSignal {
	t.Helper()

	submission := expectSubmissionResponse(t, response, http.StatusOK)
	if len(submission.Results) != 1 || len(submission.Results[0].StoredSignals) != 1 {
		t.Fatalf("Expected 1 stored event, got %+v", submission)
	}
	return submission.Results[0].StoredSignals[0]
}

// expectFailedEvent checks the event submission failed with the expected error code
func expectFailedEvent(t *testing.T, response *http.Response, expectedCode apperrors.ErrorCode) {
	t.Helper()

	submission := expectSubmissionResponse(t, response, http.StatusUnprocessableEntity)
	if len(submission.Results) != 1 || len(submission.Results[0].FailedSignals) != 1 {
		t.Fatalf("Expected 1 failed event, got %+v", submission)
	}
	if failure := submission.Results[0].FailedSignals[0]; failure.ErrorCode != expectedCode.String() {
		t.Errorf("Expected error_code %s, got %s (%s)", expectedCode, failure.ErrorCode, failure.ErrorMessage)
	}
}
