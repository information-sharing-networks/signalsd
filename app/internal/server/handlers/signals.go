package handlers

import (
	"net/http"

	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/server/responses"
)

type SignalsHandler struct {
	queries *database.Queries
}

func NewSignalsHandler(queries *database.Queries) *SignalsHandler {
	return &SignalsHandler{queries: queries}
}

// SignalsHandler godocs
//
//	@Summary		send one or more signals
//	@Tags			Signals Management
//
//	@Description	- the client can submit arrays of signals for a single type in a single req (subject to the size
//	@Description	limits defined on the signal type definition)
//	@Description	- The client-supplied local_ref must uniquely identify each signal of the
//	@Description	specified signal type that will be supplied by the account.
//	@Description	- If a local reference is received more than once from an account for a
//	@Description	specified signal_type it will be stored with a incremented version number
//	@Description	and the previous version will be marked as latest=false (it does not matter
//	@Description	if the previous signal was received in a different batch, but the signal must
//	@Description	be owned by the same account).
//	@Description	- If a deletion request is received, all version of the signal will be marked
//	@Description	as 'withdrawn'.
//	@Description	- Correlation_ids are auto generated on the server unless supplied by the client, in which case they are used to identify another signal that this signal is related to. The correlated signal does not need to be owned by the same account.
//
//	@Router			/isn/{isn_slug}/signal_types/{signal_type_slug}/signals [post]
func (s *SignalsHandler) SignalsHandlers(w http.ResponseWriter, r *http.Request) {

	//isnSlug := r.PathValue("slug_isn")
	//signalTypeSlug := r.PathValue("signal_type_slug")

	// the claims
	// middleware to check  the type of batch - is

	//auth - isn write
	// read the isn receiver on startup - go jobs to reject requests that are too big - not logs

	// is batch open?  Reject if not
	//
	// split json into array
	// for array insert
	responses.RespondWithError(w, r, http.StatusNoContent, apperrors.ErrCodeNotImplemented, "todo - signals not yet implemented")
}
