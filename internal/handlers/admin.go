package handlers

import (
	"fmt"
	"net/http"

	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/helpers"
)

type AdminHandler struct {
	cfg *signals.ServiceConfig
}

func NewAdminHandler(cfg *signals.ServiceConfig) *AdminHandler {
	return &AdminHandler{cfg: cfg}
}

// ResetHandler godoc
//
//	@Summary		reset
//	@Description	Delete all registered users and associated data.
//	@Description	This endpoint only works on environments configured as 'dev'
//	@Tags			admin
//
//	@Success		200
//	@Failure		403	{object}	signals.ErrorResponse
//	@Failure		500	{object}	signals.ErrorResponse
//
//	@Router			/admin/reset [post]
func (a *AdminHandler) ResetHandler(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Environment != "dev" {
		helpers.RespondWithError(w, r, http.StatusForbidden, signals.ErrCodeForbidden, "this api can only be used in the dev environment")
		return
	}
	deletedUserCount, err := a.cfg.DB.DeleteUsers(r.Context())
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("could not delete users: %v", err))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("%d users deleted", deletedUserCount)))
}

// ReadinessHandler godoc
//
//	@Summary		Health
//	@Description	check if the signals service is running
//	@Tags			admin
//
//	@Success		200
//	@Failure		404
//
//	@Router			/admin/health [Get]
func (a *AdminHandler) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(http.StatusText(http.StatusOK)))
}
