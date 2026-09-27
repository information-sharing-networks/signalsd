package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
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

// maxFormFieldSize is the maximum size of the form fields sent with a document (batch_ref, local_ref, correlation_id, sha256)
const maxFormFieldSize = 1024

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

	// Name is the file name supplied when the document was uploaded
	Name string `json:"name" example:"BL-2026-0042.pdf"`

	// MimeType is the declared content type, or the detected type if none was supplied by the client
	MimeType string `json:"mime_type" example:"application/pdf"`

	// SizeBytes is the size of the document in bytes (measured by the server when the document is uploaded)
	SizeBytes int64 `json:"size_bytes" example:"482133"`

	// SHA256 is the sha256 hash of the document as lowercase hex (computed by the server when the document is uploaded)
	SHA256 string `json:"sha256" example:"2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"`
}

// DocumentUploadResponse describes the stored document signal version
type DocumentUploadResponse struct {
	BatchRef        string    `json:"batch_ref" example:"daily-sync-2026-04-02"`
	LocalRef        string    `json:"local_ref" example:"bol-2026-0042"`
	SignalID        uuid.UUID `json:"signal_id" example:"b8ded113-ac0e-4a2c-a89f-0876fe97b440"`
	SignalVersionID uuid.UUID `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`
	VersionNumber   int32     `json:"version_number" example:"1"`
	DocumentMetadata
	Unchanged bool `json:"unchanged" example:"false"` // true if the document is identical to the latest version, in which case no new version is created
}

// UploadDocument godoc
//
//	@Summary		Upload a Document
//	@Tags			Signal Exchange
//
//	@Description	Upload a document (e.g. a PDF bill of lading) to a document signal type.
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
//	@Description	Uploading the same document again for a local_ref does not create a new version: the latest version is returned with unchanged=true.
//	@Description	This means uploads can be safely retried.
//	@Description
//	@Description	Rejected uploads are recorded against the batch (see the batch status endpoint).
//	@Description
//	@Description	**Optional fields**
//	@Description	- correlation_id: the ID of a signal of any type in the same ISN that this document relates to (e.g. the consignment a bill of lading belongs to).
//	@Description	The upload is rejected with 422 invalid_correlation_id if the signal does not exist in the ISN.
//	@Description	- sha256: the sha256 of the document as lowercase hex, computed by the sender before uploading. If it does not match the uploaded file the upload is rejected with 400 malformed_body and no version is created.
//	@Description	Use it to confirm the document stored is exactly the one you sent. If it is omitted, the sha256 computed by the server is returned in the response and can be used to check the upload afterwards.
//	@Description	- the Content-Type of the file part: used as the document's mime_type. If it is omitted or is application/octet-stream, the type is detected from the start of the document.
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
//	@Failure		422					{object}	responses.ErrorResponse	"invalid_correlation_id"
//	@Failure		500					{object}	responses.ErrorResponse	"database_error | internal_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/documents [post]
//
// This function should be called after the RequireAccessPermission middleware has checked the account has write permission for the ISN.
func (h *DocumentsHandler) UploadDocument(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%v/v%v", signalTypeSlug, semVer)

	// only document signal types can be uploaded to this endpoint
	if err := h.schemaCache.CheckContentKind(signalTypePath, signalsd.ContentKindDocument); err != nil {
		return apperrors.InvalidURLParam(err.Error(), nil)
	}

	accountID, ok := auth.ContextAccountID(ctx)
	if !ok {
		return apperrors.InternalError("could not get accountID from context", nil)
	}

	// 1. read the form fields - they must be sent before the file, so the request can be checked before the document is read
	multipartReader, err := r.MultipartReader()
	if err != nil {
		return apperrors.MalformedBody("the request must be multipart/form-data", err)
	}

	var batchRef, localRef, correlationIDValue, declaredSHA256 string
	var filePart *multipart.Part
	for filePart == nil {
		part, err := multipartReader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return requestReadError(err)
		}

		if part.FormName() == "file" {
			filePart = part
			break
		}

		value, err := io.ReadAll(io.LimitReader(part, maxFormFieldSize+1))
		if err != nil {
			return requestReadError(err)
		}
		if len(value) > maxFormFieldSize {
			return apperrors.MalformedBody(fmt.Sprintf("form field %q is longer than %d bytes", part.FormName(), maxFormFieldSize), nil)
		}

		switch part.FormName() {
		case "batch_ref":
			batchRef = string(value)
		case "local_ref":
			localRef = string(value)
		case "correlation_id":
			correlationIDValue = string(value)
		case "sha256":
			declaredSHA256 = strings.ToLower(string(value))
		default:
			return apperrors.MalformedBody(fmt.Sprintf("unexpected form field %q", part.FormName()), nil)
		}
	}

	if batchRef == "" {
		return apperrors.MalformedBody("batch_ref is required (form fields must be sent before the file)", nil)
	}

	if !batchRefRegexp.MatchString(batchRef) {
		return apperrors.MalformedBody("batch_ref must be less than 128 characters and can only contain alphanumeric characters, hyphens, and underscores", nil)
	}

	if localRef == "" {
		return apperrors.MalformedBody("local_ref is required (form fields must be sent before the file)", nil)
	}

	var correlationID *uuid.UUID
	if correlationIDValue != "" {
		parsed, err := uuid.Parse(correlationIDValue)
		if err != nil {
			return apperrors.MalformedBody("correlation_id is not a valid UUID", nil)
		}
		correlationID = &parsed
	}

	if declaredSHA256 != "" && !sha256Regexp.MatchString(declaredSHA256) {
		return apperrors.MalformedBody("sha256 must be 64 hex characters", nil)
	}

	if filePart == nil {
		return apperrors.MalformedBody("file is required", nil)
	}

	if filePart.FileName() == "" {
		return apperrors.MalformedBody("the file part must include a filename", nil)
	}

	// start the batch (a new batch is started if the batch ref has not been received previously).
	// From here on, rejected uploads are recorded against the batch so they are reported by the batch status endpoint.
	batch, err := h.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
		BatchRef:  batchRef,
		AccountID: accountID,
	})
	if err != nil {
		return apperrors.DatabaseError("database error", err)
	}

	logger.ContextWithLogAttrs(ctx,
		slog.String("batch_ref", batch.BatchRef),
		slog.String("local_ref", localRef),
	)

	reject := func(uploadErr *apperrors.HTTPError) error {
		recordSignalProcessingFailures(ctx, h.queries, batch.ID, signalTypeSlug, semVer, []FailedSignal{{
			LocalRef:     localRef,
			ErrorCode:    uploadErr.Code.String(),
			ErrorMessage: uploadErr.Message,
		}})
		return uploadErr
	}

	// limit the size of the document (the request size limit also allows for the form fields and multipart headers)
	file := bufio.NewReader(http.MaxBytesReader(w, filePart, h.maxDocumentSize))

	// 2. use the declared content type, or detect it from the start of the document
	// (application/octet-stream is what most clients send when they don't know the type)
	mimeType := ""
	if mediaType, params, err := mime.ParseMediaType(filePart.Header.Get("Content-Type")); err == nil && mediaType != "application/octet-stream" {
		mimeType = mime.FormatMediaType(mediaType, params)
	}
	if mimeType == "" {
		start, err := file.Peek(512)
		if err != nil && !errors.Is(err, io.EOF) {
			return reject(requestReadError(err))
		}
		mimeType = http.DetectContentType(start)
	}

	// 3. store the document - the store computes the sha256 as it reads
	key, size, err := h.documentStore.Put(ctx, accountID, file)
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
	if size == 0 {
		return reject(apperrors.MalformedBody("the file is empty", nil))
	}

	// the file must be the last part (fields sent after the file would otherwise be ignored)
	if _, err := multipartReader.NextPart(); !errors.Is(err, io.EOF) {
		return reject(apperrors.MalformedBody("the file must be the last part of the request", err))
	}

	// 4. check the declared sha256 (on a mismatch the content stays stored under its real sha256 - this is harmless)
	if declaredSHA256 != "" && declaredSHA256 != key.SHA256 {
		return reject(apperrors.MalformedBody(fmt.Sprintf("the declared sha256 does not match the file (the sha256 of the file is %s)", key.SHA256), nil))
	}

	documentMetadata := DocumentMetadata{
		Name:      filePart.FileName(),
		MimeType:  mimeType,
		SizeBytes: size,
		SHA256:    key.SHA256,
	}

	// 5. create the signal version (the content is always stored before the signal that refers to it)
	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return reject(apperrors.DatabaseError("database error", err))
	}
	defer tx.Rollback(ctx) // no-op once the transaction is committed

	queries := h.queries.WithTx(tx)

	// if the document is the same as the latest version, return that version rather than creating a new one.
	// Withdrawn signals and changes to the correlation_id always create a new version (as with json signals).
	latest, err := queries.GetLatestSignalVersionByLocalRef(ctx, database.GetLatestSignalVersionByLocalRefParams{
		AccountID:      accountID,
		SignalTypeSlug: signalTypeSlug,
		SemVer:         semVer,
		LocalRef:       localRef,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return reject(apperrors.DatabaseError("database error", err))
	}
	if err == nil && !latest.IsWithdrawn && (correlationID == nil || *correlationID == latest.CorrelationID) {
		var latestDocumentMetadata DocumentMetadata
		if err := json.Unmarshal(latest.Content, &latestDocumentMetadata); err != nil {
			return reject(apperrors.InternalError("could not read the latest version of the document", err))
		}
		if latestDocumentMetadata.SHA256 == key.SHA256 {
			return responses.JSON(w, http.StatusOK, DocumentUploadResponse{
				BatchRef:         batch.BatchRef,
				LocalRef:         localRef,
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
	if correlationID == nil {
		signalID, err = queries.CreateSignal(ctx, database.CreateSignalParams{
			AccountID:      accountID,
			LocalRef:       localRef,
			IsnSlug:        isnSlug,
			SignalTypeSlug: signalTypeSlug,
			SemVer:         semVer,
		})
	} else {
		isValid, validateErr := queries.ValidateCorrelationID(ctx, database.ValidateCorrelationIDParams{
			CorrelationID: *correlationID,
			IsnSlug:       isnSlug,
		})
		if validateErr != nil {
			return reject(apperrors.DatabaseError("database error", validateErr))
		}
		if !isValid {
			return reject(apperrors.InvalidCorrelationID(fmt.Sprintf("invalid correlation_id %v - signal does not exist in this ISN", *correlationID), nil))
		}

		signalID, err = queries.CreateOrUpdateSignalWithCorrelationID(ctx, database.CreateOrUpdateSignalWithCorrelationIDParams{
			AccountID:      accountID,
			LocalRef:       localRef,
			CorrelationID:  *correlationID,
			IsnSlug:        isnSlug,
			SignalTypeSlug: signalTypeSlug,
			SemVer:         semVer,
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
		AccountID:      accountID,
		SignalBatchID:  batch.ID,
		Content:        content,
		LocalRef:       localRef,
		SignalTypeSlug: signalTypeSlug,
		SemVer:         semVer,
	})
	if err != nil {
		return reject(apperrors.DatabaseError("database error", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return reject(apperrors.DatabaseError("database error", err))
	}

	return responses.JSON(w, http.StatusOK, DocumentUploadResponse{
		BatchRef:         batch.BatchRef,
		LocalRef:         localRef,
		SignalID:         signalID,
		SignalVersionID:  version.ID,
		VersionNumber:    version.VersionNumber,
		DocumentMetadata: documentMetadata,
		Unchanged:        false,
	})
}

// requestReadError returns 413 request_too_large if reading the request body failed because it is larger than the
// request size limit, otherwise 400 malformed_body
func requestReadError(err error) *apperrors.HTTPError {
	if maxBytesErr, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return apperrors.RequestTooLarge(maxBytesErr.Limit)
	}
	return apperrors.MalformedBody("the request could not be read", err)
}
