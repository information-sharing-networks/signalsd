package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/database"
	"github.com/nickabs/signals/internal/helpers"
)

type SignalDefHandler struct {
	cfg *signals.ServiceConfig
}

func NewSignalDefHandler(cfg *signals.ServiceConfig) *SignalDefHandler {
	return &SignalDefHandler{cfg: cfg}
}

// CreateSignalDefHandler godoc
//
//	@Summary		Create signal definition
//	@Description	The supplied title is converted into a url-friendly slug.
//	@Description
//	@Description	Slugs represent a group of versioned signal definitions.
//	@Description
//	@Description	Slugs are owned by the originating user and can't be reused by other users.
//	@Description
//	@Description	Slugs are vesioned automatically with semvers: when there is a change to the schema describing the data, the user should create a new def and specify the bump type (major/minor/patch) to increment the semver
//	@Description
//	@Description	Slugs are vesioned automatically with semvers: when there is a change to the schema describing the data, the user should create a new def and specify the bump type (major/minor/patch) to increment the semver
//	@Description
//
//	@Tags		signal definitions
//
//	@Param		request	body		handlers.CreateSignalDefHandler.createSignalDefRequest	true	"signal definition etails"
//
//	@Success	201		{object}	handlers.CreateSignalDefHandler.createSignalDefResponse
//	@Failure	400		{object}	signals.ErrorResponse
//	@Failure	409		{object}	signals.ErrorResponse
//	@Failure	500		{object}	signals.ErrorResponse
//
//	@Router		/api/signal_defs [post]
func (s *SignalDefHandler) CreateSignalDefHandler(w http.ResponseWriter, r *http.Request) {
	type createSignalDefRequest struct {
		SchemaURL string `json:"schema_url" example:"https://github.com/user/project/v0.0.1/locales/filename.json"` // Note file must be on a public github repo
		ReadmeURL string `json:"readme_url" example:"https://github.com/user/project/v0.0.1/locales/filename.md"`   // Note file must be on a public github repo
		Title     string `json:"title" example:"Sample Signal"`
		Detail    string `json:"detail" example:"Sample Signal description"`
		BumpType  string `json:"bump_type" example:"minor"` // major/minor/patch - this is used to increment semver for this signal def
		Stage     string `json:"stage" example:"dev"`       // dev/test/live/deprecated/closed/shuttered
	}

	type createSignalDefResponse struct {
		ID        uuid.UUID `json:"id"`
		CreatedAt time.Time `json:"created_at"`
		Slug      string    `json:"slug"`
		SemVer    string    `json:"sem_ver"`
	}
	//var res createSignalDefResponse
	var req createSignalDefRequest

	var createParams database.CreateSignalDefParams

	// these values are calcuated based on supplied req and used as part of the update on the signal_defs tables
	var slug string
	var semVer string
	var userID uuid.UUID

	ctx := r.Context()

	userID, ok := ctx.Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
		return
	}

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// validate fields
	if req.BumpType == "" || req.Detail == "" || req.ReadmeURL == "" || req.SchemaURL == "" ||
		req.Title == "" || req.Stage == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "one or missing field in the body of the requet")
		return
	}

	if err := helpers.CheckSignalDefURL(req.SchemaURL, "schema"); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("invalid schema url: %v", err))
		return
	}
	if err := helpers.CheckSignalDefURL(req.ReadmeURL, "readme"); err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("invalid readme url: %v", err))
		return
	}
	if !signals.ValidSignalDefStages[req.Stage] {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, "invalid stage supplied")
		return
	}

	// generate slug.
	slug, err := helpers.GenerateSlug(req.Title)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "could not create slug from title")
		return
	}

	// check if slug has already been used by another user (not permitted)
	exists, err := s.cfg.DB.ExistsSignalDefWithSlugAndDifferentUser(r.Context(), database.ExistsSignalDefWithSlugAndDifferentUserParams{
		Slug:   slug,
		UserID: userID,
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "database error")
		return
	}
	if exists {
		helpers.RespondWithError(w, r, http.StatusConflict, signals.ErrCodeResourceAlreadyExists, fmt.Sprintf("the {%s} slug is already in use - pick a new title for your slug", slug))
		return
	}

	//  increment the semver using the supplied bump instruction supplied in the
	currentSignalDef, err := s.cfg.DB.GetSemVerAndSchemaForLatestSlugVersion(r.Context(), slug)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}

	if currentSignalDef.SchemaURL == req.SchemaURL {
		helpers.RespondWithError(w, r, http.StatusConflict, signals.ErrCodeResourceAlreadyExists, "you must supply an updated schemaURL if you want to bump the version")
		return
	}

	semVer, err = helpers.IncrementSemVer(req.BumpType, currentSignalDef.SemVer)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, fmt.Sprintf("could not bump sem ver : %v", err))
		return
	}

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

	var returnedUser database.SignalDef
	returnedUser, err = s.cfg.DB.CreateSignalDef(r.Context(), createParams)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("could not create signal definition: %v", err))
		return
	}

	helpers.RespondWithJSON(w, http.StatusCreated, createSignalDefResponse{
		ID:        returnedUser.ID,
		CreatedAt: returnedUser.CreatedAt,
		Slug:      returnedUser.Slug,
		SemVer:    returnedUser.SemVer,
	})
}

// UpdateSignalDefHandler godoc
//
//	@Summary		Update signal definition
//	@Description	users can update the detailed description, the stage or the link to the readme md
//	@Description
//	@Description	Note that it is not allowed to update the schema url - instead users should create a new declaration with the same title and bump the version
//	@Tags			signal definitions
//
//	@Param			request	body	handlers.UpdateSignalDefHandler.updateSignalDefRequest	true	"signal definition etails"
//
//	@Success		204
//	@Failure		400	{object}	signals.ErrorResponse
//	@Failure		403	{object}	signals.ErrorResponse
//	@Failure		500	{object}	signals.ErrorResponse
//
//	@Router			/api/signal_defs [put]
func (s *SignalDefHandler) UpdateSignalDefHandler(w http.ResponseWriter, r *http.Request) {
	type updateSignalDefRequest struct {
		ReadmeURL string `json:"readme_url" example:"https://github.com/user/project/v0.0.1/locales/new_filename.md"` // Note file must be on a public github repo
		Detail    string `json:"detail" example:"updated description"`
		Stage     string `json:"stage" example:"test"` // dev/test/live/deprecated/closed/shuttered
	}

	var req = updateSignalDefRequest{}

	ctx := r.Context()

	userID, ok := ctx.Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
	}

	// check url
	signalDefIDString := r.PathValue("SignalDefID")

	if signalDefIDString == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, "expected /api/signal_defs/{SignalDefID}")
		return
	}

	signalDefID, err := uuid.Parse(signalDefIDString)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, fmt.Sprintf("Invalid signal definition ID: %v", err))
		return
	}
	currentSignalDef, err := s.cfg.DB.GetSignalDef(r.Context(), signalDefID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeResourceNotFound, "Signal def not found")
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, "database error")
		return
	}
	if currentSignalDef.UserID != userID {
		helpers.RespondWithError(w, r, http.StatusForbidden, signals.ErrCodeAuthorizationFailure, "you can't update this signal definition")
		return
	}

	//check body
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.Detail == "" && req.ReadmeURL == "" && req.Stage == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, "no updateable fields found in body of request")
		return
	}

	if req.Stage != "" {
		if !signals.ValidSignalDefStages[req.Stage] {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, "invalid stage supplied")
			return
		}
	}

	if req.ReadmeURL != "" {
		if err := helpers.CheckSignalDefURL(req.ReadmeURL, "readme"); err != nil {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeMalformedBody, fmt.Sprintf("invalid readme url: %v", err))
			return
		}
	}

	// if values not supplied in json then use the currentValues
	if req.ReadmeURL == "" {
		req.ReadmeURL = currentSignalDef.ReadmeURL
	}

	if req.Detail == "" {
		req.Detail = currentSignalDef.Detail
	}

	if req.Stage == "" {
		req.Stage = currentSignalDef.Stage
	}

	// update signal_defs
	rowsAffected, err := s.cfg.DB.UpdateSignalDefDetails(r.Context(), database.UpdateSignalDefDetailsParams{
		ID:        signalDefID,
		ReadmeURL: req.ReadmeURL,
		Detail:    req.Detail,
		Stage:     req.Stage,
	})
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

// only delete by signal def id is supported currently
// TODO - delete by slug; add controls to prevent/warn when deleting active signal defs.
func (s *SignalDefHandler) DeleteSignalDefsHandler(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()

	userID, ok := ctx.Value(signals.UserIDKey).(uuid.UUID)
	if !ok {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeInternalError, "did not receive userID from middleware")
	}

	signalDefIDString := r.PathValue("SignalDefID")

	if signalDefIDString == "" {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, "expected /api/signal_defs/{SignalDefID}")
		return
	}

	signalDefID, err := uuid.Parse(signalDefIDString)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, fmt.Sprintf("Invalid signal definition ID: %v", err))
		return
	}
	signalDef, err := s.cfg.DB.GetSignalDef(r.Context(), signalDefID)
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
