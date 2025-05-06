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

func (a *AdminHandler) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(http.StatusText(http.StatusOK)))
}
