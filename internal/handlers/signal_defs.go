package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/database"
	"github.com/nickabs/signals/internal/helpers"
	"github.com/rs/zerolog/log"
)

type SignalDefHandler struct {
	cfg *signals.ServiceConfig
}

func NewSignalDefHandler(cfg *signals.ServiceConfig) *SignalDefHandler {
	return &SignalDefHandler{cfg: cfg}
}

// todo - handle dupe slugs/ optional detail field
func (s *SignalDefHandler) CreateSignalDefHandler(w http.ResponseWriter, r *http.Request) {
	type createSignalDefRequest struct {
		SchemaURL   string `json:"schema_url"`
		ReadmeURL   string `json:"readme_url"`
		Title       string `json:"title"`
		Detail      string `json:"detail"`
		BumpVersion string `json:"bump_version"` // major, minor, patch (used to increment signal_def.sem_ver)
		Stage       string `json:"stage"`        // dev/test/live/deprecated/closed
	}
	var req createSignalDefRequest
	var res database.SignalDef
	var createParams database.CreateSignalDefParams

	ctx := r.Context()

	defer r.Body.Close()

	userID, ok := ctx.Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
		return
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	slug, err := helpers.GenerateSlug(req.Title)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "could not create slug from title")
		return
	}

	if err := helpers.ValidateURL(req.SchemaURL); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "invalid schema url")
		return
	}

	//todo increment semver
	// check the slug was not already registered
	// check the schmea url has changed
	// check nulls
	// check stage
	// latest version
	//
	semVer := "0.0.1"

	log.Debug().Msgf("req.Stage %v\n", req.Stage)
	createParams = database.CreateSignalDefParams{
		Slug:      slug,
		SchemaURL: req.SchemaURL,
		ReadmeURL: req.ReadmeURL,
		Title:     req.Title,
		Detail:    req.Detail,
		Stage:     req.Stage,
		SemVer:    semVer,
		UserID:    userID,
	}
	res, err = s.cfg.DB.CreateSignalDef(r.Context(), createParams)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("could not create signal definition: %v", err))
		return
	}

	helpers.RespondWithJSON(w, http.StatusCreated, res)
}

func (s *SignalDefHandler) GetSignalDefHandler(w http.ResponseWriter, r *http.Request) {
	signalDefIDStr := r.PathValue("SignalDefID")
	SignalDefID, err := uuid.Parse(signalDefIDStr)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, fmt.Sprintf("Invalid signal definition ID: %v", err))
		return
	}

	res, err := s.cfg.DB.GetSignalDef(r.Context(), SignalDefID)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusNotFound, signals.ErrCodeDatabaseError, fmt.Sprintf("Could not get signal definition for the supplied id: %v", err))
		return
	}
	helpers.RespondWithJSON(w, http.StatusOK, res)

}
func (s *SignalDefHandler) GetSignalDefsHandler(w http.ResponseWriter, r *http.Request) {

	res, err := s.cfg.DB.GetSignalDefs(r.Context())
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("error getting signalDefs from database: %v", err))
		return
	}
	helpers.RespondWithJSON(w, http.StatusOK, res)

}
func (s *SignalDefHandler) DeleteSignalDefsHandler(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()

	userID, ok := ctx.Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
	}

	SignalDefIDString := r.PathValue("SignalDefID")

	if SignalDefIDString == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, "expected /api/signal_defs/{SignalDefID}")
		return
	}

	SignalDefID, err := uuid.Parse(SignalDefIDString)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, fmt.Sprintf("Invalid signal definition ID: %v", err))
		return
	}
	signalDef, err := s.cfg.DB.GetSignalDef(r.Context(), SignalDefID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeResourceNotFound, "Signal def not found")
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, "database error")
		return
	}
	if signalDef.UserID != userID {
		helpers.RespondWithError(w, r, http.StatusForbidden, signals.ErrCodeAuthorizationFailure, "you can't delete this signal definition")
		return
	}

	rowsAffected, err := s.cfg.DB.DeleteSignalDef(r.Context(), signalDef.ID)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}
	if rowsAffected != 1 {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, "database error - more than one signal definition deleted")
		return
	}
	helpers.RespondWithJSON(w, http.StatusNoContent, "")
}
