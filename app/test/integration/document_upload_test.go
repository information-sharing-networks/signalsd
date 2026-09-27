//go:build integration

package integration

// TestDocumentUpload tests POST /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/documents:
// - storing documents and their descriptors (name, mime_type, size_bytes, sha256)
// - versions and unchanged uploads
// - content type detection and the declared sha256 check
// - request validation, size limits and permissions
// - correlation with other signals and recording rejected uploads against the batch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/documents"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

func TestDocumentUpload(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "admin@document-upload.com")
	writerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "writer@document-upload.com")
	readerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "reader@document-upload.com")

	isn := createTestISN(t, ctx, testEnv.queries, "document-upload-isn", "Document upload ISN", adminAccount.ID, "private")
	billOfLadingType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "bill of lading", "", signalsd.ContentKindDocument)
	consignmentType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "consignment", "", signalsd.ContentKindJSON)

	grantPermission(t, ctx, testEnv.queries, isn.ID, writerAccount.ID, "read-write")
	grantPermission(t, ctx, testEnv.queries, isn.ID, readerAccount.ID, "read")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	writerToken := testEnv.getAccessToken(t, writerAccount.ID)
	readerToken := testEnv.getAccessToken(t, readerAccount.ID)

	billOfLadingEndpoint := newTestSignalEndpoint(isn, billOfLadingType)
	consignmentEndpoint := newTestSignalEndpoint(isn, consignmentType)

	// note pdf headers start %PDF-
	pdfContent := []byte("%PDF-BL-2026")
	fileName := "BL-2026.pdf"
	mimeType := "application/pdf"
	amendedPDFContent := []byte("%PDF-BL-2026 (amended)")

	t.Run("upload stores the document", func(t *testing.T) {
		response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef:    "upload-batch",
			localRef:    "bol-001",
			fileName:    fileName,
			contentType: mimeType,
			content:     pdfContent,
		})
		upload := expectUploadResponse(t, response, http.StatusOK)

		if upload.SHA256 != sha256Hex(pdfContent) {
			t.Errorf("Expected sha256 %s, got %s", sha256Hex(pdfContent), upload.SHA256)
		}

		if upload.SizeBytes != int64(len(pdfContent)) {
			t.Errorf("Expected size_bytes %d, got %d", len(pdfContent), upload.SizeBytes)
		}

		if upload.Name != fileName || upload.MimeType != mimeType {
			t.Errorf("Expected name %s  and mime_type %s, got %s and %s", fileName, mimeType, upload.Name, upload.MimeType)
		}

		if upload.VersionNumber != 1 || upload.Unchanged {
			t.Errorf("Expected version 1 and unchanged=false, got version %d and unchanged=%v", upload.VersionNumber, upload.Unchanged)
		}

		// get the content from the document store
		reader, err := testEnv.documentStore.Get(ctx, documents.Key{AccountID: writerAccount.ID, SHA256: upload.SHA256})
		if err != nil {
			t.Fatalf("Failed to get the stored document: %v", err)
		}

		defer reader.Close()
		stored, _ := io.ReadAll(reader)
		if !bytes.Equal(stored, pdfContent) {
			t.Errorf("Expected the stored content to be %q, got %q", pdfContent, stored)
		}
	})

	t.Run("search returns the document metadata", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, readerToken, map[string]string{
			"local_ref": "bol-001",
		}))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 signal, got %d", len(signals))
		}
		if signals[0]["content_kind"] != signalsd.ContentKindDocument {
			t.Errorf("Expected content_kind %s, got %v", signalsd.ContentKindDocument, signals[0]["content_kind"])
		}

		content, _ := signals[0]["content"].(map[string]any)
		expectedContent := map[string]any{
			"name":       fileName,
			"mime_type":  mimeType,
			"size_bytes": float64(len(pdfContent)),
			"sha256":     sha256Hex(pdfContent),
		}
		for field, expected := range expectedContent {
			if content[field] != expected {
				t.Errorf("Expected content %s %v, got %v", field, expected, content[field])
			}
		}
	})

	t.Run("uploading the same document again returns the latest version unchanged", func(t *testing.T) {

		bolContent := []byte("%PDF-hello")
		first := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-unchanged", fileName: "bl.pdf", contentType: "application/pdf", content: bolContent,
		}), http.StatusOK)

		second := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-unchanged", fileName: "bl.pdf", contentType: "application/pdf", content: bolContent,
		}), http.StatusOK)

		if !second.Unchanged {
			t.Error("Expected unchanged=true for an identical upload")
		}

		if second.SignalVersionID != first.SignalVersionID || second.VersionNumber != 1 {
			t.Errorf("Expected the existing version 1 (%s), got version %d (%s)", first.SignalVersionID, second.VersionNumber, second.SignalVersionID)
		}
	})

	t.Run("a changed document creates a new version, and reverting to an older version creates another", func(t *testing.T) {
		for i, expected := range []struct {
			content []byte
			version int32
		}{
			{pdfContent, 1},
			{amendedPDFContent, 2},
			{pdfContent, 3}, // same as version 1, but not the latest version
		} {
			result := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
				batchRef: "upload-batch", localRef: "bol-versions", fileName: "bl.pdf", contentType: "application/pdf", content: expected.content,
			}), http.StatusOK)

			if result.VersionNumber != expected.version || result.Unchanged {
				t.Errorf("Upload %d: expected version %d and unchanged=false, got version %d and unchanged=%v", i+1, expected.version, result.VersionNumber, result.Unchanged)
			}
		}
	})

	t.Run("re-uploading a withdrawn document reactivates it with a new version", func(t *testing.T) {
		expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-withdrawn", fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		}), http.StatusOK)
		expectStatus(t, withdrawSignal(t, testEnv.baseURL, billOfLadingEndpoint, writerToken, "bol-withdrawn"), http.StatusNoContent)

		result := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-withdrawn", fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		}), http.StatusOK)
		if result.Unchanged || result.VersionNumber != 2 {
			t.Errorf("Expected version 2 and unchanged=false, got version %d and unchanged=%v", result.VersionNumber, result.Unchanged)
		}

		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, readerToken, map[string]string{"local_ref": "bol-withdrawn"}))
		if len(signals) != 1 || signals[0]["is_withdrawn"] != false {
			t.Errorf("Expected the document to be reactivated, got %v", signals)
		}
	})

	t.Run("uploading the same document with a different filename creates a new version", func(t *testing.T) {
		expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-renamed", fileName: "BL-0042-draft.pdf", contentType: "application/pdf", content: pdfContent,
		}), http.StatusOK)

		renamed := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-renamed", fileName: "BL-0042.pdf", contentType: "application/pdf", content: pdfContent,
		}), http.StatusOK)
		if renamed.Unchanged || renamed.VersionNumber != 2 || renamed.Name != "BL-0042.pdf" {
			t.Errorf("Expected version 2 named BL-0042.pdf with unchanged=false, got version %d named %s with unchanged=%v", renamed.VersionNumber, renamed.Name, renamed.Unchanged)
		}
	})

	t.Run("supported formats are accepted and the mime type is detected from the content", func(t *testing.T) {
		tests := []struct {
			name             string
			fileName         string
			content          []byte
			expectedMimeType string
		}{
			{"PDF", "bl.pdf", pdfContent, "application/pdf"},
			{"JPEG", "scan.jpg", []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), "image/jpeg"},
			{"JPEG with a .jpeg extension", "scan.jpeg", []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), "image/jpeg"},
			{"PNG", "scan.png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), "image/png"},
			{"XML", "invoice.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><Invoice/>`), "text/xml"},
			{"XML with a byte order mark", "invoice.xml", []byte("\xef\xbb\xbf<?xml version=\"1.0\"?><Invoice/>"), "text/xml"},
			{"extension in upper case", "BL.PDF", pdfContent, "application/pdf"},
		}
		for i, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// the declared content type is ignored
				result := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
					batchRef: "upload-batch", localRef: fmt.Sprintf("bol-format-%d", i), fileName: tt.fileName, contentType: "application/octet-stream", content: tt.content,
				}), http.StatusOK)
				if result.MimeType != tt.expectedMimeType {
					t.Errorf("Expected mime_type %s, got %s", tt.expectedMimeType, result.MimeType)
				}
			})
		}
	})

	t.Run("filenames whose extension does not match the format are rejected", func(t *testing.T) {
		tests := []struct {
			name     string
			fileName string
			content  []byte
		}{
			{"PDF named as an image", "bl.png", pdfContent},
			{"XML named as a web page", "invoice.html", []byte(`<?xml version="1.0"?><html><script>alert(1)</script></html>`)},
			{"no extension", "bl", pdfContent},
		}
		for i, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
					batchRef: "upload-batch", localRef: fmt.Sprintf("bol-extension-%d", i), fileName: tt.fileName, contentType: "application/pdf", content: tt.content,
				})
				expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
			})
		}
	})

	t.Run("unsupported formats are rejected, whatever content type is declared", func(t *testing.T) {
		tests := []struct {
			name    string
			content []byte
		}{
			{"Windows executable", []byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00")},
			{"HTML", []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")},
			{"plain text", []byte("just some text")},
			{"ZIP", []byte("PK\x03\x04\x14\x00\x00\x00")},
			{"XML without an XML declaration", []byte("<Invoice/>")},
		}
		for i, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
					batchRef: "upload-batch", localRef: fmt.Sprintf("bol-unsupported-%d", i), fileName: "bl.pdf", contentType: "application/pdf", content: tt.content,
				})
				expectErrorCode(t, response, http.StatusUnsupportedMediaType, apperrors.ErrCodeUnsupportedMediaType)
			})
		}
	})

	t.Run("a declared sha256 that matches the file is accepted", func(t *testing.T) {
		expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-checked", sha256: sha256Hex(pdfContent), fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		}), http.StatusOK)
	})

	t.Run("a declared sha256 that does not match the file is rejected and recorded against the batch", func(t *testing.T) {
		response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "mismatch-batch", localRef: "bol-mismatch", sha256: sha256Hex([]byte("something else")), fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)

		batchStatus := expectJSONResponse(t, getBatchStatusRequest(t, testEnv.baseURL, writerToken, "mismatch-batch"), http.StatusOK)
		failures := unresolvedFailures(t, batchStatus, isn.Slug)
		if len(failures) != 1 || failures[0]["local_ref"] != "bol-mismatch" {
			t.Errorf("Expected an unresolved failure for bol-mismatch, got %v", batchStatus)
		}
	})

	t.Run("a document can be correlated with a signal of another type", func(t *testing.T) {
		consignmentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-001"), writerToken, consignmentEndpoint)

		expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-correlated", correlationID: consignmentID, fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		}), http.StatusOK)

		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, readerToken, map[string]string{"correlation_id": consignmentID}))
		if len(signals) != 1 || signals[0]["local_ref"] != "bol-correlated" {
			t.Errorf("Expected bol-correlated to be linked to the consignment, got %v", signals)
		}
	})

	t.Run("re-uploading with the same or no correlation_id is unchanged, a different correlation_id creates a new version", func(t *testing.T) {
		consignmentA := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-a"), writerToken, consignmentEndpoint)
		consignmentB := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-b"), writerToken, consignmentEndpoint)

		tests := []struct {
			description       string
			correlationID     string
			expectedVersion   int32
			expectedUnchanged bool
			expectedLinkedTo  string // the consignment the document should be linked to after the upload
		}{
			{"first upload, linked to consignment A", consignmentA, 1, false, consignmentA},
			{"same correlation_id", consignmentA, 1, true, consignmentA},
			{"no correlation_id keeps the existing link", "", 1, true, consignmentA},
			{"different correlation_id", consignmentB, 2, false, consignmentB},
		}
		for _, tt := range tests {
			result := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
				batchRef: "upload-batch", localRef: "bol-relinked", correlationID: tt.correlationID, fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
			}), http.StatusOK)
			if result.VersionNumber != tt.expectedVersion || result.Unchanged != tt.expectedUnchanged {
				t.Errorf("%s: expected version %d and unchanged=%v, got version %d and unchanged=%v",
					tt.description, tt.expectedVersion, tt.expectedUnchanged, result.VersionNumber, result.Unchanged)
			}

			linked := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, readerToken, map[string]string{"correlation_id": tt.expectedLinkedTo}))
			if findSignalByLocalRef(linked, "bol-relinked") == nil {
				t.Errorf("%s: expected bol-relinked to be linked to %s", tt.description, tt.expectedLinkedTo)
			}
		}
	})

	t.Run("unchanged uploads are not counted in the batch they were sent in", func(t *testing.T) {
		expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "first-batch", localRef: "bol-retried", fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		}), http.StatusOK)
		retried := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "retry-batch", localRef: "bol-retried", fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		}), http.StatusOK)
		if !retried.Unchanged {
			t.Fatal("Expected the retried upload to be unchanged")
		}

		// the version belongs to the batch that stored it
		firstBatch := expectJSONResponse(t, getBatchStatusRequest(t, testEnv.baseURL, writerToken, "first-batch"), http.StatusOK)
		if storedCount(firstBatch, isn.Slug) != 1 {
			t.Errorf("Expected 1 stored document in first-batch, got %v", firstBatch)
		}
		retryBatch := expectJSONResponse(t, getBatchStatusRequest(t, testEnv.baseURL, writerToken, "retry-batch"), http.StatusOK)
		if storedCount(retryBatch, isn.Slug) != 0 || retryBatch["contains_failures"] != false {
			t.Errorf("Expected no stored documents and no failures in retry-batch, got %v", retryBatch)
		}
	})

	t.Run("a correlation_id for a signal that does not exist is rejected", func(t *testing.T) {
		response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-bad-correlation", correlationID: uuid.NewV7().String(), fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		})
		expectErrorCode(t, response, http.StatusUnprocessableEntity, apperrors.ErrCodeInvalidCorrelationID)
	})

	t.Run("malformed requests are rejected", func(t *testing.T) {
		tests := []struct {
			name   string
			upload documentUpload
		}{
			{"missing batch_ref", documentUpload{localRef: "bol-bad", fileName: "bl.pdf", content: pdfContent}},
			{"invalid batch_ref", documentUpload{batchRef: "not a valid batch ref!", localRef: "bol-bad", fileName: "bl.pdf", content: pdfContent}},
			{"missing local_ref", documentUpload{batchRef: "upload-batch", fileName: "bl.pdf", content: pdfContent}},
			{"invalid correlation_id", documentUpload{batchRef: "upload-batch", localRef: "bol-bad", correlationID: "not-a-uuid", fileName: "bl.pdf", content: pdfContent}},
			{"invalid sha256", documentUpload{batchRef: "upload-batch", localRef: "bol-bad", sha256: "not-a-sha256", fileName: "bl.pdf", content: pdfContent}},
			{"unexpected form field", documentUpload{batchRef: "upload-batch", localRef: "bol-bad", unexpectedField: "localref", fileName: "bl.pdf", content: pdfContent}},
			{"missing file", documentUpload{batchRef: "upload-batch", localRef: "bol-bad", omitFile: true}},
			{"file without a filename", documentUpload{batchRef: "upload-batch", localRef: "bol-bad", fileName: "", content: pdfContent}},
			{"file sent before the form fields", documentUpload{batchRef: "upload-batch", localRef: "bol-bad", fileName: "bl.pdf", content: pdfContent, fileFirst: true}},
			{"empty file", documentUpload{batchRef: "upload-batch", localRef: "bol-bad", fileName: "bl.pdf", content: []byte{}}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, tt.upload)
				expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
			})
		}
	})

	t.Run("a request that is not multipart is rejected", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/documents", testEnv.baseURL, isn.Slug, billOfLadingType.Slug, billOfLadingType.SemVer)
		response := sendJSONWithContentLength(t, http.MethodPost, url, writerToken, []byte(`{"local_ref": "bol-json"}`))
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeMalformedBody)
	})

	t.Run("a file larger than MAX_DOCUMENT_SIZE is rejected", func(t *testing.T) {
		// the request is within the request size limit (which allows for the form fields and multipart headers), so the file size limit applies
		response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-too-large", fileName: "bl.pdf", contentType: "application/pdf", content: append([]byte("%PDF-1.7\n"), make([]byte, testMaxDocumentSize)...),
		})
		expectErrorCode(t, response, http.StatusRequestEntityTooLarge, apperrors.ErrCodeRequestTooLarge)
	})

	t.Run("a request larger than the request size limit is rejected", func(t *testing.T) {
		// the request size limit is MAX_DOCUMENT_SIZE plus an allowance for the form fields, so a request of twice MAX_DOCUMENT_SIZE is over it
		response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-too-large", fileName: "bl.pdf", contentType: "application/pdf", content: append([]byte("%PDF-1.7\n"), make([]byte, 2*testMaxDocumentSize)...),
		})
		expectErrorCode(t, response, http.StatusRequestEntityTooLarge, apperrors.ErrCodeRequestTooLarge)
	})

	t.Run("json signal types are rejected", func(t *testing.T) {
		response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, consignmentEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "consignment-document", fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		})
		expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam)
	})

	t.Run("accounts without write permission are forbidden", func(t *testing.T) {
		response := uploadDocumentRequest(t, testEnv.baseURL, readerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-reader", fileName: "bl.pdf", contentType: "application/pdf", content: pdfContent,
		})
		expectErrorCode(t, response, http.StatusForbidden, apperrors.ErrCodeForbidden)
	})
}

// storedCount returns the stored count for the ISN in a batch status response (0 if the ISN is not in the batch)
func storedCount(batchStatus map[string]any, isnSlug string) int {
	statuses, _ := batchStatus["batch_status"].([]any)
	for _, s := range statuses {
		status := s.(map[string]any)
		if status["isn_slug"] == isnSlug {
			count, _ := status["stored_count"].(float64)
			return int(count)
		}
	}
	return 0
}
