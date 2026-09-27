//go:build integration

package integration

// Document download tests: GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/{signal_id}/content
//
// - TestDocumentDownload: downloading versions of documents, the response headers and who can download documents
// - TestDocumentTransferTimeout: document uploads are given DOCUMENT_TRANSFER_TIMEOUT instead of READ_TIMEOUT

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"testing"
	"time"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

func TestDocumentDownload(t *testing.T) {
	ctx := context.Background()

	testEnv := startInProcessServer(t, "")

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "admin@document-download.com")
	writerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "writer@document-download.com")
	writeOnlyAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "write-only@document-download.com")
	readerAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "reader@document-download.com")
	outsiderAccount := createTestAccount(t, ctx, testEnv.queries, "member", "user", "outsider@document-download.com")

	isn := createTestISN(t, ctx, testEnv.queries, "document-download-isn", "Document download ISN", adminAccount.ID, "private")
	billOfLadingType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "bill of lading", "", signalsd.ContentKindDocument)
	invoiceType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "invoice", "", signalsd.ContentKindDocument)
	consignmentType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "consignment", "", signalsd.ContentKindJSON)

	grantPermission(t, ctx, testEnv.queries, isn.ID, writerAccount.ID, "read-write")
	grantPermission(t, ctx, testEnv.queries, isn.ID, writeOnlyAccount.ID, "write")
	grantPermission(t, ctx, testEnv.queries, isn.ID, readerAccount.ID, "read")

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}

	writerToken := testEnv.getAccessToken(t, writerAccount.ID)
	writeOnlyToken := testEnv.getAccessToken(t, writeOnlyAccount.ID)
	readerToken := testEnv.getAccessToken(t, readerAccount.ID)
	outsiderToken := testEnv.getAccessToken(t, outsiderAccount.ID)

	billOfLadingEndpoint := newTestSignalEndpoint(isn, billOfLadingType)
	invoiceEndpoint := newTestSignalEndpoint(isn, invoiceType)
	consignmentEndpoint := newTestSignalEndpoint(isn, consignmentType)

	version1Content := []byte("%PDF-1.7 bill of lading BL-0042")
	version2Content := []byte("%PDF-1.7 bill of lading BL-0042 (amended)")

	// the writer uploads two versions of a bill of lading, linked to a consignment
	consignmentID := submitSignalAndGetID(t, testEnv.baseURL, createValidSignalPayload("consignment-001"), writerToken, consignmentEndpoint)
	expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
		batchRef: "download-batch", localRef: "bol-001", correlationID: consignmentID, fileName: "BL-0042.pdf", content: version1Content,
	}), http.StatusOK)
	upload := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
		batchRef: "download-batch", localRef: "bol-001", correlationID: consignmentID, fileName: "BL-0042.pdf", content: version2Content,
	}), http.StatusOK)
	documentID := upload.SignalID.String()

	// the write-only account uploads its own document
	writeOnlyUpload := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writeOnlyToken, billOfLadingEndpoint, documentUpload{
		batchRef: "download-batch", localRef: "bol-write-only", fileName: "BL-0043.pdf", content: version1Content,
	}), http.StatusOK)

	t.Run("the latest version is downloaded with its metadata in the headers", func(t *testing.T) {
		response := downloadDocumentRequest(t, testEnv.baseURL, readerToken, billOfLadingEndpoint, documentID, "")
		content := expectDownload(t, response)

		if !bytes.Equal(content, version2Content) {
			t.Errorf("Expected the latest version %q, got %q", version2Content, content)
		}

		expectedHeaders := map[string]string{
			"Content-Type":           "application/pdf",
			"Content-Length":         fmt.Sprint(len(version2Content)),
			"ETag":                   fmt.Sprintf("%q", sha256Hex(version2Content)),
			"X-Content-Type-Options": "nosniff",
			"Cache-Control":          "private, no-store",
		}
		for header, expected := range expectedHeaders {
			if got := response.Header.Get(header); got != expected {
				t.Errorf("Expected %s %q, got %q", header, expected, got)
			}
		}

		disposition, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
		if err != nil || disposition != "attachment" || params["filename"] != "BL-0042.pdf" {
			t.Errorf("Expected Content-Disposition attachment with filename BL-0042.pdf, got %q", response.Header.Get("Content-Disposition"))
		}
	})

	t.Run("a previous version can be downloaded", func(t *testing.T) {
		content := expectDownload(t, downloadDocumentRequest(t, testEnv.baseURL, readerToken, billOfLadingEndpoint, documentID, "version=1"))
		if !bytes.Equal(content, version1Content) {
			t.Errorf("Expected version 1 %q, got %q", version1Content, content)
		}
	})

	t.Run("documents found by searching for the signals linked to a consignment can be downloaded", func(t *testing.T) {
		signals := expectSearchResults(t, searchPrivateSignals(t, testEnv.baseURL, billOfLadingEndpoint, readerToken, map[string]string{
			"correlation_id": consignmentID,
		}))
		if len(signals) != 1 {
			t.Fatalf("Expected 1 linked document, got %d", len(signals))
		}

		content := expectDownload(t, downloadDocumentRequest(t, testEnv.baseURL, readerToken, billOfLadingEndpoint, signals[0]["signal_id"].(string), ""))
		if !bytes.Equal(content, version2Content) {
			t.Errorf("Expected the linked document %q, got %q", version2Content, content)
		}
	})

	t.Run("filenames that are not plain ASCII are returned correctly", func(t *testing.T) {
		unicodeUpload := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "download-batch", localRef: "bol-unicode", fileName: "Konnossement Übersee.pdf", content: version1Content,
		}), http.StatusOK)

		response := downloadDocumentRequest(t, testEnv.baseURL, readerToken, billOfLadingEndpoint, unicodeUpload.SignalID.String(), "")
		expectDownload(t, response)

		_, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
		if err != nil || params["filename"] != "Konnossement Übersee.pdf" {
			t.Errorf("Expected filename %q, got %q (%v)", "Konnossement Übersee.pdf", response.Header.Get("Content-Disposition"), err)
		}
	})

	t.Run("withdrawn documents are only downloaded with include_withdrawn=true", func(t *testing.T) {
		withdrawnUpload := expectUploadResponse(t, uploadDocumentRequest(t, testEnv.baseURL, writerToken, billOfLadingEndpoint, documentUpload{
			batchRef: "download-batch", localRef: "bol-withdrawn", fileName: "BL-0044.pdf", content: version1Content,
		}), http.StatusOK)
		expectStatus(t, withdrawSignal(t, testEnv.baseURL, billOfLadingEndpoint, writerToken, "bol-withdrawn"), http.StatusNoContent)

		response := downloadDocumentRequest(t, testEnv.baseURL, readerToken, billOfLadingEndpoint, withdrawnUpload.SignalID.String(), "")
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)

		expectDownload(t, downloadDocumentRequest(t, testEnv.baseURL, readerToken, billOfLadingEndpoint, withdrawnUpload.SignalID.String(), "include_withdrawn=true"))
	})

	t.Run("write-only accounts can only download the documents they uploaded", func(t *testing.T) {
		expectDownload(t, downloadDocumentRequest(t, testEnv.baseURL, writeOnlyToken, billOfLadingEndpoint, writeOnlyUpload.SignalID.String(), ""))

		response := downloadDocumentRequest(t, testEnv.baseURL, writeOnlyToken, billOfLadingEndpoint, documentID, "")
		expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
	})

	t.Run("accounts without access to the ISN are forbidden", func(t *testing.T) {
		response := downloadDocumentRequest(t, testEnv.baseURL, outsiderToken, billOfLadingEndpoint, documentID, "")
		expectErrorCode(t, response, http.StatusForbidden, apperrors.ErrCodeForbidden)
	})

	t.Run("downloads require authentication", func(t *testing.T) {
		response := downloadDocumentRequest(t, testEnv.baseURL, "", billOfLadingEndpoint, documentID, "")
		expectErrorCode(t, response, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure)
	})

	t.Run("documents that don't exist are not found", func(t *testing.T) {
		tests := []struct {
			name     string
			endpoint testSignalEndpoint
			signalID string
			query    string
		}{
			{"unknown signal_id", billOfLadingEndpoint, uuid.NewV7().String(), ""},
			{"version that does not exist", billOfLadingEndpoint, documentID, "version=3"},
			{"document requested with a different document signal type", invoiceEndpoint, documentID, ""},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				response := downloadDocumentRequest(t, testEnv.baseURL, readerToken, tt.endpoint, tt.signalID, tt.query)
				expectErrorCode(t, response, http.StatusNotFound, apperrors.ErrCodeResourceNotFound)
			})
		}
	})

	t.Run("invalid requests are rejected", func(t *testing.T) {
		tests := []struct {
			name     string
			endpoint testSignalEndpoint
			signalID string
			query    string
		}{
			{"malformed signal_id", billOfLadingEndpoint, "not-a-uuid", ""},
			{"version 0", billOfLadingEndpoint, documentID, "version=0"},
			{"version that is not a number", billOfLadingEndpoint, documentID, "version=latest"},
			{"json signal type", consignmentEndpoint, consignmentID, ""},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				response := downloadDocumentRequest(t, testEnv.baseURL, readerToken, tt.endpoint, tt.signalID, tt.query)
				expectErrorCode(t, response, http.StatusBadRequest, apperrors.ErrCodeInvalidURLParam)
			})
		}
	})
}

// TestDocumentTransferTimeout checks document uploads are given DOCUMENT_TRANSFER_TIMEOUT instead of READ_TIMEOUT,
// so large documents can be uploaded on slow connections, while other requests keep the shorter READ_TIMEOUT.
func TestDocumentTransferTimeout(t *testing.T) {
	ctx := context.Background()

	// the slow requests below take 1s to send - twice READ_TIMEOUT.
	// WRITE_TIMEOUT must be well over 1s: RequestTimeout cancels the context of other requests at WRITE_TIMEOUT - 1s
	const sendDuration = 1 * time.Second
	testEnv := startInProcessServerWithConfig(t, "", func(cfg *signalsd.ServerEnvironment) {
		cfg.ReadTimeout = 500 * time.Millisecond
		cfg.WriteTimeout = 2 * time.Second
		cfg.DocumentTransferTimeout = 10 * time.Second
	})

	adminAccount := createTestAccount(t, ctx, testEnv.queries, "siteadmin", "user", "admin@transfer-timeout.com")
	isn := createTestISN(t, ctx, testEnv.queries, "transfer-timeout-isn", "Transfer timeout ISN", adminAccount.ID, "private")
	documentType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "bill of lading", "", signalsd.ContentKindDocument)
	jsonType := createTestSignalType(t, ctx, testEnv.queries, isn.ID, "consignment", "", signalsd.ContentKindJSON)

	if err := testEnv.schemaCache.Load(ctx); err != nil {
		t.Fatalf("Failed to refresh schema cache: %v", err)
	}
	adminToken := testEnv.getAccessToken(t, adminAccount.ID)

	// the subtests are independent, so they run in parallel to keep the test quick
	t.Run("a slow document upload succeeds", func(t *testing.T) {
		t.Parallel()

		// build the multipart request, then send it slowly
		request := uploadDocumentRequestBody(t, documentUpload{
			batchRef: "document-timeout-batch", localRef: "bol-slow", fileName: "bl.pdf", content: []byte("%PDF-1.7 sent slowly"),
		})
		url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/signals/upload", testEnv.baseURL, isn.Slug, documentType.Slug, documentType.SemVer)

		response, err := sendSlowly(url, request.contentType, adminToken, request.body, sendDuration)
		if err != nil {
			t.Fatalf("Slow document upload failed: %v", err)
		}
		expectUploadResponse(t, response, http.StatusOK)
	})

	t.Run("a slow request to another endpoint is cut off by READ_TIMEOUT", func(t *testing.T) {
		t.Parallel()

		url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/signals", testEnv.baseURL, isn.Slug, jsonType.Slug, jsonType.SemVer)
		body := []byte(`{"batch_ref": "json-timeout-batch", "signals": [{"local_ref": "slow-001", "content": {"test": "sent slowly"}}]}`)

		response, err := sendSlowly(url, "application/json", adminToken, body, sendDuration)
		if err == nil {
			defer response.Body.Close()
			if response.StatusCode == http.StatusOK {
				t.Error("Expected the slow request to be cut off by READ_TIMEOUT, but it succeeded")
			}
		}
	})
}

// downloadDocumentRequest downloads a document: GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/{signal_id}/content?{query}
func downloadDocumentRequest(t *testing.T, baseURL, token string, endpoint testSignalEndpoint, signalID, query string) *http.Response {
	t.Helper()

	url := fmt.Sprintf("%s/api/isn/%s/signal-types/%s/v%s/signals/%s/content",
		baseURL, endpoint.isnSlug, endpoint.signalTypeSlug, endpoint.signalTypeSemVer, signalID)
	if query != "" {
		url += "?" + query
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to download document: %v", err)
	}
	return response
}

// expectDownload checks the download response status is 200, closes the body and returns the document content
func expectDownload(t *testing.T, response *http.Response) []byte {
	t.Helper()
	defer response.Body.Close()

	content, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("Failed to read the downloaded document: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d: %s", response.StatusCode, content)
	}
	return content
}

// sendSlowly posts the body in 10 chunks spread over the duration (the request is sent without a Content-Length)
func sendSlowly(url, contentType, token string, body []byte, duration time.Duration) (*http.Response, error) {
	reader, writer := io.Pipe()
	go func() {
		chunkSize := len(body)/10 + 1
		for start := 0; start < len(body); start += chunkSize {
			time.Sleep(duration / 10)
			if _, err := writer.Write(body[start:min(start+chunkSize, len(body))]); err != nil {
				writer.CloseWithError(err)
				return
			}
		}
		writer.Close()
	}()

	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)

	return http.DefaultClient.Do(req)
}
