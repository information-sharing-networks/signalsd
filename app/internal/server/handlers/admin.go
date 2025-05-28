package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/server/responses"
)

type AdminHandler struct {
	queries *database.Queries
}

func NewAdminHandler(queries *database.Queries) *AdminHandler {
	return &AdminHandler{queries: queries}
}

// ResetHandler godoc
//
//	@Summary		reset
//	@Description	Delete all registered users and associated data.
//	@Description	This endpoint only works on environments configured as 'dev'
//	@Tags			Site admin
//
//	@Success		200
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Router			/admin/reset [post]
func (a *AdminHandler) ResetHandler(w http.ResponseWriter, r *http.Request) {

	deletedAccountsCount, err := a.queries.DeleteAccounts(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not delete accounts: %v", err))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("%d accounts deleted", deletedAccountsCount)))
}

// ReadinessHandler godoc
//
//	@Summary		Readiness
//	@Description	check if the signalsd service is ready
//	@Tags			Site admin
//
//	@Success		200
//	@Failure		404
//
//	@Router			/health/ready [Get]
func (a *AdminHandler) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	_, err := a.queries.IsDatabaseRunning(ctx)
	if err == nil {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(http.StatusText(http.StatusOK)))
	}
}

// Liveness godoc
//
//	@Summary		Liveness check
//	@Description	check if the signalsd service is up
//	@Tags			Site admin
//
//	@Success		200
//	@Failure		404
//
//	@Router			/admin/live [Get]
func (a *AdminHandler) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(http.StatusText(http.StatusOK)))
}
