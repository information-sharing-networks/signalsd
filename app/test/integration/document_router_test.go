//go:build integration

package integration

// TestRouteDocument tests POST /api/router/signal-types/{signal_type_slug}/v{sem_ver}/signals/upload:
// - documents are sent to the ISN of the correlated signal (including signals sent by another account)
// - correlation_id is required and must refer to an existing signal
// - the write permission and signal type checks on the resolved ISN
// - rejected uploads are recorded against the batch

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

func TestRouteDocument(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	// the consignment (json) and bill of lading (document) signal types are used on isn-a and isn-b.
	// isn-c only has the consignment type.
	//
	// - siteAdminAccount owns the ISNs and sends the consignments
	// - writerAccount has write permission on isn-a only - it uploads bills of lading for consignments it did not send
	siteAdminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "siteadmin@document-router.com")
	writerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "writer@document-router.com")

	isnA := createTestISN(t, ctx, testEnv.queries, "document-router-isn-a", "Document router ISN A", siteAdminAccount.ID, "private")
	isnB := createTestISN(t, ctx, testEnv.queries, "document-router-isn-b", "Document router ISN B", siteAdminAccount.ID, "private")
	isnC := createTestISN(t, ctx, testEnv.queries, "document-router-isn-c", "Document router ISN C", siteAdminAccount.ID, "private")

	consignmentType := createTestSignalType(t, ctx, testEnv.queries, isnA.ID, "consignment", "", signalsd.ContentKindJSON)
	addSignalTypeToIsn(t, ctx, testEnv.queries, isnB.ID, consignmentType.ID)
	addSignalTypeToIsn(t, ctx, testEnv.queries, isnC.ID, consignmentType.ID)

	billOfLadingType := createTestSignalType(t, ctx, testEnv.queries, isnA.ID, "bill of lading", "", signalsd.ContentKindDocument)
	addSignalTypeToIsn(t, ctx, testEnv.queries, isnB.ID, billOfLadingType.ID)

	grantPermission(t, ctx, testEnv.queries, isnA.ID, writerAccount.ID, "read-write")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	siteAdminToken := testEnv.getAccessToken(t, siteAdminAccount.ID)
	writerToken := testEnv.getAccessToken(t, writerAccount.ID)

	// the consignments the bills of lading are correlated with
	consignmentOnIsnA := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-a"), siteAdminToken, newTestSignalEndpoint(isnA, consignmentType))
	consignmentOnIsnB := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-b"), siteAdminToken, newTestSignalEndpoint(isnB, consignmentType))
	consignmentOnIsnC := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-c"), siteAdminToken, newTestSignalEndpoint(isnC, consignmentType))

	pdfContent := []byte("%PDF-BL-2026")

	t.Run("the document is sent to the ISN of the correlated signal", func(t *testing.T) {
		tests := []struct {
			description   string
			token         string
			localRef      string
			correlationID string
			expectedIsn   database.Isn
			otherIsn      database.Isn
		}{
			{"a consignment on isn-a sent by another account", writerToken, "bol-for-consignment-a", consignmentOnIsnA, isnA, isnB},
			{"a consignment on isn-b", siteAdminToken, "bol-for-consignment-b", consignmentOnIsnB, isnB, isnA},
		}
		for _, tt := range tests {
			t.Run(tt.description, func(t *testing.T) {
				upload := expectUploadResponse(t, routeDocumentRequest(t, testEnv.baseURL, tt.token, billOfLadingType, documentUpload{
					batchRef: "router-batch", localRef: tt.localRef, correlationID: tt.correlationID, fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
				}), http.StatusOK)
				if upload.IsnSlug != tt.expectedIsn.Slug {
					t.Errorf("Expected the response to report ISN %s, got %s", tt.expectedIsn.Slug, upload.IsnSlug)
				}
				if upload.VersionNumber != 1 || upload.SHA256 != sha256Hex(pdfContent) {
					t.Errorf("Expected version 1 with sha256 %s, got %+v", sha256Hex(pdfContent), upload)
				}

				linked := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, newTestSignalEndpoint(tt.expectedIsn, billOfLadingType), siteAdminToken, map[string]string{"correlation_id": tt.correlationID}))
				if len(linked) != 1 || linked[0]["local_ref"] != tt.localRef {
					t.Errorf("Expected %s to be stored on %s and linked to the consignment, got %v", tt.localRef, tt.expectedIsn.Slug, linked)
				}

				other := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, newTestSignalEndpoint(tt.otherIsn, billOfLadingType), siteAdminToken, map[string]string{"correlation_id": tt.correlationID}))
				if len(other) != 0 {
					t.Errorf("Expected no documents linked to the consignment on %s, got %v", tt.otherIsn.Slug, other)
				}
			})
		}
	})

	t.Run("an upload without a correlation_id is rejected", func(t *testing.T) {
		response := routeDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingType, documentUpload{
			batchRef: "router-batch", localRef: "bol-no-correlation", fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})

	t.Run("an unknown correlation_id is rejected and recorded against the batch without an ISN", func(t *testing.T) {
		response := routeDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingType, documentUpload{
			batchRef: "unknown-correlation-batch", localRef: "bol-unknown-correlation", correlationID: uuid.NewV7().String(), fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		})
		expectErrorCode(t, response, http.StatusUnprocessableEntity, apperrors.ErrCodeInvalidCorrelationID)

		batchStatus := expectJSONResponse(t, getBatchStatusRequest(t, testEnv.baseURL, writerToken, "unknown-correlation-batch"), http.StatusOK)
		failures := unresolvedFailures(t, batchStatus, "")
		if len(failures) != 1 || failures[0]["local_ref"] != "bol-unknown-correlation" {
			t.Errorf("Expected an unresolved failure for bol-unknown-correlation with no ISN, got %v", batchStatus)
		}
	})

	t.Run("an upload is rejected if the account cannot write to the ISN of the correlated signal", func(t *testing.T) {
		response := routeDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingType, documentUpload{
			batchRef: "forbidden-batch", localRef: "bol-forbidden", correlationID: consignmentOnIsnB, fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		})
		expectErrorCode(t, response, http.StatusForbidden, apperrors.ErrCodeForbidden)

		batchStatus := expectJSONResponse(t, getBatchStatusRequest(t, testEnv.baseURL, writerToken, "forbidden-batch"), http.StatusOK)
		failures := unresolvedFailures(t, batchStatus, isnB.Slug)
		if len(failures) != 1 || failures[0]["local_ref"] != "bol-forbidden" {
			t.Errorf("Expected an unresolved failure for bol-forbidden on %s, got %v", isnB.Slug, batchStatus)
		}
	})

	t.Run("an upload is rejected if the document signal type is not used on the ISN of the correlated signal", func(t *testing.T) {
		response := routeDocumentRequest(t, testEnv.baseURL, siteAdminToken, billOfLadingType, documentUpload{
			batchRef: "router-batch", localRef: "bol-for-consignment-c", correlationID: consignmentOnIsnC, fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		})
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
	})

	t.Run("json signal types are rejected", func(t *testing.T) {
		response := routeDocumentRequest(t, testEnv.baseURL, writerToken, consignmentType, documentUpload{
			batchRef: "router-batch", localRef: "consignment-document", correlationID: consignmentOnIsnA, fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam)
	})
}

// routeDocumentRequest posts a multipart document upload to the router: POST /api/router/signal-types/{signal_type_slug}/v{sem_ver}/signals/upload
func routeDocumentRequest(t *testing.T, baseURL, token string, signalType database.SignalType, upload documentUpload) *http.Response {
	t.Helper()

	request := uploadDocumentRequestBody(t, upload)

	url := fmt.Sprintf("%s/api/router/signal-types/%s/v%s/signals/upload", baseURL, signalType.Slug, signalType.SemVer)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(request.body))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", request.contentType)
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to upload document via the router: %v", err)
	}
	return response
}
