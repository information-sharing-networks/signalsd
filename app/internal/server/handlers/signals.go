package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/server/responses"
)

type SignalsHandler struct {
	queries *database.Queries
	pool    *pgxpool.Pool
}

func NewSignalsHandler(queries *database.Queries, pool *pgxpool.Pool) *SignalsHandler {
	return &SignalsHandler{
		queries: queries,
		pool:    pool,
	}
}

type CreateSignalsRequest struct {
	Signals []struct {
		LocalRef      string          `json:"local_ref" example:"item_id_#1"`
		CorrelationId *uuid.UUID      `json:"correlation_id" example:"75b45fe1-ecc2-4629-946b-fd9058c3b2ca"` //optional - supply the id of another signal if you want to link to it
		Content       json.RawMessage `json:"content"`
	} `json:"signals"`
}

// workaround because Swaggo can't process json.RawMessage
type CreateSignalsRequestDoc struct {
	Signals []struct {
		LocalRef      string         `json:"local_ref" example:"item_id_#1"`
		CorrelationId *uuid.UUID     `json:"correlation_id" example:"75b45fe1-ecc2-4629-946b-fd9058c3b2ca"`
		Content       map[string]any `json:"content"`
	} `json:"signals"`
}

type CreateSignalsResponse2 struct {
	LocalRef      string     `json:"local_ref" example:"item_id_#1"`
	SignalId      *uuid.UUID `json:"signal_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`
	VersionNumber int32      `json:"version_number" example:"1"`
}
type CreateSignalsResponse struct {
	IsnSlug        string         `json:"isn_slug" example:"sample-isn--example-org"`
	SignalTypePath string         `json:"signal_type_path" example:"signal-type-1/v0.0.1"`
	AccountID      uuid.UUID      `json:"account_id" example:"a38c99ed-c75c-4a4a-a901-c9485cf93cf3"`
	StoredSignals  []StoredSignal `json:"stored_signals"`
}
type StoredSignal struct {
	LocalRef        string    `json:"local_ref" example:"item_id_#1"`
	SignalVersionID uuid.UUID `json:"signal_version_id" example:"835788bd-789d-4091-96e3-db0f51ccbabc"`
	VersionNumber   int32     `json:"version_number" example:"1"`
}

// SignalsHandler godocs
//
//	@Summary		Send signals
//	@Tags			Signal sharing
//
//	@Description	- the client can submit an array of signals to this endpoint for storage on the ISN
//	@Description	- payloads must not mix signals of different types and the payload is subject to the sizen	limits defined on the ISN.
//	@Description	- The client-supplied local_ref must uniquely identify each signal of the specified signal type that will be supplied by the account.
//	@Description	- If a local reference is received more than once from an account for a specified signal_type it will be stored with a incremented version number.
//	@Description	- Optionally a correlation_id can be supplied - this will link the signal to a previously received signal. The correlated signal does not need to be owned by the same account.
//	@Description	- requests are only accepted for the open signal batch for this account/ISN.
//	@Description
//	@Description	**Authentication**
//	@Description
//	@Description	Requires a valid access token.
//	@Description	The claims in the access token list the ISNs and signal_types that the account is permitted to use.
//	@Description
//	@Description	- the RequireIsnWritePermission middleware will consult the claims in the access token to confirm the user is allowed to write to the isn in the URL.
//	@Description	- This handler also checks that the signal_type/sem_ver in the url is also listed in the claims (this is to catch mistyped urls)
//	@Description
//	@Description	**Validation**
//	@Description
//	@Description	the content is validated against the json schema asynchronously, however basic checks are done on the incoming data.
//	@Description	The following issues create a 400 error and cause the entire payload to be rejected
//	@Description	- invalid json format
//	@Description	- missing fields (the array of signals must be in a json object called signals, and content and local_ref must be present for each record).
//	@Description	- incorrect correlation ids - where supplied, correlation ids must refer to another signal of the same type (error_code is set to "invalid_correlation_id" in this case)
//	@Description
//	@Description	internal errors cause the whole payload to be rejected.
//
//	@Param			isn_slug			path	string								true	"isn slug"						example(sample-isn--example-org)
//	@Param			signal_type_slug	path	string								true	"signal type slug"				example(sample-signal--example-org)
//	@Param			sem_ver				path	string								true	"signal type sem_ver number"	example(0.0.1)
//	@Param			request				body	handlers.CreateSignalsRequestDoc	true	"create signals"
//
//	@Success		201 {object} 	handlers.CreateSignalsResponse
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/isn/{isn_slug}/signal_types/{signal_type_slug}/v{sem_ver}/signals [post]
func (s *SignalsHandler) CreateSignalsHandler(w http.ResponseWriter, r *http.Request) {

	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")
	signalTypePath := fmt.Sprintf("%v/v%v", signalTypeSlug, semVer)

	claims, ok := auth.ContextAccessTokenClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	accountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get accountID from context")
		return
	}

	if claims.AccountID != accountID {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "the accountID and the claims.AccountID from context do not match")
		return
	}

	// check that this the user is requesting a valid signal type/sem_ver for this isn
	found := slices.Contains(claims.IsnPerms[isnSlug].SignalTypePaths, signalTypePath)
	if !found {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("signal type %v is not available on ISN %v", signalTypePath, isnSlug))
		return
	}

	// TODO - temp table > upsert for larger payloads)

	createSignalsRequest := CreateSignalsRequest{}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&createSignalsRequest)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	createSignalsResponse := CreateSignalsResponse{
		IsnSlug:        isnSlug,
		SignalTypePath: signalTypePath,
		AccountID:      accountID,
	}

	storedSignals := make([]StoredSignal, 0)

	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to being transaction: %v", err))
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to rollback transaction: %v", err))
			return
		}
	}()

	txQueries := s.queries.WithTx(tx)

	for _, signal := range createSignalsRequest.Signals {
		// if the processing fails - because of a bad record, report which one failed and produce an audit
		// return signal id/signal master id.

		var err error

		// create the signal master record the first time this acccount id/signal type/local_ref is received
		if signal.CorrelationId == nil {
			_, err = txQueries.CreateSignal(r.Context(), database.CreateSignalParams{
				AccountID:      claims.AccountID,
				LocalRef:       signal.LocalRef,
				SignalTypeSlug: signalTypeSlug,
				SemVer:         semVer,
			})
		} else {

			// if an invalid correlation id is supplied this insert will fail with a fk error (checked for below)
			_, err = txQueries.CreateOrUpdateSignalWithCorrelationID(r.Context(), database.CreateOrUpdateSignalWithCorrelationIDParams{
				AccountID:      claims.AccountID,
				LocalRef:       signal.LocalRef,
				SignalTypeSlug: signalTypeSlug,
				CorrelationID:  *signal.CorrelationId,
				SemVer:         semVer,
			})
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) { // no rows are created if adding a new version of an existing signal
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				if pgErr.Code == "23503" && pgErr.ConstraintName == "fk_correlation_id" {
					responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidCorrelationID, fmt.Sprintf("error processing local ref %s - invalid correlation id %v", signal.LocalRef, signal.CorrelationId))
					return
				}
			}
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: local ref %s - could not insert signals: %v", signal.LocalRef, err))
			return
		}

		// record a new version of the signal
		returnedRow, err := txQueries.CreateSignalVersion(r.Context(), database.CreateSignalVersionParams{
			AccountID:      claims.AccountID,
			LocalRef:       signal.LocalRef,
			SignalTypeSlug: signalTypeSlug,
			SemVer:         semVer,
			SignalBatchID:  *claims.IsnPerms[isnSlug].SignalBatchID,
			Content:        signal.Content,
		})
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: local ref %s - could not insert signal_versions: %v", signal.LocalRef, err))
			return
		}

		storedSignals = append(storedSignals, StoredSignal{
			LocalRef:        signal.LocalRef,
			SignalVersionID: returnedRow.ID,
			VersionNumber:   returnedRow.VersionNumber,
		})
	}

	createSignalsResponse.StoredSignals = storedSignals

	if err = tx.Commit(r.Context()); err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("failed to commit transaction: %v", err))
		return
	}
	// for array insert
	responses.RespondWithJSON(w, http.StatusCreated, createSignalsResponse)
}

// DeleteSignalsHandler godocs
//
//	@Summary	Withdraw a signal (TODO)
//	@Tags		Signal sharing
//
//	@Router		/isn/{isn_slug}/signal_types/{signal_type_slug}/signals/{signal_id} [delete]
func (s *SignalsHandler) DeleteSignalHandler(w http.ResponseWriter, r *http.Request) {
	responses.RespondWithError(w, r, http.StatusNoContent, apperrors.ErrCodeNotImplemented, "todo - signals not yet implemented")
}

// GetSignalsHandler godocs
//
//	@Summary	get a signal (TODO)
//	@Tags		Signal sharing
//
//	@Router		/isn/{isn_slug}/signal_types/{signal_type_slug}/signals/{signal_id} [get]
func (s *SignalsHandler) GetSignalHandler(w http.ResponseWriter, r *http.Request) {
	responses.RespondWithError(w, r, http.StatusNoContent, apperrors.ErrCodeNotImplemented, "todo - signals not yet implemented")
}
