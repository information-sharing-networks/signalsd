package handlers

// event signals: json signals that record a process waypoint for the signal they are correlated to (e.g. "export health certificate approved").
//
// Events are submitted with the json signal endpoints (CreateSignals and RouteSignals) and are validated against the signal type schema.
// storeEventSignal adds the checks that make events immutable:
// - the correlation_id is required (checked by readSignalsRequest)
// - the content must include an occurred_at timestamp (RFC 3339, with a time zone offset) - a field of the content object itself, not nested in another object
// - a resubmitted event is never stored as a new version: an identical resubmission returns the existing version (unchanged),
//   and a changed resubmission (or a resubmission of a withdrawn event) is rejected. Events are corrected by withdrawing them and sending a new event.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/jackc/pgx/v5"
)

// storeEventSignal validates an event and stores it on the ISN as a new signal.
// An event that has already been stored with the same content and correlation_id is returned as unchanged.
//
// As with storeJSONSignal, each event is stored in its own transaction and the account's write permission on the ISN must have been checked.
func (s *SignalsHandler) storeEventSignal(ctx context.Context, submission *signalsSubmission, signal SubmittedSignal, isnSlug string) (StoredSignal, *FailedSignal) {
	failed := func(code apperrors.ErrorCode, message string) (StoredSignal, *FailedSignal) {
		return StoredSignal{}, &FailedSignal{LocalRef: signal.LocalRef, ErrorCode: code.String(), ErrorMessage: message}
	}

	// readSignalsRequest rejects events without a correlation_id
	if signal.CorrelationID == nil {
		return failed(apperrors.ErrCodeMalformedBody, "correlation_id is required for event signal types")
	}
	correlationID := *signal.CorrelationID

	if err := checkOccurredAt(signal.Content); err != nil {
		return failed(apperrors.ErrCodeMalformedBody, err.Error())
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

	// the correlated signal must be in the same ISN
	isValid, err := queries.ValidateCorrelationID(ctx, database.ValidateCorrelationIDParams{
		CorrelationID: correlationID,
		IsnSlug:       isnSlug,
	})
	if err != nil {
		return failed(apperrors.ErrCodeDatabaseError, "failed to validate correlation_id")
	}
	if !isValid {
		return failed(apperrors.ErrCodeInvalidCorrelationID, fmt.Sprintf("invalid correlation_id %v - signal does not exist in this ISN", correlationID))
	}

	// create the signal master record - no rows are returned if the account has already sent an event with this local_ref
	signalID, err := queries.CreateEventSignal(ctx, database.CreateEventSignalParams{
		AccountID:      submission.accountID,
		LocalRef:       signal.LocalRef,
		CorrelationID:  correlationID,
		IsnSlug:        isnSlug,
		SignalTypeSlug: submission.signalTypeSlug,
		SemVer:         submission.semVer,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return s.resubmittedEvent(ctx, queries, submission, signal, isnSlug)
	}
	if err != nil {
		return failed(apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to create signal master record: %v", err))
	}

	version, err := queries.CreateSignalVersion(ctx, database.CreateSignalVersionParams{
		AccountID:     submission.accountID,
		SignalBatchID: submission.batch.ID,
		SignalID:      signalID,
		Content:       signal.Content,
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

// resubmittedEvent handles an event whose master record could not be created: either the account has already sent an event
// with this local_ref to the ISN, or the ISN or signal type is not in use.
// An identical resubmission (same content and correlation_id, and not withdrawn) returns the existing version as unchanged - so events can be safely resent.
// Any other resubmission is rejected, since events are immutable.
func (s *SignalsHandler) resubmittedEvent(ctx context.Context, queries *database.Queries, submission *signalsSubmission, signal SubmittedSignal, isnSlug string) (StoredSignal, *FailedSignal) {
	failed := func(code apperrors.ErrorCode, message string) (StoredSignal, *FailedSignal) {
		return StoredSignal{}, &FailedSignal{LocalRef: signal.LocalRef, ErrorCode: code.String(), ErrorMessage: message}
	}

	existing, err := queries.CompareWithLatestSignalVersion(ctx, database.CompareWithLatestSignalVersionParams{
		Content:        signal.Content,
		AccountID:      submission.accountID,
		IsnSlug:        isnSlug,
		SignalTypeSlug: submission.signalTypeSlug,
		SemVer:         submission.semVer,
		LocalRef:       signal.LocalRef,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// there is no existing event, so the insert was rejected because the ISN or signal type is not in use
		// (possible if they were disabled after the access token was issued)
		return failed(apperrors.ErrCodeResourceNotFound, "the ISN or signal type is not in use")
	}
	if err != nil {
		return failed(apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to get the existing event: %v", err))
	}

	switch {
	case existing.IsWithdrawn:
		return failed(apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("event %q has been withdrawn - events are immutable, so send a new event with a new local_ref", signal.LocalRef))
	case !existing.ContentMatches || existing.CorrelationID != *signal.CorrelationID:
		return failed(apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("event %q already exists with different content or correlation_id - events are immutable: withdraw the event and send a new one with a new local_ref", signal.LocalRef))
	}

	return StoredSignal{
		LocalRef:        signal.LocalRef,
		SignalID:        existing.SignalID,
		SignalVersionID: existing.SignalVersionID,
		VersionNumber:   existing.VersionNumber,
		Unchanged:       true,
	}, nil
}

// checkOccurredAt checks the event content is a json object with an occurred_at field (a field of the content object itself, not nested in another object)
// containing an RFC 3339 timestamp with a time zone offset (e.g. 2026-09-27T14:02:00Z)
func checkOccurredAt(content json.RawMessage) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return fmt.Errorf("event content must be a json object")
	}

	rawOccurredAt, ok := fields["occurred_at"]
	if !ok {
		return fmt.Errorf("event content is missing the required field 'occurred_at'")
	}

	var occurredAt string
	if err := json.Unmarshal(rawOccurredAt, &occurredAt); err != nil {
		return fmt.Errorf("occurred_at must be a string containing an RFC 3339 timestamp (e.g. 2026-09-27T14:02:00Z)")
	}
	if _, err := time.Parse(time.RFC3339, occurredAt); err != nil {
		return fmt.Errorf("occurred_at %q is not an RFC 3339 timestamp with a time zone offset (e.g. 2026-09-27T14:02:00Z)", occurredAt)
	}
	return nil
}
