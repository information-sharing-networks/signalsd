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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"
	"time"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/documents"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/handlers"
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
	amendedPDFContent := []byte("%BL-2026 (amended)")

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

		bolContent := []byte("hello")
		// TODO - how do we handle docs with different names but identical content?
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

	// TODO - remove option to declare mimetype
	t.Run("the content type is detected when it is not declared", func(t *testing.T) {
		for _, declaredContentType := range []string{"", "application/octet-stream"} {
			result := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
				batchRef: "upload-batch", localRef: "bol-detected", fileName: "bl.pdf", contentType: declaredContentType, content: pdfContent,
			}), http.StatusOK)
			if result.MimeType != "application/pdf" {
				t.Errorf("Declared content type %q: expected the detected mime_type application/pdf, got %s", declaredContentType, result.MimeType)
			}
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
			batchRef: "upload-batch", localRef: "bol-too-large", fileName: "bl.pdf", contentType: "application/pdf", content: make([]byte, testMaxDocumentSize+1),
		})
		expectErrorCode(t, response, http.StatusRequestEntityTooLarge, apperrors.ErrCodeRequestTooLarge)
	})

	t.Run("a request larger than the request size limit is rejected", func(t *testing.T) {
		// the request size limit is MAX_DOCUMENT_SIZE plus an allowance for the form fields, so a request of twice MAX_DOCUMENT_SIZE is over it
		response := uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "upload-batch", localRef: "bol-too-large", fileName: "bl.pdf", contentType: "application/pdf", content: make([]byte, 2*testMaxDocumentSize),
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

// documentUpload describes a document upload request. Empty form fields are not sent.
type documentUpload struct {
	batchRef      string
	localRef      string
	correlationID string
	sha256        string

	fileName    string
	contentType string // the Content-Type of the file part ("" sends no Content-Type)
	content     []byte

	unexpectedField string // the name of an extra form field to send
	omitFile        bool   // send the form fields without a file
	fileFirst       bool   // send the file before the form fields
}

// uploadDocumentRequest posts a multipart document upload: POST /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/documents
// fileFirst reorders the file part before the other fields, omitFile skips the file part entirely,
// and unexpectedField (if set) injects an additional, unsupported form field.
func uploadDocumentRequest(t *testing.T, baseURL, token string, endpoint testSignalEndpoint, upload documentUpload) *http.Response {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	writeFilePart := func() {
		disposition := `form-data; name="file"`
		if upload.fileName != "" {
			disposition = fmt.Sprintf(`form-data; name="file"; filename=%q`, upload.fileName)
		}
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", disposition)
		if upload.contentType != "" {
			header.Set("Content-Type", upload.contentType)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("Failed to create file part: %v", err)
		}
		if _, err := part.Write(upload.content); err != nil {
			t.Fatalf("Failed to write file part: %v", err)
		}
	}

	if upload.fileFirst {
		writeFilePart()
	}

	// load the other fields
	for _, field := range []struct{ name, value string }{
		{"batch_ref", upload.batchRef},
		{"local_ref", upload.localRef},
		{"correlation_id", upload.correlationID},
		{"sha256", upload.sha256},
	} {
		if field.name == "" || field.value == "" {
			continue
		}
		if err := writer.WriteField(field.name, field.value); err != nil {
			t.Fatalf("Failed to write form field %s: %v", field.name, err)
		}
	}

	if upload.unexpectedField != "" {
		if err := writer.WriteField(upload.unexpectedField, "value"); err != nil {
			t.Fatalf("Failed to write unexpected field: %v", err)
		}
	}

	if !upload.fileFirst && !upload.omitFile {
		writeFilePart()
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close multipart writer: %v", err)
	}

	url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/documents",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to upload document: %v", err)
	}
	return response
}

// expectUploadResponse checks the status of a document upload response, closes the body and returns the decoded response
func expectUploadResponse(t *testing.T, response *http.Response, expectedStatus int) handlers.DocumentUploadResponse {
	t.Helper()
	defer response.Body.Close()

	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != expectedStatus {
		t.Fatalf("Expected status %d, got %d: %s", expectedStatus, response.StatusCode, body)
	}

	var upload handlers.DocumentUploadResponse
	if err := json.Unmarshal(body, &upload); err != nil {
		t.Fatalf("Failed to decode upload response: %v: %s", err, body)
	}
	return upload
}

// sha256Hex returns the lowercase hex sha256 of the content
func sha256Hex(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
