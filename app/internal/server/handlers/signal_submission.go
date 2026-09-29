package handlers

// signal submission: the endpoints that store JSON and event signals.
// - CreateSignals stores signals on the ISN in the URL
// - RouteSignals resolves the ISN for each signal (by correlation_id or the signal type's routing rules)
//
// Both endpoints share the same steps (readSignalsRequest, startBatch, then storeJSONSignal or storeEventSignal for each signal)
// and differ only in how the ISN is found - the same structure as the document upload endpoints in documents.go.
// Event signals are stored by storeEventSignal (see events.go), which adds the checks that make events immutable.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/jackc/pgx/v5"
)

// SubmittedSignal is a json or event signal sent to the Submit Signals endpoints
type SubmittedSignal struct {

	// LocalRef is supplied by the sender and uniquely identify each signal
	// Repeat deliveries of the same LocalRef are treated as updates to the original signal
	LocalRef string `json:"local_ref" example:"item_id_#1"`

	// CorrelationID is an optional ID for another signal in the same ISN - the submitted signal is linked to the correlated signal
	CorrelationID *uuid.UUID `json:"correlation_id" example:"75b45fe1-ecc2-4629-946b-fd9058c3b2ca"` //optional - supply the id of another signal if you want to link to it

	// Content is the json payload that is validated against the stored signal type schema
	Content json.RawMessage `json:"content" swaggertype:"object"`
}

// CreateSignalRequest contains the http request body used when submitting signals
type CreateSignalsRequest struct {

	// BatchRef groups signals under a sender-chosen label
	BatchRef string `json:"batch_ref" example:"daily-sync-2026-04-02"`

	// Signals - the list of signals to be loaded
	Signals []SubmittedSignal `json:"signals"`
}

// batcRefRegexp - batch refs must be alphanumeric, hyphens, and underscores only, length 1–128.
var batchRefRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// IsnResult holds stored and failed signals
// This is also used by Signals Router (which can route to more than one ISN)
type IsnResult struct {

	// IsnSlug is the target ISN
	IsnSlug string `json:"isn_slug" example:"sample-isn"`

	// SignalTypePath is the target Signal Type
	SignalTypePath string `json:"signal_type_path" example:"signal-type-1/v0.0.1"`

	// StoredSignals is the list of signals sucessfully processed
	StoredSignals []StoredSignal `json:"stored_signals"`

	// Failed Signals is the list of signals that could not be loaded to the target ISN
	FailedSignals []FailedSignal `json:"failed_signals,omitempty"`
}

// SignalSubmissionResponse is the response body for signal submission endpoints.
// Results is a slice of per-ISN outcomes. For the standard endpoint it always contains
type SignalSubmissionResponse struct {

	// BatchRef is the client supplied batch identifier
	BatchRef string `json:"batch_ref" example:"daily-sync-2026-04-02"`

	// AccountID is the account that sent the data
	AccountID uuid.UUID `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`

	// Results contains per-ISN outcomes.  It will only contain one entry for standard signal submisions (the ISN from the URL)
	Results []IsnResult `json:"results"`

	// UnroutableSignals is only included when using the Signal Router (N/A for standard signal submission)
	UnroutableSignals []FailedSignal `json:"unroutable_signals,omitempty"`

	// Summary contains the summary of counts
	Summary CreateSignalsSummary `json:"summary"`
}

type StoredSignal struct {
	// LocalRef is the sender-supplied identifier for the signal
	LocalRef string `json:"local_ref" example:"item_id_#1"`

	//SignalID is the server generated ID for this signal
	SignalID uuid.UUID `json:"signal_id" example:"b8ded113-ac0e-4a2c-a89f-0876fe97b440"`

	//SignalVersionID is the server generated version ID for the version of this signal created by the handler
	SignalVersionID uuid.UUID `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`

	// VersionNumber is the version created by the server
	//(where the same localRef is received in subsequent loads the versionNumber is incremented)
	VersionNumber int32 `json:"version_number" example:"1"`

	// Unchanged is true if an event was resubmitted with the same content and correlation_id - the existing version is returned
	// and no new version is created (only used for event signals)
	Unchanged bool `json:"unchanged,omitempty" example:"false"`
}

type FailedSignal struct {
	LocalRef     string `json:"local_ref" example:"item_id_#2"`
	ErrorCode    string `json:"error_code" example:"validation_error"`
	ErrorMessage string `json:"error_message" example:"field 'name' is required"`
}

// recordSignalProcessingFailures writes the details of the signals that could not be stored to the
// signal_processing_failures table (these are reported by the batch status endpoint).
//
// isnSlug is the ISN the signals were sent to (nil for signals the signal router could not route to an ISN).
//
// The signals in the batch have already been processed by the time this is called, so a failure to write
// the detail is logged rather than returned - the caller still reports the outcome to the client.
// Used by the direct signal post, the signals router and the document upload.
func recordSignalProcessingFailures(ctx context.Context, queries *database.Queries, batchID uuid.UUID, isnSlug *string, signalTypeSlug string, semVer string, failedSignals []FailedSignal) {
	for _, failed := range failedSignals {
		_, err := queries.CreateSignalProcessingFailureDetail(ctx, database.CreateSignalProcessingFailureDetailParams{
			SignalBatchID:    batchID,
			IsnSlug:          isnSlug,
			SignalTypeSlug:   signalTypeSlug,
			SignalTypeSemVer: semVer,
			LocalRef:         failed.LocalRef,
			ErrorCode:        failed.ErrorCode,
			ErrorMessage:     failed.ErrorMessage,
		})
		if err != nil {
			// the failure detail could not be recorded - log it so the missing batch detail can be traced
			logger.ContextWithLogAttrs(ctx,
				slog.String("local_ref", failed.LocalRef),
				slog.String("error", err.Error()),
			)
		}
	}
}

// CreateSignalsSummary is included in the response to summarise the outcome of the load
type CreateSignalsSummary struct {

	// TotalSubmitted is the count of all the signals supplied in the request
	// StoredCount+RejectedCount+UnroutableCount = TotalSubmitted
	TotalSubmitted int `json:"total_submitted" example:"3"`

	// StoredCount is the count of records sucessfully loaded
	StoredCount int `json:"stored_count" example:"1"`

	// Rejected Count is the list of records rejected due to issues with the contents of the record
	RejectedCount int `json:"rejected_count" example:"1"`

	// UnroutableCount is the count of signals sent to the Signal Router that could not be routed.
	// (e.g because no routing rule matched the data, or the account lacks write permission to the resolved ISN)
	//
	// This field is only supplied in responses to the Signals Router handler.
	UnroutableCount int `json:"unroutable_count,omitempty" example:"1"`
}

// CreateSignals godocs
//
//	@Summary		Submit Signals
//	@Tags			Signal Exchange
//
//	@Description	Submit JSON or event signals to an ISN (documents are sent to document signal types with Upload a Document)
//	@Description	- payloads must not mix signals of different types and are subject to the size limits defined on the site.
//	@Description	- The client-supplied local_ref must uniquely identify each signal of the specified signal type that will be supplied by the account.
//	@Description	- If a local reference is received more than once from an account for the specified signal_type a new version of the signal will be stored with a incremented version number.
//	@Description	- Optionally a correlation_id can be supplied - this will link the signal to a previously received signal. The correlated signal does not need to be owned by the same account but must be in the same ISN.
//	@Description
//	@Description	**Batches**
//	@Description
//	@Description	Batches group separate loads for reporting and tracking purposes.
//	@Description	- Signal loads are tracked under the batch_ref supplied as part of the request. To start a new batch
//	@Description	just supply a different batch_ref.
//	@Description	- Batches are stored at the account level and therefore can include signals from different ISNs and Signal Types
//	@Description	- Use the *Get Batch Status* endpoint to get a report on the status of signals loaded in a batch.
//	@Description
//	@Description	**Authentication**
//	@Description
//	@Description	Requires a valid access token.
//	@Description	The claims in the access token list the ISNs and signal_types that the account is permitted to use.
//	@Description
//	@Description	**Error handling**
//	@Description
//	@Description	Partial loads of the data are possible where the request is a valid format but individual signals fail to load
//	@Description	(e.g schema validation errors, incorrect correlations ids).
//	@Description	Failures are logged and trackable via the Batch Status endpoint.
//	@Description	The response provides an audit trail detailing the submission outcome.
//	@Description
//	@Description	Note the response structure is also used by the Signals Router endpoint, which can return results for multiple ISNs -
//	@Description	consequently the `Results` field is an array (one element for each ISN in the results).
//	@Description	There will only ever be a single entry when using this handler.
//	@Description
//	@Description	Errors that relate to the entire request  - e.g invalid json, authentication, permission and server errors (400, 401, 403, 500) -
//	@Description	return a simple error_code/error_message response rather than a detailed audit log.
//	@Description	The individual signal failures are not logged in this case, and the client must resupply the data once the problem is resolved.
//	@Description
//	@Description	**JSON Schema Validation**
//	@Description
//	@Description	the json contained in the `content` field is validated against the JSON schema specified for the signal type unless validation is disabled on the type definition.
//	@Description
//	@Description	When schema validation is disabled, basic checks are still done on the incoming data and the following issues create a 400 error and cause the entire payload to be rejected:
//	@Description	- invalid json format
//	@Description	- missing fields (batch_ref must be present; the array of signals must be in a json object called signals; and the content and local_ref must be present for each element of the signals array).
//	@Description
//	@Description	**Signal versions**
//	@Description
//	@Description	New versions are created when signals are resupplied using the same local_ref, e.g. because the client wants to correct a previously publsihed signal.
//	@Description	If a signal has been withdrawn it will be reactivated if you resubmit it using the same local_ref.
//	@Description
//	@Description	**Correlating signals**
//	@Description
//	@Description	Correlation IDs can be used to link signals together (a `correlation_id` is the `signals_id` of a previosuly submitted signal)
//	@Description	Signals can only be correlated within the same ISN.
//	@Description	If the supplied correlation_id is not found in the same ISN as the signal being submitted,
//	@Description	the response will contain a 422 or 207 status code and the error_code for the failed signal will be `invalid_correlation_id`.
//	@Description
//	@Description	**Events**
//	@Description
//	@Description	Signals sent to event signal types record that a process waypoint has been reached for another signal (e.g. an approved export health certificate is available for a consignment).
//	@Description	- events must have a `correlation_id` - correlate the event to the signal it is about (e.g. the consignment). Requests with events that don't have one are rejected (400)
//	@Description	- the `content` object must include an `occurred_at` field (directly in `content`, not nested in another object) containing an RFC 3339 timestamp with a time zone offset (e.g. 2026-09-27T14:02:00Z) - the time the waypoint was reached
//	@Description	- events are immutable: resubmitting an event with the same local_ref, content and correlation_id returns the existing version with `unchanged: true` (so events can be safely resent),
//	@Description	and resubmitting it with anything different, or after it was withdrawn, fails with `resource_already_exists`.
//	@Description	To correct an event, withdraw it and send a new event with a new local_ref.
//	@Description	- where an event is about a specific version of a signal (e.g. version 3 of a document), name it in a `subject` field in `content`: `{"signal_id": "...", "version": 3}`
//	@Description
//	@Description	Example event: `{"local_ref": "ehc-approved-001", "correlation_id": "<consignment signal_id>", "content": {"occurred_at": "2026-09-27T14:02:00Z", "subject": {"signal_id": "<document signal_id>", "version": 3}, "certificate_no": "EHC-001"}}`
//	@Description
//
//	@Param		isn_slug			path		string								true	"ISN slug"			example(sample-isn)
//	@Param		signal_type_slug	path		string								true	"signal type slug"	example(sample-signal-type)
//	@Param		sem_ver				path		string								true	"version"			example(1.0.0)
//	@Param		request				body		handlers.CreateSignalsRequest		true	"create signals"
//
//	@Success	200					{object}	handlers.SignalSubmissionResponse	"All signals processed successfully"
//	@Success	207					{object}	handlers.SignalSubmissionResponse	"Partial success - some signals succeeded, some failed"
//	@Success	422					{object}	handlers.SignalSubmissionResponse	"Valid request format but all signals failed processing - returns detailed error information"
//	@Failure	400					{object}	responses.ErrorResponse				"malformed_body"
//	@Failure	401					{object}	responses.ErrorResponse				"authentication_error"
//	@Failure	403					{object}	responses.ErrorResponse				"forbidden"
//	@Failure	404					{object}	responses.ErrorResponse				"resource_not_found"
//	@Failure	413					{object}	responses.ErrorResponse				"request_too_large"
//	@Failure	500					{object}	responses.ErrorResponse				"database_error | internal_error"
//
//	@Security	BearerAccessToken
//
//	@Router		/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals [post]
//
// the CreateSignal Handler inserts signals and signal_versions records - signals are the master records containing
// the local_ref and correlation_id, signal_versions contains the content and links back to the signal.
// the handler will record errors encountered when processing individual signals (see the signal_processing_failures table).
//
// this function should be called after the RequireAccessPermission middleware has checked the account has write permission for the ISN
// (the middleware also checks the isn and signal type are in use)
func (s *SignalsHandler) CreateSignals(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	isnSlug := r.PathValue("isn_slug")

	submission, err := s.readSignalsRequest(r)
	if err != nil {
		return err
	}

	if err := s.startBatch(ctx, submission); err != nil {
		return err
	}

	result := IsnResult{
		IsnSlug:        isnSlug,
		SignalTypePath: submission.signalTypePath(),
		StoredSignals:  make([]StoredSignal, 0),
		FailedSignals:  make([]FailedSignal, 0),
	}

	for _, signal := range submission.signals {
		var stored StoredSignal
		var failure *FailedSignal
		switch submission.contentKind {
		case signalsd.ContentKindJSON:
			stored, failure = s.storeJSONSignal(ctx, submission, signal, isnSlug)
		case signalsd.ContentKindEvent:
			stored, failure = s.storeEventSignal(ctx, submission, signal, isnSlug)
		default:
			return apperrors.InternalError(fmt.Sprintf("unexpected content kind %q", submission.contentKind), nil)
		}
		if failure != nil {
			result.FailedSignals = append(result.FailedSignals, *failure)
			continue
		}
		result.StoredSignals = append(result.StoredSignals, stored)
	}

	logSubmissionFailures(ctx, len(result.StoredSignals), result.FailedSignals)
	recordSignalProcessingFailures(ctx, s.queries, submission.batch.ID, &isnSlug, submission.signalTypeSlug, submission.semVer, result.FailedSignals)

	summary := CreateSignalsSummary{
		TotalSubmitted: len(submission.signals),
		StoredCount:    len(result.StoredSignals),
		RejectedCount:  len(result.FailedSignals),
	}
	return responses.JSON(w, submissionStatus(summary), SignalSubmissionResponse{
		BatchRef:  submission.batch.BatchRef,
		AccountID: submission.accountID,
		Results:   []IsnResult{result},
		Summary:   summary,
	})
}

// RouteSignals godoc
//
//	@Summary		Submit Signals via Router
//	@Tags			Signal Exchange
//
//	@Description	Submit JSON or event signals without specifying a target ISN - the router resolves the ISN for each signal
//	@Description	(documents are sent to document signal types with _Upload a Document via Router_).
//	@Description	Events always have a correlation ID, so they are always routed to the ISN of the correlated signal.
//	@Description
//	@Description	**ISN resolution by correlation ID**
//	@Description
//	@Description	Where a _correlation ID_ is supplied, the routing rules do not apply and the signal
//	@Description	is routed to the ISN that received the correlated signal.
//	@Description
//	@Description	**ISN resolution by pattern match**
//	@Description
//	@Description	If no correlation ID is supplied, the ISN is resolved using the routing rules defined for the _Signal Type_.
//	@Description	The rules are applied in the order defined in the _Routing Rules Config_ (first match is accepted).
//	@Description
//	@Description	**Resolution failures**
//	@Description
//	@Description	Signals that can't be routed are listed in `unroutable_signals` (and counted in `unroutable_count`):
//	@Description	- signals that do not contain the routing field defined in the _Routing Rules Config_, or do not satisfy any of the routing rules
//	@Description	- signals whose correlation_id is not found (or whose ISN is not in use)
//	@Description
//	@Description	Signals that resolve to an ISN where the account lacks write permission, or where the signal type is not in use,
//	@Description	are rejected and listed in the `failed_signals` of that ISN's result.
//	@Description
//	@Description	**Usage**
//	@Description
//	@Description	Other than the ISN resolution, this endpoint behaves the same way as the standard _Submit Signals_ endpoint
//	@Description	(request format, batches, error handling, schema validation, versions and correlation).
//	@Description	The response contains a result for each ISN the signals were routed to.
//
//	@Param			signal_type_slug	path		string								true	"signal type slug"	example(sample-signal-type)
//	@Param			sem_ver				path		string								true	"version"			example(1.0.0)
//	@Param			request				body		handlers.CreateSignalsRequest		true	"signals to submit"
//
//	@Success		200					{object}	handlers.SignalSubmissionResponse	"All signals processed successfully"
//	@Success		207					{object}	handlers.SignalSubmissionResponse	"Partial success - some signals succeeded, some failed"
//	@Success		422					{object}	handlers.SignalSubmissionResponse	"Valid request format but all signals failed processing - returns detailed error information"
//	@Failure		400					{object}	responses.ErrorResponse				"malformed_body"
//	@Failure		401					{object}	responses.ErrorResponse				"authentication_error"
//	@Failure		404					{object}	responses.ErrorResponse				"resource_not_found"
//	@Failure		413					{object}	responses.ErrorResponse				"request_too_large"
//	@Failure		500					{object}	responses.ErrorResponse				"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/router/signal-types/{signal_type_slug}/v{sem_ver}/signals [post]
//
// This handler should be used with RequireValidAccessToken middleware.
// It can't be used with RequireAccessPermission since the ISNs are not in the URL - the handler checks the write permission
// against the claims once the ISN for each signal has been resolved.
func (s *SignalsHandler) RouteSignals(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	claims, ok := auth.ContextClaims(ctx)
	if !ok {
		return apperrors.InternalError("could not get claims from context", nil)
	}

	submission, err := s.readSignalsRequest(r)
	if err != nil {
		return err
	}

	if err := s.startBatch(ctx, submission); err != nil {
		return err
	}

	signalTypePath := submission.signalTypePath()

	// the results for each ISN the signals are routed to (in the order the ISNs were first resolved)
	var isnSlugs []string
	isnResults := make(map[string]*IsnResult)
	unroutable := make([]FailedSignal, 0)
	totalStored, totalRejected := 0, 0

	for _, signal := range submission.signals {
		isnSlug, failure, err := s.resolveIsn(ctx, signalTypePath, signal)
		if err != nil {
			return err
		}
		if failure != nil {
			unroutable = append(unroutable, *failure)
			continue
		}

		result, exists := isnResults[isnSlug]
		if !exists {
			result = &IsnResult{
				IsnSlug:        isnSlug,
				SignalTypePath: signalTypePath,
				StoredSignals:  make([]StoredSignal, 0),
				FailedSignals:  make([]FailedSignal, 0),
			}
			isnResults[isnSlug] = result
			isnSlugs = append(isnSlugs, isnSlug)
		}

		// check the account can write this signal type to the ISN (we can't use the RequireAccessPermission middleware here since
		// the ISN is only known after each signal has been routed)
		if err := auth.CheckIsnWritePermission(claims, isnSlug, signalTypePath); err != nil {
			failure := FailedSignal{LocalRef: signal.LocalRef, ErrorCode: apperrors.ErrCodeInternalError.String(), ErrorMessage: err.Error()}
			if permissionErr, ok := errors.AsType[*apperrors.HTTPError](err); ok {
				failure.ErrorCode = permissionErr.Code.String()
				failure.ErrorMessage = permissionErr.Message
			}
			if signal.CorrelationID != nil {
				failure.ErrorMessage = fmt.Sprintf("%s (ISN %s was resolved via correlation_id %s)", failure.ErrorMessage, isnSlug, signal.CorrelationID)
			}
			result.FailedSignals = append(result.FailedSignals, failure)
			totalRejected++
			continue
		}

		var stored StoredSignal
		switch submission.contentKind {
		case signalsd.ContentKindJSON:
			stored, failure = s.storeJSONSignal(ctx, submission, signal, isnSlug)
		case signalsd.ContentKindEvent:
			stored, failure = s.storeEventSignal(ctx, submission, signal, isnSlug)
		default:
			return apperrors.InternalError(fmt.Sprintf("unexpected content kind %q", submission.contentKind), nil)
		}
		if failure != nil {
			result.FailedSignals = append(result.FailedSignals, *failure)
			totalRejected++
			continue
		}
		result.StoredSignals = append(result.StoredSignals, stored)
		totalStored++
	}

	// record the failures (signals that could not be routed have no ISN)
	recordSignalProcessingFailures(ctx, s.queries, submission.batch.ID, nil, submission.signalTypeSlug, submission.semVer, unroutable)

	results := make([]IsnResult, 0, len(isnSlugs))
	allFailures := unroutable
	for _, isnSlug := range isnSlugs {
		result := isnResults[isnSlug]
		recordSignalProcessingFailures(ctx, s.queries, submission.batch.ID, &result.IsnSlug, submission.signalTypeSlug, submission.semVer, result.FailedSignals)
		results = append(results, *result)
		allFailures = append(allFailures, result.FailedSignals...)
	}
	logSubmissionFailures(ctx, totalStored, allFailures)

	summary := CreateSignalsSummary{
		TotalSubmitted:  len(submission.signals),
		StoredCount:     totalStored,
		RejectedCount:   totalRejected,
		UnroutableCount: len(unroutable),
	}
	return responses.JSON(w, submissionStatus(summary), SignalSubmissionResponse{
		BatchRef:          submission.batch.BatchRef,
		AccountID:         submission.accountID,
		Results:           results,
		UnroutableSignals: unroutable,
		Summary:           summary,
	})
}

// signalsSubmission is a signals request that has been read and checked
type signalsSubmission struct {
	accountID      uuid.UUID
	signalTypeSlug string
	semVer         string
	contentKind    string // json or event
	batchRef       string
	signals        []SubmittedSignal

	// batch is set by startBatch
	batch database.UpsertSignalBatchRow
}

func (s *signalsSubmission) signalTypePath() string {
	return fmt.Sprintf("%v/v%v", s.signalTypeSlug, s.semVer)
}

// readSignalsRequest checks the signal type is a json or event type, then decodes the request body and checks the mandatory fields.
// A request that fails these checks is rejected as a whole (and is not recorded against a batch).
func (s *SignalsHandler) readSignalsRequest(r *http.Request) (*signalsSubmission, error) {
	submission := &signalsSubmission{
		signalTypeSlug: r.PathValue("signal_type_slug"),
		semVer:         r.PathValue("sem_ver"),
	}

	// only json and event signal types can be submitted (documents are uploaded - see documents.go)
	switch contentKind := s.schemaCache.ContentKind(submission.signalTypePath()); contentKind {
	case signalsd.ContentKindJSON, signalsd.ContentKindEvent:
		submission.contentKind = contentKind
	case "":
		return nil, apperrors.InvalidURLParam(fmt.Sprintf("signal type %s not found", submission.signalTypePath()), nil)
	default:
		return nil, apperrors.InvalidURLParam(fmt.Sprintf("signal type %s is a %s signal type - this endpoint only accepts json and event signals", submission.signalTypePath(), contentKind), nil)
	}

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		return nil, apperrors.InternalError("could not get accountID from context", nil)
	}
	submission.accountID = accountID

	defer r.Body.Close()

	var req CreateSignalsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		return nil, err
	}

	if req.BatchRef == "" {
		return nil, apperrors.MalformedBody("batch_ref is required", nil)
	}
	if !batchRefRegexp.MatchString(req.BatchRef) {
		return nil, apperrors.MalformedBody("batch_ref must be less than 128 characters and can only contain alphanumeric characters, hyphens, and underscores", nil)
	}

	if req.Signals == nil {
		return nil, apperrors.MalformedBody("request must contain a 'signals' array", nil)
	}
	if len(req.Signals) == 0 {
		return nil, apperrors.MalformedBody("request must contain must contain at least one signal in the 'signals' array", nil)
	}

	for i, signal := range req.Signals {
		if signal.LocalRef == "" {
			return nil, apperrors.MalformedBody(fmt.Sprintf("signal[%d] is missing required field 'local_ref'", i), nil)
		}
		if len(signal.Content) == 0 {
			return nil, apperrors.MalformedBody(fmt.Sprintf("signal[%d] (local_ref=%q) is missing required field 'content'", i, signal.LocalRef), nil)
		}
		// an event is always about the signal it is correlated to (this also means the router always resolves the ISN of an event by its correlation_id)
		if submission.contentKind == signalsd.ContentKindEvent && signal.CorrelationID == nil {
			return nil, apperrors.MalformedBody(fmt.Sprintf("signal[%d] (local_ref=%q) is missing field 'correlation_id' (required for event signal types)", i, signal.LocalRef), nil)
		}
	}

	submission.batchRef = req.BatchRef
	submission.signals = req.Signals
	return submission, nil
}

// startBatch starts the batch (a new batch is started if the batch ref has not been received previously).
// This is done outside the signal transactions, so the batch can record the signals that fail to load.
func (s *SignalsHandler) startBatch(ctx context.Context, submission *signalsSubmission) error {
	batch, err := s.queries.UpsertSignalBatch(ctx, database.UpsertSignalBatchParams{
		BatchRef:  submission.batchRef,
		AccountID: submission.accountID,
	})
	if err != nil {
		return apperrors.DatabaseError("database error", err)
	}
	submission.batch = batch

	logger.ContextWithLogAttrs(ctx,
		slog.String("batch_ref", batch.BatchRef),
		slog.String("batch_id", batch.ID.String()),
	)
	return nil
}

// resolveIsn returns the ISN a routed signal is sent to: the ISN of the correlated signal if a correlation_id is supplied,
// otherwise the ISN of the first routing rule that matches the signal content.
// A signal that can't be routed is returned as a failure.
func (s *SignalsHandler) resolveIsn(ctx context.Context, signalTypePath string, signal SubmittedSignal) (string, *FailedSignal, error) {
	if signal.CorrelationID != nil {
		isn, err := s.queries.GetIsnBySignalID(ctx, *signal.CorrelationID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", &FailedSignal{
					LocalRef:     signal.LocalRef,
					ErrorCode:    apperrors.ErrCodeInvalidCorrelationID.String(),
					ErrorMessage: fmt.Sprintf("correlation_id %s not found or its ISN is not in use", signal.CorrelationID),
				}, nil
			}
			return "", nil, apperrors.DatabaseError("database error", err)
		}
		return isn.Slug, nil, nil
	}

	_, isnSlug, ok := s.signalRouterCache.Resolve(signalTypePath, signal.Content)
	if !ok {
		return "", &FailedSignal{
			LocalRef:     signal.LocalRef,
			ErrorCode:    apperrors.ErrCodeInvalidRequest.String(),
			ErrorMessage: fmt.Sprintf("no routing rule matched for signal type %s", signalTypePath),
		}, nil
	}
	return isnSlug, nil, nil
}

// storeJSONSignal validates a json signal against the signal type's schema and stores it on the ISN
// (as a new signal, or a new version of the account's existing signal with the same local_ref).
// It returns the stored signal, or the reason the signal could not be stored.
//
// Each signal is stored in its own transaction, so one failure doesn't affect the other signals in the request.
// The account's write permission on the ISN must have been checked.
func (s *SignalsHandler) storeJSONSignal(ctx context.Context, submission *signalsSubmission, signal SubmittedSignal, isnSlug string) (StoredSignal, *FailedSignal) {
	failed := func(code apperrors.ErrorCode, message string) (StoredSignal, *FailedSignal) {
		return StoredSignal{}, &FailedSignal{LocalRef: signal.LocalRef, ErrorCode: code.String(), ErrorMessage: message}
	}

	if err := s.schemaCache.ValidateJSONSignal(ctx, s.queries, submission.signalTypePath(), signal.Content); err != nil {
		return failed(apperrors.ErrCodeMalformedBody, fmt.Sprintf("validation failed: %v", err))
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return failed(apperrors.ErrCodeDatabaseError, "failed to begin transaction")
	}
	defer tx.Rollback(ctx) // no-op once the transaction is committed

	queries := s.queries.WithTx(tx)

	// create or update the signal master record
	var signalID uuid.UUID
	if signal.CorrelationID == nil {
		signalID, err = queries.CreateSignal(ctx, database.CreateSignalParams{
			AccountID:      submission.accountID,
			LocalRef:       signal.LocalRef,
			IsnSlug:        isnSlug,
			SignalTypeSlug: submission.signalTypeSlug,
			SemVer:         submission.semVer,
		})
	} else {
		// the correlated signal must be in the same ISN
		isValid, validateErr := queries.ValidateCorrelationID(ctx, database.ValidateCorrelationIDParams{
			CorrelationID: *signal.CorrelationID,
			IsnSlug:       isnSlug,
		})
		if validateErr != nil {
			return failed(apperrors.ErrCodeDatabaseError, "failed to validate correlation_id")
		}
		if !isValid {
			return failed(apperrors.ErrCodeInvalidCorrelationID, fmt.Sprintf("invalid correlation_id %v - signal does not exist in this ISN", *signal.CorrelationID))
		}

		signalID, err = queries.CreateOrUpdateSignalWithCorrelationID(ctx, database.CreateOrUpdateSignalWithCorrelationIDParams{
			AccountID:      submission.accountID,
			LocalRef:       signal.LocalRef,
			CorrelationID:  *signal.CorrelationID,
			IsnSlug:        isnSlug,
			SignalTypeSlug: submission.signalTypeSlug,
			SemVer:         submission.semVer,
		})
	}
	if err != nil {
		// the insert checks the ISN and signal type are in use - if not it returns no rows
		// (possible if they were disabled after the access token was issued)
		if errors.Is(err, pgx.ErrNoRows) {
			return failed(apperrors.ErrCodeResourceNotFound, "the ISN or signal type is not in use")
		}
		return failed(apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to create signal master record: %v", err))
	}

	// create the signal version, which holds the content
	version, err := queries.CreateSignalVersion(ctx, database.CreateSignalVersionParams{
		AccountID:      submission.accountID,
		SignalBatchID:  submission.batch.ID,
		Content:        signal.Content,
		LocalRef:       signal.LocalRef,
		SignalTypeSlug: submission.signalTypeSlug,
		SemVer:         submission.semVer,
	})
	if err != nil {
		return failed(apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to create signal version: %v", err))
	}

	if err := tx.Commit(ctx); err != nil {
		return failed(apperrors.ErrCodeDatabaseError, "failed to commit transaction")
	}

	return StoredSignal{
		LocalRef:        signal.LocalRef,
		SignalID:        signalID,
		SignalVersionID: version.ID,
		VersionNumber:   version.VersionNumber,
	}, nil
}

// submissionStatus returns the response status for a submission:
// 200 if all the signals were stored, 207 if some were, and 422 if none were
func submissionStatus(summary CreateSignalsSummary) int {
	switch {
	case summary.StoredCount == 0:
		return http.StatusUnprocessableEntity
	case summary.StoredCount < summary.TotalSubmitted:
		return http.StatusMultiStatus
	default:
		return http.StatusOK
	}
}

// logSubmissionFailures logs a summary of the signals that could not be stored (the number of each kind of failure)
func logSubmissionFailures(ctx context.Context, storedCount int, failures []FailedSignal) {
	if len(failures) == 0 {
		return
	}

	failureCounts := make(map[string]int)
	for _, failed := range failures {
		failureCounts[failed.ErrorMessage]++
	}

	requestLogger := logger.ContextRequestLogger(ctx)
	requestLogger.Warn("Signal processing failures",
		slog.Int("stored_count", storedCount),
		slog.Int("rejected_count", len(failures)),
		slog.Int("unique_errors", len(failureCounts)),
	)
	for errorMessage, count := range failureCounts {
		requestLogger.Warn("Failure type",
			slog.String("error_message", errorMessage),
			slog.Int("count", count),
		)
	}
}
