package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/publicisns"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/router"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SignalsHandler handles the signal endpoints:
//   - submission of JSON signals (signal_submission.go) - documents are uploaded by DocumentsHandler (documents.go)
//   - search (signal_search.go) and withdrawal (below), for signals of every content kind
type SignalsHandler struct {
	queries        *database.Queries
	pool           *pgxpool.Pool
	schemaCache    *schemas.Cache
	publicIsnCache *publicisns.Cache

	// signalRouterCache contains the routing rules for each signal type that has been set up for routing (used by RouteSignals)
	signalRouterCache *router.Cache
}

func NewSignalsHandler(queries *database.Queries, pool *pgxpool.Pool, schemaCache *schemas.Cache, publicIsnCache *publicisns.Cache, signalRouterCache *router.Cache) *SignalsHandler {
	return &SignalsHandler{
		queries:           queries,
		pool:              pool,
		schemaCache:       schemaCache,
		publicIsnCache:    publicIsnCache,
		signalRouterCache: signalRouterCache,
	}
}

// WithdrawSignalsRequest contains the local ref for the signal being withdrawn
type WithdrawSignalRequest struct {
	LocalRef *string `json:"local_ref,omitempty" example:"item_id_#1"`
}

// WithdrawSignal godoc
//
//	@Summary		Withdraw a Signal
//	@Description	Withdraw a signal by local reference
//	@Description
//	@Description	Withdrawn signals are hidden from search results by default but remain in the database.
//	@Description	Signals can only be withdrawn by the account that created the signal.
//	@Description	To reactivate a signal resupply it with the same local_ref using the 'create signals' end point.
//
//	@Tags			Signal Exchange
//
//	@Param			isn_slug			path	string							true	"ISN slug"			example(sample-isn)
//	@Param			signal_type_slug	path	string							true	"Signal type slug"	example(signal-type-1)
//	@Param			sem_ver				path	string							true	"version"			example(1.0.0)
//	@Param			request				body	handlers.WithdrawSignalRequest	true	"Withdrawal request"
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse	"malformed_body"
//	@Failure		401	{object}	responses.ErrorResponse	"authentication_error"
//	@Failure		403	{object}	responses.ErrorResponse	"forbidden"
//	@Failure		404	{object}	responses.ErrorResponse	"resource_not_found"
//	@Failure		409	{object}	responses.ErrorResponse	"resource_already_exists"
//	@Failure		500	{object}	responses.ErrorResponse	"database_error"
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/withdraw [put]
func (s *SignalsHandler) WithdrawSignal(w http.ResponseWriter, r *http.Request) error {

	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		return apperrors.InternalError("could not get accountID from context", nil)
	}

	// Parse request body
	var req WithdrawSignalRequest
	defer r.Body.Close()
	if err := decodeJSONBody(r, &req); err != nil {
		return err
	}

	if req.LocalRef == nil {
		return apperrors.MalformedBody("you must supply a local_ref", nil)
	}

	// Get the signal
	signal, err := s.queries.GetSignalByAccountAndLocalRef(r.Context(), database.GetSignalByAccountAndLocalRefParams{
		AccountID: accountID,
		Slug:      signalTypeSlug,
		SemVer:    semVer,
		LocalRef:  *req.LocalRef,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound("signal not found", nil)
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("local_ref", *req.LocalRef),
		)

		return apperrors.DatabaseError("database error", err)
	}

	// Check if signal is already withdrawn
	if signal.IsWithdrawn {
		return apperrors.AlreadyExists("signal is already withdrawn", nil)
	}

	// Withdraw the signal - query enforces is_in_use at ISN and signal type level as defence against stale claims
	rowsAffected, err := s.queries.WithdrawSignalByLocalRef(r.Context(), database.WithdrawSignalByLocalRefParams{
		AccountID: accountID,
		Slug:      signalTypeSlug,
		SemVer:    semVer,
		IsnSlug:   isnSlug,
		LocalRef:  *req.LocalRef,
	})

	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("local_ref", *req.LocalRef),
		)

		return apperrors.DatabaseError("database error", err)
	}

	if rowsAffected == 0 {
		return apperrors.NotFound("signal not found or ISN/signal type no longer active", nil)
	}

	logger.ContextWithLogAttrs(r.Context(),
		slog.String("signal_id", signal.ID.String()),
		slog.String("local_ref", signal.LocalRef),
		slog.String("withdrawn_by", accountID.String()),
	)

	return responses.NoContent(w, http.StatusNoContent)
}
