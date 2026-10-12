package schemas

import (
	"context"
	"fmt"
	"uuid"

	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// recentSignalsSampleSize is the number of the most recent signal versions checked against an updated schema
const recentSignalsSampleSize = 1000

// maxReportedFailures is the number of failed signal versions included in the check result (the rest are only counted)
const maxReportedFailures = 5

// RecentSignalsCheck is the result of checking the recent signals of a signal type against a schema
type RecentSignalsCheck struct {
	// SignalsChecked is the number of signal versions checked
	SignalsChecked int

	// FailureCount is the number of checked signal versions that are not valid against the schema
	FailureCount int

	// Failures lists the first signal versions that are not valid against the schema (up to maxReportedFailures)
	Failures []StoredSignalFailure
}

// StoredSignalFailure is a stored signal version that is not valid against the schema
type StoredSignalFailure struct {
	SignalID      uuid.UUID
	LocalRef      string
	VersionNumber int32
	Error         string
}

// CheckRecentSignals validates the most recent versions of the signal type's signals against the schema
//
// Used as a safety check before updating the schema of a signal type: it catches an updated schema that rejects
// the data currently being received.
func CheckRecentSignals(ctx context.Context, queries *database.Queries, signalTypeID uuid.UUID, schema *jsonschema.Schema) (RecentSignalsCheck, error) {
	var check RecentSignalsCheck

	versions, err := queries.GetRecentSignalVersionsBySignalTypeID(ctx, database.GetRecentSignalVersionsBySignalTypeIDParams{
		SignalTypeID: signalTypeID,
		SampleSize:   recentSignalsSampleSize,
	})
	if err != nil {
		return check, fmt.Errorf("failed to get the recent signal versions: %w", err)
	}

	for _, version := range versions {
		check.SignalsChecked++

		if err := ValidateContent(schema, version.Content); err != nil {
			check.FailureCount++
			if len(check.Failures) < maxReportedFailures {
				check.Failures = append(check.Failures, StoredSignalFailure{
					SignalID:      version.SignalID,
					LocalRef:      version.LocalRef,
					VersionNumber: version.VersionNumber,
					Error:         err.Error(),
				})
			}
		}
	}

	return check, nil
}
