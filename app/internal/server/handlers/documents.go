package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/documents"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maxFormFieldSize is the maximum size of the form fields sent with a document (batch_ref, local_ref, correlation_id, sha256).
// Each field is read into memory, so this stops a malicious client using large field values to exhaust the server's memory
const maxFormFieldSize = 1024

// uploadRequestOverhead allows for the form fields and multipart headers sent with the document -
// the request size limit for uploads is MAX_DOCUMENT_SIZE plus this
// (the document itself is limited to MAX_DOCUMENT_SIZE by storeDocumentSignal)
const uploadRequestOverhead = 64 * 1024

// MaxUploadRequestSize is the request size limit for the document upload routes
func MaxUploadRequestSize(maxDocumentSize int64) int64 {
	return maxDocumentSize + uploadRequestOverhead
}

var sha256Regexp = regexp.MustCompile(`^[a-f0-9]{64}$`)

type DocumentsHandler struct {
	queries         *database.Queries
	pool            *pgxpool.Pool
	schemaCache     *schemas.Cache
	documentStore   documents.Store
	maxDocumentSize int64
}

func NewDocumentsHandler(queries *database.Queries, pool *pgxpool.Pool, schemaCache *schemas.Cache, documentStore documents.Store, maxDocumentSize int64) *DocumentsHandler {
	return &DocumentsHandler{
		queries:         queries,
		pool:            pool,
		schemaCache:     schemaCache,
		documentStore:   documentStore,
		maxDocumentSize: maxDocumentSize,
	}
}

// DocumentMetadata is stored as the content of each document signal version and returned by the signal search endpoints.
// The document itself is downloaded separately.
type DocumentMetadata struct {

	// Name is the file name supplied when the document was uploaded (its extension must match the mime type)
	Name string `json:"name" example:"BL-2026-0042.pdf"`

	// MimeType is the type of document, detected from its content (application/pdf, image/jpeg, image/png or text/xml)
	MimeType string `json:"mime_type" example:"application/pdf"`

	// SizeBytes is the size of the document in bytes (measured by the server when the document is uploaded)
	SizeBytes int64 `json:"size_bytes" example:"482133"`

	// SHA256 is the sha256 hash of the document as lowercase hex (computed by the server when the document is uploaded)
	SHA256 string `json:"sha256" example:"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"`
}

// DocumentUploadResponse describes the stored document signal version
type DocumentUploadResponse struct {
	IsnSlug         string    `json:"isn_slug" example:"sample-isn"` // the ISN the document was stored on
	BatchRef        string    `json:"batch_ref" example:"daily-sync-2026-04-02"`
	LocalRef        string    `json:"local_ref" example:"bol-2026-0042"`
	SignalID        uuid.UUID `json:"signal_id" example:"b8ded113-ac0e-4a2c-a89f-0876fe97b440"`
	SignalVersionID uuid.UUID `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`
	VersionNumber   int32     `json:"version_number" example:"1"`
	DocumentMetadata
	Unchanged bool `json:"unchanged" example:"false"` // true if the file and filename are the same as the latest version, in which case that version is returned and no new version is created
}

// UploadDocument godoc
//
//	@Summary		Upload a Document
//	@Tags			Signal Exchange
//
//	@Description	Upload a document (e.g. a PDF bill of lading) to a document signal type (JSON signals are sent with Submit Signals).
//	@Description
//	@Description	Documents are signals: they have a local_ref, versions, can be correlated with other signals and withdrawn.
//	@Description	The document details (name, mime_type, size_bytes and sha256) are returned as the signal content by the signal search endpoints.
//	@Description
//	@Description	**Request format**
//	@Description
//	@Description	The request is multipart/form-data with one document per request:
//	@Description	- send the form fields first (batch_ref, local_ref and any optional fields) - **the file must be the last part**
//	@Description	- the file part must be named `file` and must include a filename - the filename is stored as the document's name
//	@Description
//	@Description	The request body looks like this:
//	@Description	```
//	@Description	--boundary
//	@Description	Content-Disposition: form-data; name="batch_ref"
//	@Description
//	@Description	daily-2026-09-26
//	@Description	--boundary
//	@Description	Content-Disposition: form-data; name="local_ref"
//	@Description
//	@Description	bol-0042
//	@Description	--boundary
//	@Description	Content-Disposition: form-data; name="file"; filename="BL-0042.pdf"
//	@Description	Content-Type: application/pdf
//	@Description
//	@Description	(the document bytes)
//	@Description	--boundary--
//	@Description	```
//	@Description
//	@Description	**Supported formats**
//	@Description
//	@Description	Only common business document formats can be uploaded: PDF, JPEG, PNG and XML. Other files are rejected with 415 unsupported_media_type.
//	@Description	The format is detected from the content of the file - the Content-Type of the file part is ignored.
//	@Description	XML documents must start with an XML declaration (<?xml ...?>).
//	@Description
//	@Description	The filename extension must match the format (.pdf, .jpg or .jpeg, .png, .xml - in any case), otherwise the upload is rejected with 400 malformed_body.
//	@Description
//	@Description	**Versions**
//	@Description
//	@Description	Uploads for a local_ref you have already used are compared with its latest version:
//	@Description	- the same file with the same filename creates no new version: the response contains the signal_id, signal_version_id and version_number of the existing latest version, with unchanged=true - so uploads can be safely retried
//	@Description	- a different file, or the same file with a different filename, creates a new version (including a file that matches an older version)
//	@Description	- re-uploading a withdrawn document creates a new version and reactivates it
//	@Description	- supplying a different correlation_id creates a new version with the new link (omitting correlation_id keeps the existing link)
//	@Description
//	@Description	Unchanged uploads are not counted in the batch they were sent in - the existing version belongs to the batch that stored it.
//	@Description	The same file uploaded with a different local_ref is a separate document.
//	@Description
//	@Description	Rejected uploads are recorded against the batch (see the batch status endpoint).
//	@Description
//	@Description	**Optional fields**
//	@Description	- correlation_id: the ID of a signal of any type in the same ISN that this document relates to (e.g. the consignment a bill of lading belongs to).
//	@Description	The upload is rejected with 422 invalid_correlation_id if the signal does not exist in the ISN.
//	@Description	(To send a document to the ISN of the signal it relates to without specifying the ISN, use _Upload a Document via Router_.)
//	@Description	- sha256: the sha256 of the document as lowercase hex, computed by the sender before uploading. If it does not match the uploaded file the upload is rejected with 400 malformed_body and no version is created.
//	@Description	Use it to confirm the document stored is exactly the one you sent. If it is omitted, the sha256 computed by the server is returned in the response and can be used to check the upload afterwards.
//
//	@Accept			multipart/form-data
//	@Param			isn_slug			path		string	true	"ISN slug"			example(sample-isn)
//	@Param			signal_type_slug	path		string	true	"signal type slug"	example(bill-of-lading)
//	@Param			sem_ver				path		string	true	"version"			example(1.0.0)
//	@Param			batch_ref			formData	string	true	"batch reference (up to 128 alphanumeric characters, hyphens and underscores)"
//	@Param			local_ref			formData	string	true	"your reference for the document"
//	@Param			correlation_id		formData	string	false	"the ID of a signal in the same ISN that this document relates to"
//	@Param			sha256				formData	string	false	"the sha256 of the document (lowercase hex) - the upload is rejected if it does not match"
//	@Param			file				formData	file	true	"the document (must be the last part of the request)"
//
//	@Success		200					{object}	handlers.DocumentUploadResponse
//	@Failure		400					{object}	responses.ErrorResponse	"malformed_body | invalid_url_param"
//	@Failure		401					{object}	responses.ErrorResponse	"authentication_error"
//	@Failure		403					{object}	responses.ErrorResponse	"forbidden"
//	@Failure		404					{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		413					{object}	responses.ErrorResponse	"request_too_large"
//	@Failure		415					{object}	responses.ErrorResponse	"unsupported_media_type"
//	@Failure		422					{object}	responses.ErrorResponse	"invalid_correlation_id"
//	@Failure		500					{object}	responses.ErrorResponse	"database_error | internal_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/upload [post]
//
// This function should be called after the RequireAccessPermission middleware has checked the account has write permission for the ISN.
func (h *DocumentsHandler) UploadDocument(w http.ResponseWriter, r *http.Request) error {
	isnSlug := r.PathValue("isn_slug")

	upload, err := h.readUploadFields(r)
	if err != nil {
		return err
	}

	if err := h.startBatch(r.Context(), upload); err != nil {
		return err
	}

	return h.storeDocumentSignal(w, r, upload, isnSlug)
}

// RouteDocument godoc
//
//	@Summary		Upload a Document via Router
//	@Tags			Signal Exchange
//
//	@Description	Upload a document without specifying the target ISN: the document is sent to the ISN of the signal it is correlated with
//	@Description	(e.g. a bill of lading is sent to the ISN that received its consignment).
//	@Description
//	@Description	correlation_id is required - routing rules are not used for documents (they match on fields in JSON signals).
//	@Description	The upload is rejected with 422 invalid_correlation_id if the correlated signal is not found,
//	@Description	and with 403 forbidden if the account does not have write permission on the correlated signal's ISN.
//	@Description
//	@Description	Other than the ISN resolution, this endpoint behaves the same way as the standard _Upload a Document_ endpoint
//	@Description	(request format, supported formats, versions and the optional sha256 field).
//	@Description
//	@Description	The response includes the isn_slug of the ISN the document was sent to.
//
//	@Accept			multipart/form-data
//	@Param			signal_type_slug	path		string	true	"signal type slug"	example(bill-of-lading)
//	@Param			sem_ver				path		string	true	"version"			example(1.0.0)
//	@Param			batch_ref			formData	string	true	"batch reference (up to 128 alphanumeric characters, hyphens and underscores)"
//	@Param			local_ref			formData	string	true	"your reference for the document"
//	@Param			correlation_id		formData	string	true	"the ID of the signal this document relates to - the document is sent to the same ISN"
//	@Param			sha256				formData	string	false	"the sha256 of the document (lowercase hex) - the upload is rejected if it does not match"
//	@Param			file				formData	file	true	"the document (must be the last part of the request)"
//
//	@Success		200					{object}	handlers.DocumentUploadResponse
//	@Failure		400					{object}	responses.ErrorResponse	"malformed_body | invalid_url_param"
//	@Failure		401					{object}	responses.ErrorResponse	"authentication_error"
//	@Failure		403					{object}	responses.ErrorResponse	"forbidden"
//	@Failure		404					{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		413					{object}	responses.ErrorResponse	"request_too_large"
//	@Failure		415					{object}	responses.ErrorResponse	"unsupported_media_type"
//	@Failure		422					{object}	responses.ErrorResponse	"invalid_correlation_id"
//	@Failure		500					{object}	responses.ErrorResponse	"database_error | internal_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/router/signal-types/{signal_type_slug}/v{sem_ver}/signals/upload [post]
//
// This handler should be used with RequireValidAccessToken middleware.
// It can't be used with RequireAccessPermission since the ISN is not in the URL - the handler checks the write permission
// against the claims once the ISN has been resolved.
func (h *DocumentsHandler) RouteDocument(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	claims, ok := auth.ContextClaims(ctx)
	if !ok {
		return apperrors.InternalError("could not get claims from context", nil)
	}

	upload, err := h.readUploadFields(r)
	if err != nil {
		return err
	}

	if upload.correlationID == nil {
		return apperrors.MalformedBody("correlation_id is required - the document is sent to the ISN of the correlated signal", nil)
	}

	if err := h.startBatch(ctx, upload); err != nil {
		return err
	}

	// resolve the ISN from the correlated signal (failures are recorded without an ISN, as with unroutable json signals)
	isn, err := h.queries.GetIsnBySignalID(ctx, *upload.correlationID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return h.recordFailure(ctx, upload, nil, apperrors.InvalidCorrelationID(fmt.Sprintf("correlation_id %v not found or its ISN is not in use", *upload.correlationID), nil))
		}
		return h.recordFailure(ctx, upload, nil, apperrors.DatabaseError("database error", err))
	}

	// check the account can write this signal type to the ISN (the checks RequireAccessPermission does for the ISN upload endpoint)
	if err := auth.CheckIsnWritePermission(claims, isn.Slug, upload.signalTypePath()); err != nil {
		permissionErr, ok := errors.AsType[*apperrors.HTTPError](err)
		if !ok {
			permissionErr = apperrors.InternalError("could not check the ISN permissions", err)
		}
		return h.recordFailure(ctx, upload, &isn.Slug, permissionErr)
	}

	logger.ContextWithLogAttrs(ctx,
		slog.String("isn_slug", isn.Slug),
	)

	return h.storeDocumentSignal(w, r, upload, isn.Slug)
}

// documentUpload is an upload request whose form fields have been read.
// The file has not been read yet - storeDocumentSignal reads it from filePart.
type documentUpload struct {
	accountID      uuid.UUID
	signalTypeSlug string
	semVer         string
	batchRef       string
	localRef       string
	correlationID  *uuid.UUID
	declaredSHA256 string

	// batch is set by startBatch
	batch database.UpsertSignalBatchRow

	multipartReader *multipart.Reader
	filePart        *multipart.Part
}

func (u *documentUpload) signalTypePath() string {
	return fmt.Sprintf("%v/v%v", u.signalTypeSlug, u.semVer)
}

// readUploadFields checks the signal type is a document type and reads the form fields sent before the file.
// The form fields must be sent before the file, so the request can be checked before the document is read.
func (h *DocumentsHandler) readUploadFields(r *http.Request) (*documentUpload, error) {
	upload := &documentUpload{
		signalTypeSlug: r.PathValue("signal_type_slug"),
		semVer:         r.PathValue("sem_ver"),
	}

	// only document signal types can be uploaded
	switch contentKind := h.schemaCache.ContentKind(upload.signalTypePath()); contentKind {
	case signalsd.ContentKindDocument:
	case "":
		return nil, apperrors.InvalidURLParam(fmt.Sprintf("signal type %s not found", upload.signalTypePath()), nil)
	default:
		return nil, apperrors.InvalidURLParam(fmt.Sprintf("signal type %s is a %s signal type - only document signal types can be uploaded", upload.signalTypePath(), contentKind), nil)
	}

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		return nil, apperrors.InternalError("could not get accountID from context", nil)
	}
	upload.accountID = accountID

	multipartReader, err := r.MultipartReader()
	if err != nil {
		return nil, apperrors.MalformedBody("the request must be multipart/form-data", err)
	}
	upload.multipartReader = multipartReader

	// read the form fields until the file part (or the end of the request) is reached
	var correlationIDValue string
	for {
		part, err := multipartReader.NextPart()
		if errors.Is(err, io.EOF) {
			break // no file was sent
		}
		if err != nil {
			if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
				return nil, apperrors.RequestTooLarge(maxBytesErr.Limit)
			}
			return nil, apperrors.MalformedBody("the request could not be read", err)
		}

		if part.FormName() == "file" {
			upload.filePart = part
			break // the file is read by storeDocumentSignal
		}

		value, err := io.ReadAll(io.LimitReader(part, maxFormFieldSize+1))
		if err != nil {
			if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
				return nil, apperrors.RequestTooLarge(maxBytesErr.Limit)
			}
			return nil, apperrors.MalformedBody("the request could not be read", err)
		}
		if len(value) > maxFormFieldSize {
			return nil, apperrors.MalformedBody(fmt.Sprintf("form field %q is longer than %d bytes", part.FormName(), maxFormFieldSize), nil)
		}

		switch part.FormName() {
		case "batch_ref":
			upload.batchRef = string(value)
		case "local_ref":
			upload.localRef = string(value)
		case "correlation_id":
			correlationIDValue = string(value)
		case "sha256":
			upload.declaredSHA256 = strings.ToLower(string(value))
		default:
			return nil, apperrors.MalformedBody(fmt.Sprintf("unexpected form field %q", part.FormName()), nil)
		}
	}

	if upload.batchRef == "" {
		return nil, apperrors.MalformedBody("batch_ref is required (form fields must be sent before the file)", nil)
	}

	if !batchRefRegexp.MatchString(upload.batchRef) {
		return nil, apperrors.MalformedBody("batch_ref must be less than 128 characters and can only contain alphanumeric characters, hyphens, and underscores", nil)
	}

	if upload.localRef == "" {
		return nil, apperrors.MalformedBody("local_ref is required (form fields must be sent before the file)", nil)
	}

	if correlationIDValue != "" {
		correlationID, err := uuid.Parse(correlationIDValue)
		if err != nil {
			return nil, apperrors.MalformedBody("correlation_id is not a valid UUID", nil)
		}
		upload.correlationID = &correlationID
	}

	if upload.declaredSHA256 != "" && !sha256Regexp.MatchString(upload.declaredSHA256) {
		return nil, apperrors.MalformedBody("sha256 must be 64 hex characters", nil)
	}

	if upload.filePart == nil {
		return nil, apperrors.MalformedBody("file is required", nil)
	}

	if upload.filePart.FileName() == "" {
		return nil, apperrors.MalformedBody("the file part must include a filename", nil)
	}

	return upload, nil
}

// startBatch starts the batch (a new batch is started if the batch ref has not been received previously).
// From here on, rejected uploads are recorded against the batch so they are reported by the batch status endpoint.
func (h *DocumentsHandler) startBatch(ctx context.Context, upload *documentUpload) error {
	batch, err := h.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
		BatchRef:  upload.batchRef,
		AccountID: upload.accountID,
	})
	if err != nil {
		return apperrors.DatabaseError("database error", err)
	}
	upload.batch = batch

	logger.ContextWithLogAttrs(ctx,
		slog.String("batch_ref", batch.BatchRef),
		slog.String("local_ref", upload.localRef),
	)
	return nil
}

// recordFailure records a rejected upload against the batch and returns the error.
// isnSlug is nil if the ISN is not known (a routed upload whose ISN could not be resolved).
func (h *DocumentsHandler) recordFailure(ctx context.Context, upload *documentUpload, isnSlug *string, uploadErr *apperrors.HTTPError) error {
	recordSignalProcessingFailures(ctx, h.queries, upload.batch.ID, isnSlug, upload.signalTypeSlug, upload.semVer, []FailedSignal{{
		LocalRef:     upload.localRef,
		ErrorCode:    uploadErr.Code.String(),
		ErrorMessage: uploadErr.Message,
	}})
	return uploadErr
}

// storeDocumentSignal reads the file, stores it and creates the document signal version on the ISN.
// The account's write permission on the ISN must have been checked.
func (h *DocumentsHandler) storeDocumentSignal(w http.ResponseWriter, r *http.Request, upload *documentUpload, isnSlug string) error {
	ctx := r.Context()

	reject := func(uploadErr *apperrors.HTTPError) error {
		return h.recordFailure(ctx, upload, &isnSlug, uploadErr)
	}

	// limit the size of the document (the request size limit also allows for the form fields and multipart headers)
	file := bufio.NewReader(http.MaxBytesReader(w, upload.filePart, h.maxDocumentSize))

	// 1. detect the type of document from its content - only supported document formats can be uploaded.
	// The Content-Type declared by the client is ignored (it can't be trusted, and many clients send application/octet-stream)
	start, err := file.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) {
		if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return reject(apperrors.RequestTooLarge(maxBytesErr.Limit))
		}
		return reject(apperrors.MalformedBody("the request could not be read", err))
	}
	if len(start) == 0 {
		return reject(apperrors.MalformedBody("the file is empty", nil))
	}

	// a UTF-8 byte order mark is removed first: http.DetectContentType treats content that starts with one as text,
	// but XML written by Windows tools often includes it
	mimeType, _, err := mime.ParseMediaType(http.DetectContentType(bytes.TrimPrefix(start, []byte("\xef\xbb\xbf"))))
	allowedExtensions, supported := signalsd.SupportedDocumentFormats[mimeType]
	if err != nil || !supported {
		supportedMimeTypes := slices.Sorted(maps.Keys(signalsd.SupportedDocumentFormats))
		return reject(apperrors.UnsupportedMediaType(fmt.Sprintf("the file is not a supported document format (%s)", strings.Join(supportedMimeTypes, ", ")), nil))
	}

	// the filename extension must match the format
	extension := strings.ToLower(filepath.Ext(upload.filePart.FileName()))
	if !slices.Contains(allowedExtensions, extension) {
		return reject(apperrors.MalformedBody(fmt.Sprintf("the filename extension %q does not match the document format %s (use %s)",
			extension, mimeType, strings.Join(allowedExtensions, " or ")), nil))
	}

	// 2. store the document - the store computes the sha256 as it reads
	key, size, err := h.documentStore.Put(ctx, upload.accountID, file)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err // the client went away (reported as 499)
		}
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return reject(apperrors.RequestTooLarge(h.maxDocumentSize))
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return reject(apperrors.MalformedBody("the file could not be read - the request ended unexpectedly", err))
		}
		return reject(apperrors.InternalError("could not store the document", err))
	}

	// the file must be the last part (fields sent after the file would otherwise be ignored)
	if _, err := upload.multipartReader.NextPart(); !errors.Is(err, io.EOF) {
		return reject(apperrors.MalformedBody("the file must be the last part of the request", err))
	}

	// 3. check the declared sha256 (on a mismatch the content stays stored under its real sha256 - this is harmless)
	if upload.declaredSHA256 != "" && upload.declaredSHA256 != key.SHA256 {
		return reject(apperrors.MalformedBody(fmt.Sprintf("the declared sha256 does not match the file (the sha256 of the file is %s)", key.SHA256), nil))
	}

	documentMetadata := DocumentMetadata{
		Name:      upload.filePart.FileName(),
		MimeType:  mimeType,
		SizeBytes: size,
		SHA256:    key.SHA256,
	}

	// 4. create the signal version (the content is always stored before the signal that refers to it)
	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return reject(apperrors.DatabaseError("database error", err))
	}
	defer tx.Rollback(ctx) // no-op once the transaction is committed

	queries := h.queries.WithTx(tx)

	// if the upload is the same as the latest version (the same file with the same name), return that version rather than creating a new one.
	// Withdrawn signals and changes to the correlation_id always create a new version (as with json signals).
	latest, err := queries.GetLatestSignalVersionByLocalRef(ctx, database.GetLatestSignalVersionByLocalRefParams{
		AccountID:      upload.accountID,
		SignalTypeSlug: upload.signalTypeSlug,
		SemVer:         upload.semVer,
		LocalRef:       upload.localRef,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return reject(apperrors.DatabaseError("database error", err))
	}
	if err == nil && !latest.IsWithdrawn && (upload.correlationID == nil || *upload.correlationID == latest.CorrelationID) {
		var latestDocumentMetadata DocumentMetadata
		if err := json.Unmarshal(latest.Content, &latestDocumentMetadata); err != nil {
			return reject(apperrors.InternalError("could not read the latest version of the document", err))
		}
		// the mime type and size are determined by the content, so this compares the sha256 and name
		if latestDocumentMetadata == documentMetadata {
			return responses.JSON(w, http.StatusOK, DocumentUploadResponse{
				IsnSlug:          isnSlug,
				BatchRef:         upload.batch.BatchRef,
				LocalRef:         upload.localRef,
				SignalID:         latest.SignalID,
				SignalVersionID:  latest.SignalVersionID,
				VersionNumber:    latest.VersionNumber,
				DocumentMetadata: latestDocumentMetadata,
				Unchanged:        true,
			})
		}
	}

	// create or update the signal master record
	var signalID uuid.UUID
	if upload.correlationID == nil {
		signalID, err = queries.CreateSignal(ctx, database.CreateSignalParams{
			AccountID:      upload.accountID,
			LocalRef:       upload.localRef,
			IsnSlug:        isnSlug,
			SignalTypeSlug: upload.signalTypeSlug,
			SemVer:         upload.semVer,
		})
	} else {
		isValid, validateErr := queries.ValidateCorrelationID(ctx, database.ValidateCorrelationIDParams{
			CorrelationID: *upload.correlationID,
			IsnSlug:       isnSlug,
		})
		if validateErr != nil {
			return reject(apperrors.DatabaseError("database error", validateErr))
		}
		if !isValid {
			return reject(apperrors.InvalidCorrelationID(fmt.Sprintf("invalid correlation_id %v - signal does not exist in this ISN", *upload.correlationID), nil))
		}

		signalID, err = queries.CreateOrUpdateSignalWithCorrelationID(ctx, database.CreateOrUpdateSignalWithCorrelationIDParams{
			AccountID:      upload.accountID,
			LocalRef:       upload.localRef,
			CorrelationID:  *upload.correlationID,
			IsnSlug:        isnSlug,
			SignalTypeSlug: upload.signalTypeSlug,
			SemVer:         upload.semVer,
		})
	}
	if err != nil {
		// the insert checks the ISN and signal type are in use - if not it returns no rows
		// (possible if they were disabled after the access token was issued)
		if errors.Is(err, pgx.ErrNoRows) {
			return reject(apperrors.NotFound("the ISN or signal type is not in use", err))
		}
		return reject(apperrors.DatabaseError("database error", err))
	}

	content, err := json.Marshal(documentMetadata)
	if err != nil {
		return reject(apperrors.InternalError("could not create the document metadata", err))
	}

	version, err := queries.CreateSignalVersion(ctx, database.CreateSignalVersionParams{
		AccountID:      upload.accountID,
		SignalBatchID:  upload.batch.ID,
		Content:        content,
		LocalRef:       upload.localRef,
		SignalTypeSlug: upload.signalTypeSlug,
		SemVer:         upload.semVer,
	})
	if err != nil {
		return reject(apperrors.DatabaseError("database error", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return reject(apperrors.DatabaseError("database error", err))
	}

	return responses.JSON(w, http.StatusOK, DocumentUploadResponse{
		IsnSlug:          isnSlug,
		BatchRef:         upload.batch.BatchRef,
		LocalRef:         upload.localRef,
		SignalID:         signalID,
		SignalVersionID:  version.ID,
		VersionNumber:    version.VersionNumber,
		DocumentMetadata: documentMetadata,
		Unchanged:        false,
	})
}

// DownloadDocument godoc
//
//	@Summary		Download a Document
//	@Tags			Signal Exchange
//
//	@Description	Download the document stored with a document signal. The latest version is returned unless a version is requested.
//	@Description
//	@Description	Use the signal search endpoints to find documents - the search results include the signal_id, version_number and document metadata (name, mime_type, size_bytes and sha256).
//	@Description
//	@Description	The document is returned as an attachment with its original filename:
//	@Description	- Content-Type is the document's mime_type
//	@Description	- ETag is the document's sha256 (in quotes) - use it to check the document is the one you expected
//	@Description
//	@Description	Withdrawn documents are only returned with include_withdrawn=true.
//	@Description	Write-only accounts can only download the documents they uploaded, and the documents other accounts have correlated to their signals.
//
//	@Param			isn_slug			path	string	true	"ISN slug"												example(sample-isn)
//	@Param			signal_type_slug	path	string	true	"signal type slug"										example(bill-of-lading)
//	@Param			sem_ver				path	string	true	"version"												example(1.0.0)
//	@Param			signal_id			path	string	true	"signal ID"												example(4cedf4fa-2a01-4cbf-8668-6b44f8ac6e19)
//	@Param			version				query	integer	false	"the version to download (default: the latest version)"	example(1)
//	@Param			include_withdrawn	query	boolean	false	"return withdrawn documents (default: false)"
//
//	@Produce		application/pdf,image/jpeg,image/png,text/xml
//	@Success		200	{file}		file					"the document"
//	@Failure		400	{object}	responses.ErrorResponse	"invalid_url_param"
//	@Failure		401	{object}	responses.ErrorResponse	"authentication_error"
//	@Failure		403	{object}	responses.ErrorResponse	"forbidden"
//	@Failure		404	{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		500	{object}	responses.ErrorResponse	"database_error | internal_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/{signal_id}/content [get]
//
// This function should be called after the RequireIsnMembership middleware has checked the account is a member of the ISN.
// The document is streamed from the document store to the client.
func (h *DocumentsHandler) DownloadDocument(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%v/v%v", signalTypeSlug, semVer)

	// only document signal types have documents to download
	switch contentKind := h.schemaCache.ContentKind(signalTypePath); contentKind {
	case signalsd.ContentKindDocument:
	case "":
		return apperrors.InvalidURLParam(fmt.Sprintf("signal type %s not found", signalTypePath), nil)
	default:
		return apperrors.InvalidURLParam(fmt.Sprintf("signal type %s is a %s signal type - only document signal types can be downloaded", signalTypePath, contentKind), nil)
	}

	signalID, err := uuid.Parse(r.PathValue("signal_id"))
	if err != nil {
		return apperrors.InvalidURLParam("signal_id is not a valid UUID", nil)
	}

	var versionNumber *int32
	if versionValue := r.URL.Query().Get("version"); versionValue != "" {
		parsed, err := strconv.ParseInt(versionValue, 10, 32)
		if err != nil || parsed < 1 {
			return apperrors.InvalidURLParam("version must be a positive whole number", nil)
		}
		version := int32(parsed)
		versionNumber = &version
	}
	includeWithdrawn := r.URL.Query().Get("include_withdrawn") == "true"

	claims, ok := auth.ContextClaims(ctx)
	if !ok {
		return apperrors.InternalError("could not get claims from context", nil)
	}
	accountID, ok := auth.ContextAccountID(ctx)
	if !ok {
		return apperrors.InternalError("could not get accountID from context", nil)
	}

	signalVersion, err := h.queries.GetSignalVersion(ctx, database.GetSignalVersionParams{
		SignalID:       signalID,
		IsnSlug:        isnSlug,
		SignalTypeSlug: signalTypeSlug,
		SemVer:         semVer,
		VersionNumber:  versionNumber,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound("document not found", nil)
		}
		return apperrors.DatabaseError("database error", err)
	}

	// write-only accounts can only download the documents they uploaded and the documents correlated to their signals
	// (other documents are reported as not found, as in the signal search)
	isnPerms := claims.IsnPerms[isnSlug]
	if !isnPerms.CanRead && signalVersion.AccountID != accountID && signalVersion.CorrelatedToAccountID != accountID {
		return apperrors.NotFound("document not found", nil)
	}
	if signalVersion.IsWithdrawn && !includeWithdrawn {
		return apperrors.NotFound("the document has been withdrawn (use include_withdrawn=true to download it)", nil)
	}

	var documentMetadata DocumentMetadata
	if err := json.Unmarshal(signalVersion.Content, &documentMetadata); err != nil {
		return apperrors.InternalError("could not read the document metadata", err)
	}

	// the content is always stored before the signal version that refers to it, so failing to get it is a server fault
	document, err := h.documentStore.Get(ctx, documents.Key{AccountID: signalVersion.AccountID, SHA256: documentMetadata.SHA256})
	if err != nil {
		return apperrors.InternalError("could not get the document", err)
	}
	defer document.Close()

	w.Header().Set("Content-Type", documentMetadata.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(documentMetadata.SizeBytes, 10))
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": documentMetadata.Name}))
	w.Header().Set("ETag", fmt.Sprintf("%q", documentMetadata.SHA256))
	w.Header().Set("X-Content-Type-Options", "nosniff")  // browsers must not guess a different type
	w.Header().Set("Cache-Control", "private, no-store") // documents can be withdrawn and access revoked, so they are not cached
	w.WriteHeader(http.StatusOK)

	// the status has been sent, so an error while streaming the document can only be logged
	if _, err := io.Copy(w, document); err != nil {
		logger.ContextRequestLogger(ctx).Warn("document download was not completed",
			slog.String("signal_id", signalID.String()),
			slog.String("error", err.Error()),
		)
	}
	return nil
}
