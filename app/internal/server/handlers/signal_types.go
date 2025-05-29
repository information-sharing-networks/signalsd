package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nickabs/signalsd/app/internal/apperrors"
	"github.com/nickabs/signalsd/app/internal/auth"
	"github.com/nickabs/signalsd/app/internal/database"
	"github.com/nickabs/signalsd/app/internal/server/responses"
	"github.com/nickabs/signalsd/app/internal/server/utils"
)

type SignalTypeHandler struct {
	queries *database.Queries
}

func NewSignalTypeHandler(queries *database.Queries) *SignalTypeHandler {
	return &SignalTypeHandler{queries: queries}
}

type CreateSignalTypeRequest struct {
	SchemaURL string  `json:"schema_url" example:"https://github.com/user/project/v0.0.1/locales/filename.json"` // Note file must be on a public github repo
	Title     string  `json:"title" example:"Sample Signal @example.org"`                                        // unique title
	BumpType  string  `json:"bump_type" example:"patch" enums:"major,minor,patch"`                               // this is used to increment semver for the signal definition
	ReadmeURL *string `json:"readme_url" example:"https://github.com/user/project/v0.0.1/locales/filename.md"`   // Updated readme file. Note file must be on a public github repo
	Detail    *string `json:"detail" example:"description"`                                                      // updated description
}

type CreateSignalTypeResponse struct {
	Slug        string `json:"slug" example:"sample-signal--example-org"`
	SemVer      string `json:"sem_ver" example:"0.0.1"`
	ResourceURL string `json:"resource_url" example:"http://localhost:8080/api/isn/sample-isn--example-org/signals_types/sample-signal--example-org/v0.0.1"`
}

// these are the only fields that can be updated after a signal type is defined
type UpdateSignalTypeRequest struct {
	ReadmeURL *string `json:"readme_url" example:"https://github.com/user/project/v0.0.1/locales/filename.md"` // Updated readme file. Note file must be on a public github repo
	Detail    *string `json:"detail" example:"description"`                                                    // updated description
	IsInUse   *bool   `json:"is_in_use" example:"false"`
}

// used in GET handler
type SignalTypeAndLinkedInfo struct {
	database.GetForDisplaySignalTypeBySlugRow
	Isn database.Isn `json:"isn"`
}

// CreateSignalTypeHandler godoc
//
//	@Summary		Create signal type definition
//	@Description	A signal type describes a data set that is sharable over an ISN.  Setup the ISN before defining any signal defs.
//	@Description	Signal types specify a record that can be shared over the ISN
//	@Description	- Each type has a unique title
//	@Description	- A URL-friendly slug is created based on the title supplied when you load the first version of a definition.
//	@Description	- The title and slug fields can't be changed and it is not allowed to reuse a slug that was created by another account.
//	@Description	- The field definition is held as an external json schema file
//	@Description
//	@Description	Versions
//	@Description	- A signal type can have multiple versions - these share the same title/slug but have different json schemas.
//	@Description	- use this endpoint to create the first version - the bump_type (major/minor/patch) determines the inital semver(1.0.0, 0.1.0 or 0.0.1)
//	@Description	- subsequent POSTs to this endpoint that reference a previously sumbitted title/slug but point to a different schema will increment the version
//	@Description
//	@Description	Signal type definitions are referred to with a url like this http://{hostname}/api/isn/{isn_slug}/signal_types/{slug}/v{sem_ver}
//	@Description
//
//	@Tags		signal config
//
//	@Param		request	body		handlers.CreateSignalTypeRequest	true	"signal definition details"
//
//	@Success	201		{object}	handlers.CreateSignalTypeResponse
//	@Failure	400		{object}	responses.ErrorResponse
//	@Failure	409		{object}	responses.ErrorResponse
//	@Failure	500		{object}	responses.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/isn/{isn_slug}/signal_types [post]
//
// Should only be used with RequiresIsnWritePermission middleware
func (s *SignalTypeHandler) CreateSignalTypeHandler(w http.ResponseWriter, r *http.Request) {
	//var res createSignalTypeResponse
	var req CreateSignalTypeRequest

	var slug string
	var semVer string

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	isnSlug := r.PathValue("isn_slug")

	// check isn exists and is owned by user
	isn, err := s.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isn.UserAccountID != userAccountID {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}
	// check the isn is in use
	if !isn.IsInUse {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this ISN is marked as 'not in use'")
		return
	}

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	// validate fields
	if req.SchemaURL == "" ||
		req.Title == "" ||
		req.BumpType == "" ||
		req.ReadmeURL == nil ||
		req.Detail == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "one or missing field in the body of the requet")
		return
	}

	if !isn.IsInUse {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this ISN is marked as 'not in use'")
		return
	}

	if err := utils.CheckSignalTypeURL(req.SchemaURL, "schema"); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid schema url: %v", err))
		return
	}
	if err := utils.CheckSignalTypeURL(*req.ReadmeURL, "readme"); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid readme url: %v", err))
		return
	}

	// generate slug.
	slug, err = utils.GenerateSlug(req.Title)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not create slug from title")
		return
	}

	//  if this is the first version then the query below returns currentSignalType.semver == "0.0.0"
	currentSignalType, err := s.queries.GetSemVerAndSchemaForLatestSlugVersion(r.Context(), slug)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
		return
	}

	// if there is already a version for this slug, assume the client wants to bump the version and...
	if currentSignalType.SemVer != "0.0.0" {
		//... check the signal type was not previously registerd with this schema
		exists, err := s.queries.ExistsSignalTypeWithSlugAndSchema(r.Context(), database.ExistsSignalTypeWithSlugAndSchemaParams{
			Slug:      slug,
			SchemaURL: req.SchemaURL,
		})
		if err != nil {
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("database error: %v", err))
			return
		}
		if exists {
			responses.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, "you must supply an updated schemaURL if you want to bump the version")
			return
		}
	}

	//  increment the semver using the supplied bump instruction supplied in the req
	semVer, err = utils.IncrementSemVer(req.BumpType, currentSignalType.SemVer)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, fmt.Sprintf("could not bump sem ver : %v", err))
		return
	}

	// create signal def
	var returnedSignalType database.SignalType
	returnedSignalType, err = s.queries.CreateSignalType(r.Context(), database.CreateSignalTypeParams{
		IsnID:     isn.ID,
		Slug:      slug,
		SemVer:    semVer,
		SchemaURL: req.SchemaURL,
		Title:     req.Title,
		Detail:    *req.Detail,
		ReadmeURL: *req.ReadmeURL,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("could not create signal definition: %v", err))
		return
	}

	resourceURL := fmt.Sprintf("%s://%s/api/isn/%s/signal_types/%s/v%s",
		utils.GetScheme(r),
		r.Host,
		isn.Slug,
		slug,
		semVer,
	)

	responses.RespondWithJSON(w, http.StatusCreated, CreateSignalTypeResponse{
		Slug:        returnedSignalType.Slug,
		SemVer:      returnedSignalType.SemVer,
		ResourceURL: resourceURL,
	})
}

// UpdateSignalTypeHandler godoc
//
//	@Summary		Update signal definition
//	@Description	users can mark the signal type as *in use/not in use* and update the description or link to the readme file
//	@Description
//	@Description	It is not allowed to update the schema url - instead users should create a new declaration with the same title and bump the version
//	@Param			slug	path	string								true	"signal definiton slug"		example(sample-signal--example-org)
//	@Param			sem_ver	path	string								true	"Sem ver"	example(0.0.1)
//	@Param			request	body	handlers.UpdateSignalTypeRequest	true	"signal type details to be updated"
//
//	@Tags			signal config
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal_types/{slug}/v{sem_ver} [put]
func (s *SignalTypeHandler) UpdateSignalTypeHandler(w http.ResponseWriter, r *http.Request) {

	var req = UpdateSignalTypeRequest{}

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
	}

	slug := r.PathValue("slug")
	semVer := r.PathValue("sem_ver")

	// check signal def exists
	signalType, err := s.queries.GetSignalTypeBySlug(r.Context(), database.GetSignalTypeBySlugParams{
		Slug:   slug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No signal type found for %s/v%s", slug, semVer))
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}

	isnSlug := r.PathValue("isn_slug")

	// check isn exists and is owned by user
	isn, err := s.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error: %v", err))
		return
	}
	if isn.UserAccountID != userAccountID {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you are not the owner of this ISN")
		return
	}
	// check the isn is in use
	if !isn.IsInUse {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "this ISN is marked as 'not in use'")
		return
	}

	//check body
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("could not decode request body: %v", err))
		return
	}

	if req.Detail == nil &&
		req.ReadmeURL == nil &&
		req.IsInUse == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "no updateable fields found in body of request")
		return
	}
	// prepare struct for update
	if req.ReadmeURL != nil {
		if err := utils.CheckSignalTypeURL(*req.ReadmeURL, "readme"); err != nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid readme url: %v", err))
			return
		}
		signalType.ReadmeURL = *req.ReadmeURL
	}

	if req.Detail != nil {
		signalType.Detail = *req.Detail
	}

	if req.IsInUse != nil {
		signalType.IsInUse = *req.IsInUse
	}

	// update signal_types
	rowsAffected, err := s.queries.UpdateSignalTypeDetails(r.Context(), database.UpdateSignalTypeDetailsParams{
		ID:        signalType.ID,
		ReadmeURL: signalType.ReadmeURL,
		Detail:    signalType.Detail,
		IsInUse:   signalType.IsInUse,
	})
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}
	if rowsAffected != 1 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error - more than one signal type deleted")
		return
	}
	responses.RespondWithJSON(w, http.StatusNoContent, "")
}

// GetSignalTypeHandler godoc
//
//	@Summary	Get a signal definition
//	@Param		slug	path	string	true	"signal definiton slug"		example(sample-signal--example-org)
//	@Param		sem_ver	path	string	true	"version to be recieved"	example(0.0.1)
//
//	@Tags		ISN view
//
//	@Success	200	{object}	handlers.SignalTypeAndLinkedInfo
//	@Failure	400	{object}	responses.ErrorResponse
//	@Failure	404	{object}	responses.ErrorResponse
//	@Failure	500	{object}	responses.ErrorResponse
//
//	@Router		/api/isn/{isn_slug}/signal_types/{slug}/v{sem_ver} [get]
func (s *SignalTypeHandler) GetSignalTypeHandler(w http.ResponseWriter, r *http.Request) {

	slug := r.PathValue("slug")
	semVer := r.PathValue("sem_ver")

	// check signal def eists
	signalType, err := s.queries.GetForDisplaySignalTypeBySlug(r.Context(), database.GetForDisplaySignalTypeBySlugParams{
		Slug:   slug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No signal definition found for %s/v%s", slug, semVer))
			return
		}
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}

	isn, err := s.queries.GetIsnBySignalTypeID(r.Context(), signalType.ID)
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("database error %v", err))
		return
	}

	res := SignalTypeAndLinkedInfo{
		GetForDisplaySignalTypeBySlugRow: signalType,
		Isn:                              isn,
	}
	responses.RespondWithJSON(w, http.StatusOK, res)
}

// GetSignalTypesHandler godoc
//
//	@Summary	Get the signal definitions
//	@Tags		ISN view
//
//	@Success	200	{array}		database.SignalType
//	@Failure	500	{object}	responses.ErrorResponse
//
//	@Router		/api/isn/{isn_slug}/signal_types [get]
func (s *SignalTypeHandler) GetSignalTypesHandler(w http.ResponseWriter, r *http.Request) {

	res, err := s.queries.GetSignalTypes(r.Context())
	if err != nil {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, fmt.Sprintf("error getting signalTypes from database: %v", err))
		return
	}
	responses.RespondWithJSON(w, http.StatusOK, res)

}
