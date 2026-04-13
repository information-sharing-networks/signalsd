package handlers

// this handler is used when submitting signals using the signals router.
// the router allows signals of the same type to be sent to a single endpoint and
// then distributed to different ISNs based on their content.
//
// the response and request structs are shared with signals.go (which handles standard signal submission)

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/router"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SignalRouter holds the dependencies used by the handler
type SignalRouter struct {

	// queries contains the sql queries needed by the handler
	queries *database.Queries

	// pool postgress connection
	pool *pgxpool.Pool

	// schemaCache contains the pre-compiled json schemas used to check the signal data is correct
	schemaCache *schemas.Cache

	// signalRouterCache contains the rule configs for each signal type path that has been set up for routing
	signalRouterCache *router.Cache
}

func NewSignalRouter(queries *database.Queries, pool *pgxpool.Pool, schemaCache *schemas.Cache, signalRouterCache *router.Cache) *SignalRouter {
	return &SignalRouter{
		queries:           queries,
		pool:              pool,
		schemaCache:       schemaCache,
		signalRouterCache: signalRouterCache,
	}
}

// resolvedSignal pairs an input signal with its resolved target ISN.
type resolvedSignal struct {
	signal  Signal
	isnSlug string
	isnID   uuid.UUID
}

// RouteSignals godoc
//
//	@Summary		Submit Signals via Router
//
//	@Tags			Signal Exchange
//
//	@Description	Submit signals without specifying a target ISN. The router resolves the target ISN
//	@Description	for each signal using configured routing rules, or via correlation ID if supplied.
//	@Description
//	@Description	**ISN resolution by correlation ID**
//	@Description
//	@Description	Where a _correlation ID_ is supplied, the routing rules do not apply and the signal
//	@Description	will be routed to the ISN that received the original correlated signal.
//	@Description
//	@Description	**ISN resolution by pattern match**
//	@Description
//	@Description	If no correlation ID is present the handler attempts to resolve the ISN using the routing rules
//	@Description	defined for the _Signal Type_.
//	@Description	The rules are applied in the order defined in the _Routing Rules Config_ (first match is accepted).
//	@Description
//	@Description	**Resolution Faiulres**
//	@Description
//	@Description	Signals subject to pattern matches are rejected when:
//	@Description	- they do not contain the routing field defined in the _Routing Rules Config_
//	@Description	- they do not satisfy any of the routing rules
//	@Description
//	@Description	Signals that resolve to an ISN where the account lacks write permission are rejected.
//	@Description
//	@Description	**Usage**
//	@Description
//	@Description	Other than the ISN resolution feature, this handler behaves the same way as the standard _Submit Signals_ endpoint:
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
//	@Description	(e.g resolution failurs, schema validation errors, incorrect correlations ids).
//	@Description	Failures are logged and trackable via the Batch Status endpoint.
//	@Description	The response provides an audit trail detailing the submission outcome.
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
//
//	@Param			signal_type_slug	path		string								true	"signal type slug"	example(sample-signal-type)
//	@Param			sem_ver				path		string								true	"version"			example(1.0.0)
//	@Param			request				body		handlers.CreateSignalsRequest		true	"signals to submit"
//
//	@Success		200					{object}	handlers.SignalSubmissionResponse	"All signals processed successfully"
//	@Success		207					{object}	handlers.SignalSubmissionResponse	"Partial success - some signals succeeded, some failed"
//	@Success		422					{object}	handlers.SignalSubmissionResponse	"Valid request format but all signals failed processing - returns detailed error information"
//	@Failure		400					{object}	responses.ErrorResponse				"Invalid request format (error_code = malformed_body)
//	@Failure		401					{object}	responses.ErrorResponse				"Unauthorized request (invalid credentials, error_code = authentication_error)"
//	@Failure		404					{object}	responses.ErrorResponse				"Not Found (mistyped url or signal_type marked 'not in use')
//
//	@Security		BearerAccessToken
//
//	@Router			/api/router/signal-types/{signal_type_slug}/v{sem_ver}/signals [post]
//
// This handler should be used with RequireValidAccessToken middleware.
// Note that it can't be used with RequireAccessPermission since the target ISNs are not known until
// the code in the hanlder runs (therefore the handler checks access permissions against the claims directly)
func (s *SignalRouter) RouteSignals(w http.ResponseWriter, r *http.Request) {
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%s/v%s", signalTypeSlug, semVer)

	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get accountID from context")
		return
	}

	defer r.Body.Close()

	var req CreateSignalsRequest

	// unmarshal the req body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	// check for mandatory fields in request
	if req.BatchRef == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "batch_ref is required")
		return
	}
	if !batchRefRegexp.MatchString(req.BatchRef) {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "batch_ref must be less than 128 characters and can only contain alphanumeric characters, hyphens, and underscores")
		return
	}

	if req.Signals == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "request must contain a 'signals' array")
		return
	}
	if len(req.Signals) == 0 {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "request must contain must contain at least one signal in the 'signals' array")
		return
	}

	for i, signal := range req.Signals {
		if signal.LocalRef == "" {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("signal[%d] is missing required field 'local_ref'", i))
			return
		}
		if len(signal.Content) == 0 {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("signal[%d] (local_ref=%q) is missing required field 'content'", i, signal.LocalRef))
			return
		}
	}

	// start the batch (a new batch is started if the batch ref has not been received previously)
	batch, err := s.queries.UpsertSignalBatch(r.Context(), database.UpsertSignalBatchParams{
		BatchRef:  req.BatchRef,
		AccountID: accountID,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	var routed []resolvedSignal
	var routeFailures []FailedSignal

	// Resolve target ISN for each signal.
	for _, signal := range req.Signals {

		// route by correlation ID
		if signal.CorrelationID != nil {
			isnRow, err := s.queries.GetIsnBySignalID(r.Context(), *signal.CorrelationID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					routeFailures = append(routeFailures, FailedSignal{
						LocalRef:     signal.LocalRef,
						ErrorCode:    string(apperrors.ErrCodeInvalidCorrelationID),
						ErrorMessage: fmt.Sprintf("correlation_id %s not found or its ISN is not in use", signal.CorrelationID),
					})
					continue
				}
				logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
				responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
				return
			}
			routed = append(routed, resolvedSignal{signal: signal, isnSlug: isnRow.Slug, isnID: isnRow.ID})
		} else {

			// route by matching rules
			isnID, isnSlug, ok := s.signalRouterCache.Resolve(signalTypePath, signal.Content)
			if !ok {
				routeFailures = append(routeFailures, FailedSignal{
					LocalRef:     signal.LocalRef,
					ErrorCode:    string(apperrors.ErrCodeInvalidRequest),
					ErrorMessage: fmt.Sprintf("no routing rule matched for signal type %s", signalTypePath),
				})
				continue
			}
			routed = append(routed, resolvedSignal{signal: signal, isnSlug: isnSlug, isnID: isnID})
		}
	}

	// For each resolved signal: check permissions, check signal type availability, validate, and insert.
	isnResults := make(map[string]*IsnResult)
	totalStored := 0
	totalRejected := 0

	for _, rs := range routed {

		if _, exists := isnResults[rs.isnSlug]; !exists {
			isnResults[rs.isnSlug] = &IsnResult{
				IsnSlug:        rs.isnSlug,
				SignalTypePath: signalTypePath,
				StoredSignals:  make([]StoredSignal, 0),
				FailedSignals:  make([]FailedSignal, 0),
			}
		}
		result := isnResults[rs.isnSlug]

		// check the account has write permission
		// Check ISN-level write permission only — signal type availability is checked separately below.
		if err := auth.CheckIsnWritePermission(claims, rs.isnSlug, ""); err != nil {
			errMsg := err.Error()
			if rs.signal.CorrelationID != nil {
				errMsg = fmt.Sprintf("%s (ISN %s was resolved via correlation_id %s)", errMsg, rs.isnSlug, rs.signal.CorrelationID)
			}
			result.FailedSignals = append(result.FailedSignals, FailedSignal{
				LocalRef:     rs.signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeForbidden),
				ErrorMessage: errMsg,
			})
			totalRejected++
			continue
		}

		perms := claims.IsnPerms[rs.isnSlug]
		st, ok := perms.SignalTypes[signalTypePath]
		if !ok {
			result.FailedSignals = append(result.FailedSignals, FailedSignal{
				LocalRef:     rs.signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeInvalidRequest),
				ErrorMessage: fmt.Sprintf("signal type %s is not registered on ISN %s", signalTypePath, rs.isnSlug),
			})
			totalRejected++
			continue
		}
		if !st.InUse {
			result.FailedSignals = append(result.FailedSignals, FailedSignal{
				LocalRef:     rs.signal.LocalRef,
				ErrorCode:    string(apperrors.ErrCodeInvalidRequest),
				ErrorMessage: fmt.Sprintf("signal type %s is not active on ISN %s", signalTypePath, rs.isnSlug),
			})
			totalRejected++
			continue
		}

		if err := s.schemaCache.ValidateSignal(r.Context(), s.queries, signalTypePath, rs.signal.Content); err != nil {
			result.FailedSignals = append(result.FailedSignals, FailedSignal{
				LocalRef: rs.signal.LocalRef, ErrorCode: string(apperrors.ErrCodeMalformedBody),
				ErrorMessage: fmt.Sprintf("validation failed: %v", err),
			})
			totalRejected++
			continue
		}

		tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
		if err != nil {
			result.FailedSignals = append(result.FailedSignals, FailedSignal{
				LocalRef: rs.signal.LocalRef, ErrorCode: string(apperrors.ErrCodeDatabaseError), ErrorMessage: "failed to begin transaction",
			})
			totalRejected++
			continue
		}

		var signalID uuid.UUID
		var signalErr error

		if rs.signal.CorrelationID != nil {
			signalID, signalErr = s.queries.WithTx(tx).CreateOrUpdateSignalWithCorrelationID(r.Context(), database.CreateOrUpdateSignalWithCorrelationIDParams{
				AccountID: claims.AccountID, LocalRef: rs.signal.LocalRef, CorrelationID: *rs.signal.CorrelationID,
				IsnSlug: rs.isnSlug, SignalTypeSlug: signalTypeSlug, SemVer: semVer,
			})
		} else {
			signalID, signalErr = s.queries.WithTx(tx).CreateSignal(r.Context(), database.CreateSignalParams{
				AccountID: claims.AccountID, LocalRef: rs.signal.LocalRef,
				IsnSlug: rs.isnSlug, SignalTypeSlug: signalTypeSlug, SemVer: semVer,
			})
		}

		if signalErr != nil {
			// rollback partial load of the signal
			if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("error", rollbackErr.Error()),
				)
			}
			errMsg := fmt.Sprintf("failed to create signal: %v", signalErr)
			if errors.Is(signalErr, pgx.ErrNoRows) {
				errMsg = "failed to create signal - signal type may no longer be active on this ISN"
			}
			result.FailedSignals = append(result.FailedSignals, FailedSignal{
				LocalRef: rs.signal.LocalRef, ErrorCode: string(apperrors.ErrCodeDatabaseError), ErrorMessage: errMsg,
			})
			totalRejected++
			continue
		}

		versionResult, versionErr := s.queries.WithTx(tx).CreateSignalVersion(r.Context(), database.CreateSignalVersionParams{
			AccountID: claims.AccountID, SignalBatchID: batch.ID, Content: rs.signal.Content,
			LocalRef: rs.signal.LocalRef, SignalTypeSlug: signalTypeSlug, SemVer: semVer,
		})
		if versionErr != nil {
			// rollback partial load of the signal
			if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("error", rollbackErr.Error()),
				)
			}
			result.FailedSignals = append(result.FailedSignals, FailedSignal{
				LocalRef: rs.signal.LocalRef, ErrorCode: string(apperrors.ErrCodeDatabaseError),
				ErrorMessage: fmt.Sprintf("failed to create signal version: %v", versionErr),
			})
			totalRejected++
			continue
		}

		if err := tx.Commit(r.Context()); err != nil {
			result.FailedSignals = append(result.FailedSignals, FailedSignal{
				LocalRef: rs.signal.LocalRef, ErrorCode: string(apperrors.ErrCodeDatabaseError), ErrorMessage: "failed to commit transaction",
			})
			totalRejected++
			continue
		}

		result.StoredSignals = append(result.StoredSignals, StoredSignal{
			LocalRef: rs.signal.LocalRef, SignalID: signalID,
			SignalVersionID: versionResult.ID, VersionNumber: versionResult.VersionNumber,
		})
		totalStored++
	}

	// Log per-signal failures to the processing failures table
	for _, failed := range routeFailures {
		_, _ = s.queries.CreateSignalProcessingFailureDetail(r.Context(), database.CreateSignalProcessingFailureDetailParams{
			SignalBatchID: batch.ID, SignalTypeSlug: signalTypeSlug, SignalTypeSemVer: semVer,
			LocalRef: failed.LocalRef, ErrorCode: failed.ErrorCode, ErrorMessage: failed.ErrorMessage,
		})
	}
	for _, isnResult := range isnResults {
		for _, failed := range isnResult.FailedSignals {
			_, _ = s.queries.CreateSignalProcessingFailureDetail(r.Context(), database.CreateSignalProcessingFailureDetailParams{
				SignalBatchID: batch.ID, SignalTypeSlug: signalTypeSlug, SignalTypeSemVer: semVer,
				LocalRef: failed.LocalRef, ErrorCode: failed.ErrorCode, ErrorMessage: failed.ErrorMessage,
			})
		}
	}

	// Assemble response
	results := make([]IsnResult, 0, len(isnResults))
	for _, isnResult := range isnResults {
		results = append(results, *isnResult)
	}

	httpStatus := http.StatusOK
	switch {
	case totalStored == 0:
		httpStatus = http.StatusUnprocessableEntity
	case totalRejected > 0 || len(routeFailures) > 0:
		httpStatus = http.StatusMultiStatus
	}

	responses.RespondWithJSON(w, httpStatus, SignalSubmissionResponse{
		BatchRef:          batch.BatchRef,
		AccountID:         accountID,
		Results:           results,
		UnroutableSignals: routeFailures,
		Summary:           CreateSignalsSummary{TotalSubmitted: len(req.Signals), StoredCount: totalStored, RejectedCount: totalRejected, UnroutableCount: len(routeFailures)},
	})
}
