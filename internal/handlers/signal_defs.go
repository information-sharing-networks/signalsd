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
	"github.com/rs/zerolog/log"
)

type SignalDefHandler struct {
	cfg *signals.ServiceConfig
}

func NewSignalDefHandler(cfg *signals.ServiceConfig) *SignalDefHandler {
	return &SignalDefHandler{cfg: cfg}
}

// §CreateSignalDefHandler godoc
//
// @Summary		Create signal definition
// @Description A signal definition describes a data set that is sharable over the signals ISN
// @Description
// @Description A URL-friendly slug is created based on the title supplied when you load the first version of a definition.
// @Description The title and slug fields can't be changed and it is not allowed to reuse a slug that was created by another account.
// @Description
// @Description Slugs are vesioned automatically with semvers: when there is a change to the schema describing the data, the user should create a new definition and specify the bump type (major/minor/patch) to increment the semver
// @Description
// @Description The standard way to refer to a signal definition is using a url like this http://{hostname}/signal_defs/{slug}/v{sem_ver}
// @Description
// @Description The definitions are also available at http://{hostname}/{signal_id}
//
// @Tags		signal definitions
//
// @Param		request	body		handlers.CreateSignalDefHandler.createSignalDefRequest	true	"signal definition etails"
//
// @Success	201		{object}	handlers.CreateSignalDefHandler.createSignalDefResponse
// @Failure	400		{object}	signals.ErrorResponse
// @Failure	409		{object}	signals.ErrorResponse
// @Failure	500		{object}	signals.ErrorResponse
//
// @Security	BearerAccessToken
//
// @Router		/api/signal_defs [post]
func (s *SignalDefHandler) CreateSignalDefHandler(w http.ResponseWriter, r *http.Request) {
	type createSignalDefRequest struct {
		SchemaURL string `json:"schema_url" example:"https://github.com/user/project/v0.0.1/locales/filename.json"` // Note file must be on a public github repo
		ReadmeURL string `json:"readme_url" example:"https://github.com/user/project/v0.0.1/locales/filename.md"`   // Note file must be on a public github repo
		Title     string `json:"title" example:"Sample Signal (example.org)"`                                       // unique title
		Detail    string `json:"detail" example:"Sample Signal description"`                                        // description
		BumpType  string `json:"bump_type" example:"patch" enums:"major,minor,patch"`                               // this is used to increment semver for the signal definition
		Stage     string `json:"stage" example:"dev" enums:"dev,test,live,deprecated,closed,shuttered"`
	}

	type createSignalDefResponse struct {
		ID        uuid.UUID `json:"id" example:"8e4bf0e9-b962-4707-9639-ef314dcf6fed"`
		CreatedAt time.Time `json:"created_at" example:"2025-05-09T13:25:44.126721+01:00"`
		Slug      string    `json:"slug" example:"sample-signal--example-org"`
		SemVer    string    `json:"sem_ver" example:"0.0.1"`
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
//	@Failure		401	{object}	signals.ErrorResponse
//	@Failure		500	{object}	signals.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/signal_defs [put]
func (s *SignalDefHandler) UpdateSignalDefHandler(w http.ResponseWriter, r *http.Request) {
	type updateSignalDefRequest struct {
		ReadmeURL string `json:"readme_url" example:"https://github.com/user/project/v0.0.1/locales/new_t pfilename.md"` // Updated readem file. Note file must be on a public github repo
		Detail    string `json:"detail" example:"updated description"`                                                   // updated description
		Stage     string `json:"stage" enums:"dev,test,live,deprecated,closed,shuttered"`                                // updated stage
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
	currentSignalDef, err := s.cfg.DB.GetSignalDefByID(r.Context(), signalDefID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeResourceNotFound, "Signal def not found")
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, "database error")
		return
	}
	if currentSignalDef.UserID != userID {
		helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeAuthorizationFailure, "you can't update this signal definition")
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

// GetSignalDefByIDHandler godoc
//
//	@Summary	Get a signal definition by id
//	@Param		id	path	string	true	"ID of the signal definition to retrieve"	example(6f4eb8dc-1411-4395-93d6-fc316b85aa74)
//	@Tags		signal definitions
//
//	@Success	200	{object}	database.GetSignalDefByIDRow
//	@Failure	400	{object}	signals.ErrorResponse
//	@Failure	404	{object}	signals.ErrorResponse
//	@Failure	500	{object}	signals.ErrorResponse
//
//	@Router		/api/signal_defs/{id} [get]
func (s *SignalDefHandler) GetSignalDefByIDHandler(w http.ResponseWriter, r *http.Request) {
	signalDefIDStr := r.PathValue("id")
	SignalDefID, err := uuid.Parse(signalDefIDStr)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeInvalidRequest, fmt.Sprintf("Invalid signal definition ID: %v", err))
		return
	}

	res, err := s.cfg.DB.GetSignalDefByID(r.Context(), SignalDefID)
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusNotFound, signals.ErrCodeDatabaseError, fmt.Sprintf("Could not get signal definition for the supplied id: %v", err))
		return
	}
	helpers.RespondWithJSON(w, http.StatusOK, res)

}

// GetSignalDefBySlugHandler godoc
//
//	@Summary	Get a signal definition by slug
//	@Param		slug	path	string	true	"signal definiton slug"	 example(sample-signal---example-org)
//	@Param		sem_ver	path	string	true	"version to be recieved"	 example(0.0.1)
//	@Tags		signal definitions
//
//	@Success	200	{object}	database.GetSignalDefBySlugRow
//	@Failure	400	{object}	signals.ErrorResponse
//	@Failure	404	{object}	signals.ErrorResponse
//	@Failure	500	{object}	signals.ErrorResponse
//
//	@Router		/api/signal_defs/{slug}/v{sem_ver} [get]
func (s *SignalDefHandler) GetSignalDefBySlugHandler(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	semVer := r.PathValue("sem_ver")

	log.Debug().Msgf("signalDefSlug %s signalDefSemVer %s", slug, semVer)

	res, err := s.cfg.DB.GetSignalDefBySlug(r.Context(), database.GetSignalDefBySlugParams{
		Slug:   slug,
		SemVer: semVer,
	})
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusNotFound, signals.ErrCodeDatabaseError, fmt.Sprintf("Could not get signal definition for the supplied slug and version: %s/v%s :%v", slug, semVer, err))
		return
	}
	helpers.RespondWithJSON(w, http.StatusOK, res)

}

// GetSignalDefsHandler godoc
//
//	@Summary	Get all of the signal definitions
//	@Tags		signal definitions
//
//	@Success	200	{array}	database.GetSignalDefsRow
//	@Failure	500	{object}	signals.ErrorResponse
//
//	@Router		/api/signal_defs [get]
func (s *SignalDefHandler) GetSignalDefsHandler(w http.ResponseWriter, r *http.Request) {

	res, err := s.cfg.DB.GetSignalDefs(r.Context())
	if err != nil {
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, fmt.Sprintf("error getting signalDefs from database: %v", err))
		return
	}
	helpers.RespondWithJSON(w, http.StatusOK, res)

}

// DeleteSignalDefHandler godoc
//
//	@Summary	Delete signal definition
//	@Tags		signal definitions
//
//	@Success	204
//	@Failure	400	{object}	signals.ErrorResponse
//	@Failure	401	{object}	signals.ErrorResponse
//	@Failure	500	{object}	signals.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/signal_defs [put]
func (s *SignalDefHandler) DeleteSignalDefsHandler(w http.ResponseWriter, r *http.Request) {
	// TODO - delete by slug; add controls to prevent/warn when deleting active signal defs.

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
	signalDef, err := s.cfg.DB.GetSignalDefByID(r.Context(), signalDefID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			helpers.RespondWithError(w, r, http.StatusBadRequest, signals.ErrCodeResourceNotFound, "Signal def not found")
			return
		}
		helpers.RespondWithError(w, r, http.StatusInternalServerError, signals.ErrCodeDatabaseError, "database error")
		return
	}
	if signalDef.UserID != userID {
		helpers.RespondWithError(w, r, http.StatusUnauthorized, signals.ErrCodeAuthorizationFailure, "you can't delete this signal definition")
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
