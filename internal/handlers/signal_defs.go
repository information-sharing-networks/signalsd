package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/nickabs/signals"
	"github.com/nickabs/signals/internal/apperrors"
	"github.com/nickabs/signals/internal/context"
	"github.com/nickabs/signals/internal/database"
	"github.com/nickabs/signals/internal/helpers"
	"github.com/nickabs/signals/internal/response"
)

type SignalDefHandler struct {
	cfg *signals.ServiceConfig
}

func NewSignalDefHandler(cfg *signals.ServiceConfig) *SignalDefHandler {
	return &SignalDefHandler{cfg: cfg}
}

type CreateSignalDefRequest struct {
	SchemaURL string `json:"schema_url" example:"https://github.com/user/project/v0.0.1/locales/filename.json"` // Note file must be on a public github repo
	Title     string `json:"title" example:"Sample Signal @example.org"`                                        // unique title
	BumpType  string `json:"bump_type" example:"patch" enums:"major,minor,patch"`                               // this is used to increment semver for the signal definition
	IsnSlug   string `json:"isn_slug" example:"sample-isn--example-org"`
	UpdateSignalDefRequest
}

type CreateSignalDefResponse struct {
	ID          uuid.UUID `json:"id" example:"8e4bf0e9-b962-4707-9639-ef314dcf6fed"`
	Slug        string    `json:"slug" example:"sample-signal--example-org"`
	SemVer      string    `json:"sem_ver" example:"0.0.1"`
	ResourceURL string    `json:"resource_url"`
}

// these are the only fields that can be updated after a signal is defined
type UpdateSignalDefRequest struct {
	ReadmeURL *string `json:"readme_url" example:"https://github.com/user/project/v0.0.1/locales/filename.md"` // Updated readme file. Note file must be on a public github repo
	Detail    *string `json:"detail" example:"description"`                                                    // updated description
	Stage     *string `json:"stage" enums:"dev,test,live,deprecated,closed,shuttered"`                         // updated stage
}

// used in GET handler
type SignalDefAndLinkedInfo struct {
	database.GetForDisplaySignalDefBySlugRow
	Isn  database.Isn                               `json:"isn"`
	User database.GetForDisplayUserBySignalDefIDRow `json:"user"`
}

// CreateSignalDefHandler godoc
//
//	@Summary		Create signal definition
//	@Description	A signal definition describes a data set that is sharable over an ISN.  Setup the ISN before defining any signal defs.
//	@Description
//	@Description	A URL-friendly slug is created based on the title supplied when you load the first version of a definition.
//	@Description	The title and slug fields can't be changed and it is not allowed to reuse a slug that was created by another account.
//	@Description
//	@Description	Slugs are vesioned automatically with semvers: when there is a change to the schema describing the data, the user should create a new definition and specify the bump type (major/minor/patch) to increment the semver
//	@Description
//	@Description	Signal definitions are referred to with a url like this http://{hostname}/api/signal_defs/{slug}/v{sem_ver}
//	@Description
//
//	@Tags		signal config
//
//	@Param		request	body		handlers.CreateSignalDefRequest	true	"signal definition details"
//
//	@Success	201		{object}	handlers.CreateSignalDefResponse
//	@Failure	400		{object}	response.ErrorResponse
//	@Failure	409		{object}	response.ErrorResponse
//	@Failure	500		{object}	response.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/signal_defs [post]
func (s *SignalDefHandler) CreateSignalDefHandler(w http.ResponseWriter, r *http.Request) {
	//var res createSignalDefResponse
	var req CreateSignalDefRequest

	var slug string
	var semVer string

	userID, ok := context.UserID(r.Context())
	if !ok {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userID from middleware")
		return
	}

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// validate fields
	if req.SchemaURL == "" ||
		req.Title == "" ||
		req.BumpType == "" ||
		req.IsnSlug == "" ||
		req.ReadmeURL == nil ||
		req.Detail == nil ||
		req.Stage == nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "one or missing field in the body of the requet")
		return
	}

	// check isn exists, owned by user and is flagged as 'in use'
	isn, err := s.cfg.DB.GetIsnBySlug(r.Context(), req.IsnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isn.UserID != userID {
		response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}
	if !isn.IsInUse {
		response.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this ISN is marked as 'not in use'")
		return
	}

	if err := helpers.CheckSignalDefURL(req.SchemaURL, "schema"); err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid schema url: %v", err))
		return
	}
	if err := helpers.CheckSignalDefURL(*req.ReadmeURL, "readme"); err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid readme url: %v", err))
		return
	}

	if !signals.ValidSignalDefStages[*req.Stage] {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "invalid stage supplied")
		return
	}

	// generate slug.
	slug, err = helpers.GenerateSlug(req.Title)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not create slug from title")
		return
	}

	// check if slug has already been used by another user (not permitted)
	exists, err := s.cfg.DB.ExistsSignalDefWithSlugAndDifferentUser(r.Context(), database.ExistsSignalDefWithSlugAndDifferentUserParams{
		Slug:   slug,
		UserID: userID,
	})
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}
	if exists {
		response.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, fmt.Sprintf("the {%s} slug is already in use - pick a new title for your signal def", slug))
		return
	}

	//  increment the semver using the supplied bump instruction supplied in the
	currentSignalDef, err := s.cfg.DB.GetSemVerAndSchemaForLatestSlugVersion(r.Context(), slug)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}

	if currentSignalDef.SchemaURL == req.SchemaURL {
		response.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, "you must supply an updated schemaURL if you want to bump the version")
		return
	}

	semVer, err = helpers.IncrementSemVer(req.BumpType, currentSignalDef.SemVer)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("could not bump sem ver : %v", err))
		return
	}

	// create signal def
	var returnedSignalDef database.SignalDef
	returnedSignalDef, err = s.cfg.DB.CreateSignalDef(r.Context(), database.CreateSignalDefParams{
		UserID:    userID,
		IsnID:     isn.ID,
		Slug:      slug,
		SemVer:    semVer,
		SchemaURL: req.SchemaURL,
		Title:     req.Title,
		Detail:    *req.Detail,
		ReadmeURL: *req.ReadmeURL,
		Stage:     *req.Stage,
	})
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create signal definition: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/signal_defs/%s/v%s",
		helpers.GetScheme(r),
		r.Host,
		slug,
		semVer,
	)

	response.RespondWithJSON(w, http.StatusCreated, CreateSignalDefResponse{
		ID:          returnedSignalDef.ID,
		Slug:        returnedSignalDef.Slug,
		SemVer:      returnedSignalDef.SemVer,
		ResourceURL: resourceURL,
	})
}

// UpdateSignalDefHandler godoc
//
//	@Summary		Update signal definition
//	@Description	users can update the detailed description, the stage or the link to the readme md
//	@Description
//	@Description	It is not allowed to update the schema url - instead users should create a new declaration with the same title and bump the version
//	@Param			slug	path	string							true	"signal definiton slug"		example(sample-signal--example-org)
//	@Param			sem_ver	path	string							true	"version to be recieved"	example(0.0.1)
//	@Param			request	body	handlers.UpdateSignalDefRequest	true	"signal definition details to be updated"
//
//	@Tags			signal config
//
//	@Success		204
//	@Failure		400	{object}	response.ErrorResponse
//	@Failure		401	{object}	response.ErrorResponse
//	@Failure		500	{object}	response.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/signal_defs/{slug}/v{sem_ver} [put]
func (s *SignalDefHandler) UpdateSignalDefHandler(w http.ResponseWriter, r *http.Request) {

	var req = UpdateSignalDefRequest{}

	userID, ok := context.UserID(r.Context())
	if !ok {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userID from middleware")
	}

	slug := r.PathValue("slug")
	semVer := r.PathValue("sem_ver")

	// check signal def eists
	signalDef, err := s.cfg.DB.GetSignalDefBySlug(r.Context(), database.GetSignalDefBySlugParams{
		Slug:   slug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No signal definition found for %s/v%s", slug, semVer))
			return
		}
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}

	if signalDef.UserID != userID {
		response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, "you can't update this signal definition")
		return
	}

	//check body
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.Detail == nil &&
		req.ReadmeURL == nil &&
		req.Stage == nil {
		response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "no updateable fields found in body of request")
		return
	}
	// prepare struct for update
	if req.ReadmeURL != nil {
		if err := helpers.CheckSignalDefURL(*req.ReadmeURL, "readme"); err != nil {
			response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid readme url: %v", err))
			return
		}
		signalDef.ReadmeURL = *req.ReadmeURL
	}

	if req.Detail != nil {
		signalDef.Detail = *req.Detail
	}

	if req.Stage != nil {
		if !signals.ValidSignalDefStages[*req.Stage] {
			response.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeInvalidRequest, "invalid stage supplied")
			return
		}
		signalDef.Stage = *req.Stage
	}

	// update signal_defs
	rowsAffected, err := s.cfg.DB.UpdateSignalDefDetails(r.Context(), database.UpdateSignalDefDetailsParams{
		ID:        signalDef.ID,
		ReadmeURL: signalDef.ReadmeURL,
		Detail:    signalDef.Detail,
		Stage:     signalDef.Stage,
	})
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}
	if rowsAffected != 1 {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error - more than one signal definition deleted")
		return
	}
	response.RespondWithJSON(w, http.StatusNoContent, "")
}

// DeleteSignalDefHandler godoc
//
//	@Summary	Delete signal definition
//	@Tags		signal config
//	@Param		slug	path	string	true	"signal definiton slug"		example(sample-signal--example-org)
//	@Param		sem_ver	path	string	true	"version to be recieved"	example(0.0.1)
//
//	@Success	204
//	@Failure	400	{object}	response.ErrorResponse
//	@Failure	401	{object}	response.ErrorResponse
//	@Failure	500	{object}	response.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/signal_defs/{slug}/v{sem_ver} [delete]
func (s *SignalDefHandler) DeleteSignalDefHandler(w http.ResponseWriter, r *http.Request) {

	userID, ok := context.UserID(r.Context())
	if !ok {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userID from middleware")
	}

	slug := r.PathValue("slug")
	semVer := r.PathValue("sem_ver")

	// check signal def eists
	signalDef, err := s.cfg.DB.GetSignalDefBySlug(r.Context(), database.GetSignalDefBySlugParams{
		Slug:   slug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No signal definition found for %s/v%s", slug, semVer))
			return
		}
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}

	if signalDef.UserID != userID {
		response.RespondWithError(w, r, http.StatusUnauthorized, apperrors.ErrCodeAuthorizationFailure, "you can't delete this signal definition")
		return
	}

	rowsAffected, err := s.cfg.DB.DeleteSignalDef(r.Context(), signalDef.ID)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}
	if rowsAffected > 1 {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error - more than one signal definition deleted")
		return
	}
	response.RespondWithJSON(w, http.StatusNoContent, "")
}

// GetSignalDefHandler godoc
//
//	@Summary	Get a signal definition
//	@Param		slug	path	string	true	"signal definiton slug"		example(sample-signal--example-org)
//	@Param		sem_ver	path	string	true	"version to be recieved"	example(0.0.1)
//
//	@Tags		ISN view
//
//	@Success	200	{object}	handlers.SignalDefAndLinkedInfo
//	@Failure	400	{object}	response.ErrorResponse
//	@Failure	404	{object}	response.ErrorResponse
//	@Failure	500	{object}	response.ErrorResponse
//
//	@Router		/api/signal_defs/{slug}/v{sem_ver} [get]
func (s *SignalDefHandler) GetSignalDefHandler(w http.ResponseWriter, r *http.Request) {

	slug := r.PathValue("slug")
	semVer := r.PathValue("sem_ver")

	// check signal def eists
	signalDef, err := s.cfg.DB.GetForDisplaySignalDefBySlug(r.Context(), database.GetForDisplaySignalDefBySlugParams{
		Slug:   slug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No signal definition found for %s/v%s", slug, semVer))
			return
		}
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}

	isn, err := s.cfg.DB.GetIsnBySignalDefID(r.Context(), signalDef.ID)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}

	// get the owner of the signal def
	user, err := s.cfg.DB.GetForDisplayUserBySignalDefID(r.Context(), signalDef.ID)
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("There was an error getting the user for this signal definition: %v", err))
		return
	}

	res := SignalDefAndLinkedInfo{
		GetForDisplaySignalDefBySlugRow: signalDef,
		Isn:                             isn,
		User:                            user,
	}
	response.RespondWithJSON(w, http.StatusOK, res)
}

// GetSignalDefsHandler godoc
//
//	@Summary	Get the signal definitions
//	@Tags		ISN view
//
//	@Success	200	{array}		database.GetSignalDefsRow
//	@Failure	500	{object}	response.ErrorResponse
//
//	@Router		/api/signal_defs [get]
func (s *SignalDefHandler) GetSignalDefsHandler(w http.ResponseWriter, r *http.Request) {

	res, err := s.cfg.DB.GetSignalDefs(r.Context())
	if err != nil {
		response.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error getting signalDefs from database: %v", err))
		return
	}
	response.RespondWithJSON(w, http.StatusOK, res)

}
