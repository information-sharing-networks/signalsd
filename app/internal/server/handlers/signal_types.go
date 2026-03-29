package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
	"github.com/information-sharing-networks/signalsd/app/internal/server/utils"
	"github.com/jackc/pgx/v5"
)

type SignalTypeHandler struct {
	queries *database.Queries
}

func NewSignalTypeHandler(queries *database.Queries) *SignalTypeHandler {
	return &SignalTypeHandler{queries: queries}
}

type CreateSignalTypeRequest struct {
	SchemaURL string `json:"schema_url" example:"https://github.com/user/project/blob/2025.01.01/schema.json"` // JSON schema URL: must be a GitHub URL ending in .json, OR use https://github.com/skip/validation/main/schema.json to disable validation
	Title     string `json:"title" example:"Sample Signal @example.org"`                                       // unique title
	BumpType  string `json:"bump_type" example:"patch" enums:"major,minor,patch"`                              // this is used to increment semver for the signal type
	ReadmeURL string `json:"readme_url" example:"https://github.com/user/project/blob/2025.01.01/readme.md"`   // README file URL: must be a GitHub URL ending in .md
	Detail    string `json:"detail" example:"description"`                                                     // description
}

type RegisterNewSignalTypeSchemaRequest struct {
	SchemaURL string `json:"schema_url" example:"https://github.com/user/project/blob/2025.01.01/schema.json"` // JSON schema URL: must be a GitHub URL ending in .json, OR use https://github.com/skip/validation/main/schema.json to disable validation
	BumpType  string `json:"bump_type" example:"patch" enums:"major,minor,patch"`                              // this is used to increment semver for the signal type
	ReadmeURL string `json:"readme_url" example:"https://github.com/user/project/blob/2025.01.01/readme.md"`   // README file URL: must be a GitHub URL ending in .md
	Detail    string `json:"detail" example:"description"`                                                     // description
}

type NewSignalTypeResponse struct {
	Slug   string `json:"slug" example:"sample-signal--example-org"`
	SemVer string `json:"sem_ver" example:"0.0.1"`
}

type AddSignalTypeToIsnRequest struct {
	SignalTypeSlug string `json:"signal_type_slug" example:"sample-signal--example-org"` // signal type slug
	SemVer         string `json:"sem_ver" example:"0.0.1"`                               // signal type version
}

type UpdateSignalTypeRequest struct {
	ReadmeURL *string `json:"readme_url" example:"https://github.com/user/project/blob/2025.01.01/readme.md"` // README file URL: must be a GitHub URL ending in .md
	Detail    *string `json:"detail" example:"description"`                                                   // updated description
	IsInUse   *bool   `json:"is_in_use" example:"false"`                                                      // whether this signal type version is actively used
}

// Response struct for GET handlers
type SignalTypeDetail struct {
	ID        uuid.UUID `json:"id" example:"67890684-3b14-42cf-b785-df28ce570400"`
	CreatedAt time.Time `json:"created_at" example:"2025-06-03T13:47:47.331787+01:00"`
	UpdatedAt time.Time `json:"updated_at" example:"2025-06-03T13:47:47.331787+01:00"`
	Slug      string    `json:"slug" example:"sample-signal-type"`
	SchemaURL string    `json:"schema_url" example:"https://github.com/user/project/blob/2025.01.01/schema.json"`
	ReadmeURL string    `json:"readme_url" example:"https://github.com/user/project/blob/2025.01.01/readme.md"`
	Title     string    `json:"title" example:"Sample Signal Type"`
	Detail    string    `json:"detail" example:"Sample signal type description"`
	SemVer    string    `json:"sem_ver" example:"1.0.0"`
}

// CreateSignalTypeHandler godoc
//
//	@Summary		Create signal type
//
//	@Description	Signal types specify a record that can be shared over the ISN
//	@Description	- Each type has a unique title and this is used to create a URL-friendly slug
//	@Description	- The title and slug fields can't be changed and must be unique for the site
//	@Description	- The signal type fields are defined in an external JSON schema file and this schema file is used to validate signals before loading
//	@Description
//	@Description	Schema URL Requirements
//	@Description	- Must be a link to a schema file on a public github repo (e.g., https://github.com/org/repo/blob/2025.01.01/schema.json)
//	@Description	- To disable schema validation, use the special URL: https://github.com/skip/validation/main/schema.json
//	@Description
//	@Description	Readme URL requirements
//	@Description	- Must be a link to a file ending .md on a public github repo.
//	@Description	- Use the special URL: https://github.com/skip/readme/main/readme.md to indicate there is no readme
//	@Description
//	@Description	Versions
//	@Description	- A signal type can have multiple versions - these share the same title/slug but have different JSON schemas
//	@Description	- Use this endpoint to create the first version - the bump_type (major/minor/patch) determines the initial semver (1.0.0, 0.1.0 or 0.0.1)
//	@Description
//	@Description	After creating a signal type, use the AddSignalTypeToIsn endpoint to link it to an ISN.
//	@Description
//	@Description	Signal type definitions are referred to like this: /api/signal-types/{signal_type_slug}/v{sem_ver}
//	@Description
//	@Description	Note: this endpoint can only be used by site admins
//
//	@Tags		Signal Type Definitions
//
//	@Param		request	body		handlers.CreateSignalTypeRequest	true	"signal type details"
//
//	@Success	201		{object}	handlers.NewSignalTypeResponse
//	@Failure	400		{object}	responses.ErrorResponse
//	@Failure	403		{object}	responses.ErrorResponse
//	@Failure	409		{object}	responses.ErrorResponse
//
//	@Security	BearerAccessToken
//
//	@Router		/api/admin/signal-types [post]
//
// Should only be used with RequireRole (siteadmin) middleware
func (s *SignalTypeHandler) CreateSignalTypeHandler(w http.ResponseWriter, r *http.Request) {
	var req CreateSignalTypeRequest
	var slug string
	var semVer string
	var err error
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	// validate fields
	if req.SchemaURL == "" ||
		req.Title == "" ||
		req.BumpType == "" ||
		req.ReadmeURL == "" ||
		req.Detail == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply all the fields: schema URL, title, version, readme URL and detail")
		return
	}

	req.SchemaURL = strings.TrimSpace(req.SchemaURL)
	req.ReadmeURL = strings.TrimSpace(req.ReadmeURL)

	// check for valid github url formats
	if err := utils.ValidateGithubFileURL(req.SchemaURL, "schema"); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("schema_url", req.SchemaURL),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid schema URL")
		return
	}
	if err := utils.ValidateGithubFileURL(req.ReadmeURL, "readme"); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("readme_url", req.ReadmeURL),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid readme URL")
		return
	}

	// Check that the readme file exists on GitHub
	if req.ReadmeURL != signalsd.SkipReadmeURL {
		if err := utils.CheckGithubFileExists(req.ReadmeURL); err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
				slog.String("readme_url", req.ReadmeURL),
			)

			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "readme file not accessible")
			return
		}
	}

	// generate slug.
	slug, err = utils.GenerateSlug(req.Title)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("title", req.Title),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "internal server error")
		return
	}

	// check if there is already a signal type with this slug (the query below returns currentSignalType.semver == "0.0.0" if there is no existing version)
	currentSignalType, err := s.queries.GetLatestSlugVersion(r.Context(), slug)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("slug", slug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "database error")
		return
	}

	//  if this slug has already been used then reject the request
	if currentSignalType.SemVer != "0.0.0" {
		responses.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, "This signal type slug is already in use")
		return
	}

	//  increment the semver using the supplied bump instruction supplied in the req
	semVer, err = utils.IncrementSemVer(req.BumpType, currentSignalType.SemVer)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("current_version", currentSignalType.SemVer),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "internal server error")
		return
	}

	// Fetch and compile the schema
	var schemaContent string
	if req.SchemaURL == signalsd.SkipValidationURL {
		// for consistency store the permissive schema in the db
		schemaContent = "{}"
	} else {
		schemaContent, err = utils.FetchFileContentFromGithub(req.SchemaURL)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
				slog.String("schema_url", req.SchemaURL),
			)

			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "could not fetch schema from GitHub")
			return
		}
	}

	_, err = schemas.ValidateAndCompileSchema(req.SchemaURL, schemaContent)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("schema_url", req.SchemaURL),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON schema")
		return
	}

	// create signal type
	var returnedSignalType database.SignalType
	returnedSignalType, err = s.queries.CreateSignalType(r.Context(), database.CreateSignalTypeParams{
		Slug:          slug,
		SemVer:        semVer,
		SchemaURL:     req.SchemaURL,
		Title:         req.Title,
		Detail:        req.Detail,
		ReadmeURL:     req.ReadmeURL,
		SchemaContent: schemaContent,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("slug", slug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	responses.RespondWithJSON(w, http.StatusCreated, NewSignalTypeResponse{
		Slug:   returnedSignalType.Slug,
		SemVer: returnedSignalType.SemVer,
	})
}

// RegisterNewSignalTypeSchemaHandler godoc
//
//	@Summary		Registe a new schema for an existing signal type
//	@Description	You must specify a schema_url that has not been previously registered for this signal type.
//	@Description
//	@Description	Use the bump_type (major/minor/patch) parameter to determine how the version number should be incremented.
//	@Description
//	@Description	Note: this endpoint can only be used by site admins
//
//	@Tags			Signal Type Definitions
//
//	@Param			signal_type_slug	path		string										true	"signal type slug"	example(sample-signal--example-org)
//	@Param			request				body		handlers.RegisterNewSignalTypeSchemaRequest	true	"signal type details"
//
//	@Success		201					{object}	handlers.NewSignalTypeResponse
//	@Failure		400					{object}	responses.ErrorResponse
//	@Failure		403					{object}	responses.ErrorResponse
//	@Failure		409					{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/signal-types/{signal_type_slug}/schemas [post]
//
// Should only be used with RequiresRole (siteadmin) middleware
func (s *SignalTypeHandler) RegisterNewSignalTypeSchemaHandler(w http.ResponseWriter, r *http.Request) {

	slug := r.PathValue("signal_type_slug")

	var req RegisterNewSignalTypeSchemaRequest

	var semVer string

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	// validate fields
	if req.SchemaURL == "" ||
		req.BumpType == "" ||
		req.ReadmeURL == "" ||
		req.Detail == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "you must supply all the fields: schema URL, slug, version, readme URL and detail")
		return
	}

	req.SchemaURL = strings.TrimSpace(req.SchemaURL)
	req.ReadmeURL = strings.TrimSpace(req.ReadmeURL)

	// check for valid github url formats
	if err := utils.ValidateGithubFileURL(req.SchemaURL, "schema"); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("schema_url", req.SchemaURL),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid schema URL")
		return
	}
	if err := utils.ValidateGithubFileURL(req.ReadmeURL, "readme"); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("readme_url", req.ReadmeURL),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid readme URL")
		return
	}

	// Check that the readme file exists on GitHub
	if req.ReadmeURL != signalsd.SkipReadmeURL {
		if err := utils.CheckGithubFileExists(req.ReadmeURL); err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
				slog.String("readme_url", req.ReadmeURL),
			)

			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "readme file not accessible")
			return
		}
	}

	//  if this is the first version then the query below returns currentSignalType.semver == "0.0.0"
	currentSignalType, err := s.queries.GetLatestSlugVersion(r.Context(), slug)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "database error")
		return
	}

	// if there is no existing schemas for this signal type, reject the request.
	if currentSignalType.SemVer == "0.0.0" {
		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "signal type not found")
		return
	}
	//... check the signal type was not previously registered with this schema
	exists, err := s.queries.ExistsSignalTypeWithSlugAndSchema(r.Context(), database.ExistsSignalTypeWithSlugAndSchemaParams{
		Slug:      slug,
		SchemaURL: req.SchemaURL,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("slug", slug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "database error")
		return
	}
	if exists {
		responses.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceAlreadyExists, "This schema has already been registered for this signal type")
		return
	}

	//  increment the semver using the supplied bump instruction supplied in the req
	semVer, err = utils.IncrementSemVer(req.BumpType, currentSignalType.SemVer)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("current_version", currentSignalType.SemVer),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "internal server error")
		return
	}

	// Fetch and compile the schema
	var schemaContent string
	if req.SchemaURL == signalsd.SkipValidationURL {
		// for consistency store the permissive schema in the db
		schemaContent = "{}"
	} else {
		schemaContent, err = utils.FetchFileContentFromGithub(req.SchemaURL)
		if err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
				slog.String("schema_url", req.SchemaURL),
			)

			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "could not fetch schema from GitHub")
			return
		}
	}

	_, err = schemas.ValidateAndCompileSchema(req.SchemaURL, schemaContent)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("schema_url", req.SchemaURL),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON schema")
		return
	}

	// create signal type
	var returnedSignalType database.SignalType
	returnedSignalType, err = s.queries.CreateSignalType(r.Context(), database.CreateSignalTypeParams{
		Slug:          slug,
		SemVer:        semVer,
		SchemaURL:     req.SchemaURL,
		Title:         currentSignalType.Title, // use the title from the existing signal type
		Detail:        req.Detail,
		ReadmeURL:     req.ReadmeURL,
		SchemaContent: schemaContent,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("slug", slug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	responses.RespondWithJSON(w, http.StatusCreated, NewSignalTypeResponse{
		Slug:   returnedSignalType.Slug,
		SemVer: returnedSignalType.SemVer,
	})
}

// UpdateSignalTypeHandler godoc
//
//	@Summary		Update signal type
//	@Description	users can mark the signal type as *in use/not in use* and update the description or link to the readme file
//	@Description	Signal types marked as 'not in use' are not returned in signal searches and can not receive new signals
//	@Description
//	@Description	Note: this endpoint can only be used by site admins
//
//	@Param			signal_type_slug	path	string								true	"signal type slug"	example(sample-signal--example-org)
//	@Param			sem_ver				path	string								true	"Sem ver"			example(0.0.1)
//	@Param			request				body	handlers.UpdateSignalTypeRequest	true	"signal type details to be updated"
//
//	@Tags			Signal Type Definitions
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/signal-types/{signal_type_slug}/v{sem_ver} [put]
//
// Should only be used with RequiresRole (siteadmin) middleware
func (s *SignalTypeHandler) UpdateSignalTypeHandler(w http.ResponseWriter, r *http.Request) {

	var req = UpdateSignalTypeRequest{}

	slug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	// check signal def exists
	signalType, err := s.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   slug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No signal type found for %s/v%s", slug, semVer))
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("signal_type_slug", slug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	//check body
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	if req.Detail == nil &&
		req.ReadmeURL == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "no updateable fields found in body of request")
		return
	}
	// prepare struct for update
	if req.ReadmeURL != nil {
		if err := utils.ValidateGithubFileURL(*req.ReadmeURL, "readme"); err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
				slog.String("readme_url", *req.ReadmeURL),
			)

			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid readme URL")
			return
		}

		// Check that the readme file exists on GitHub
		if *req.ReadmeURL != signalsd.SkipReadmeURL {
			if err := utils.CheckGithubFileExists(*req.ReadmeURL); err != nil {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("error", err.Error()),
					slog.String("readme_url", *req.ReadmeURL),
				)

				responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "readme file not accessible")
				return
			}
		}

		signalType.ReadmeURL = *req.ReadmeURL
	}

	if req.Detail != nil {
		signalType.Detail = *req.Detail
	}

	// update signal_types
	rowsAffected, err := s.queries.UpdateSignalTypeDetails(r.Context(), database.UpdateSignalTypeDetailsParams{
		ID:        signalType.ID,
		ReadmeURL: signalType.ReadmeURL,
		Detail:    signalType.Detail,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("signal_type_id", signalType.ID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}
	if rowsAffected != 1 {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error - more than one signal type deleted")
		return
	}
	responses.RespondWithStatusCodeOnly(w, http.StatusNoContent)
}

// DeleteSignalTypeHandler godoc
//
//	@Summary		Delete signal type
//	@Description	Only signal types that have never been referenced by signals can be deleted
//
//	@Param			signal_type_slug	path	string	true	"signal type slug"		example(sample-signal--example-org)
//	@Param			sem_ver				path	string	true	"version to be deleted"	example(0.0.1)
//
//	@Tags			Signal Type Definitions
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//	@Failure		409	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/signal-types/{signal_type_slug}/v{sem_ver} [delete]
//
// Should only be used with RequireRole (siteadmin) middleware
func (s *SignalTypeHandler) DeleteSignalTypeHandler(w http.ResponseWriter, r *http.Request) {

	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	// check signal type exists
	signalType, err := s.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   signalTypeSlug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No signal type found for %s/v%s", signalTypeSlug, semVer))
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("signal_type_slug", signalTypeSlug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// check if signal type is being used by any signals
	hasSignals, err := s.queries.CheckSignalTypeHasSignals(r.Context(), signalType.ID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("signal_type_id", signalType.ID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if hasSignals {
		responses.RespondWithError(w, r, http.StatusConflict, apperrors.ErrCodeResourceInUse, fmt.Sprintf("Cannot delete signal type %s/v%s: it is being used by existing signals", signalTypeSlug, semVer))
		return
	}

	// delete the signal type
	rowsAffected, err := s.queries.DeleteSignalType(r.Context(), signalType.ID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("signal_type_id", signalType.ID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if rowsAffected == 0 {
		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("Signal type %s/v%s not found", signalTypeSlug, semVer))
		return
	}

	responses.RespondWithStatusCodeOnly(w, http.StatusNoContent)
}

// GetSignalTypeHandler godoc
//
//	@Summary		Get Signal Type
//	@Description	Returns the signal type details.
//	@Description	This endpoint can be used by anyone registered with the site
//
//	@Tags			Signal Type Definitions
//
//	@Param			signal_type_slug	path	string	true	"signal type slug"			example(sample-signal--example-org)
//	@Param			sem_ver				path	string	true	"version to be recieved"	example(0.0.1)
//
//	@Tags			Signal Type Definitions
//
//	@Success		200	{object}	handlers.SignalTypeDetail
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//	@Failure		500	{object}	responses.ErrorResponse
//
//	@Router			/api/admin/signal-types/{signal_type_slug}/v{sem_ver} [get]
func (s *SignalTypeHandler) GetSignalTypeHandler(w http.ResponseWriter, r *http.Request) {

	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	// check signal def exists
	dbSignalType, err := s.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   signalTypeSlug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("No signal type found for %s/v%s", signalTypeSlug, semVer))
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("signal_type_slug", signalTypeSlug),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	schemaURL := dbSignalType.SchemaURL
	if schemaURL == signalsd.SkipValidationURL {
		schemaURL = ""
	}
	readmeURL := dbSignalType.ReadmeURL
	if readmeURL == signalsd.SkipReadmeURL {
		readmeURL = ""
	}

	// Convert database structs to our response structs
	signalType := SignalTypeDetail{
		ID:        dbSignalType.ID,
		CreatedAt: dbSignalType.CreatedAt,
		UpdatedAt: dbSignalType.UpdatedAt,
		Slug:      dbSignalType.Slug,
		SchemaURL: schemaURL,
		ReadmeURL: readmeURL,
		Title:     dbSignalType.Title,
		Detail:    dbSignalType.Detail,
		SemVer:    dbSignalType.SemVer,
	}
	responses.RespondWithJSON(w, http.StatusOK, signalType)
}

// GetSignalTypesHandler godoc
//
//	@Summary		Get Signal Types
//	@Description	Get details for all the signal types defined on the ISN.
//	@Description	This endpoint can only be used by any account registered with the site
//
//	@Tags			Signal Type Definitions
//
//	@Success		200	{array}	handlers.SignalTypeDetail
//
//	@Router			/api/admin/signal-types [get]
func (s *SignalTypeHandler) GetSignalTypesHandler(w http.ResponseWriter, r *http.Request) {

	var dbSignalTypes []database.SignalType
	var err error

	dbSignalTypes, err = s.queries.GetSignalTypes(r.Context())
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	signalTypes := make([]SignalTypeDetail, len(dbSignalTypes))
	for i, dbSignalType := range dbSignalTypes {

		schemaURL := dbSignalType.SchemaURL
		if schemaURL == signalsd.SkipValidationURL {
			schemaURL = ""
		}
		readmeURL := dbSignalType.ReadmeURL
		if readmeURL == signalsd.SkipReadmeURL {
			readmeURL = ""
		}
		signalTypes[i] = SignalTypeDetail{
			ID:        dbSignalType.ID,
			CreatedAt: dbSignalType.CreatedAt,
			UpdatedAt: dbSignalType.UpdatedAt,
			Slug:      dbSignalType.Slug,
			SchemaURL: schemaURL,
			ReadmeURL: readmeURL,
			Title:     dbSignalType.Title,
			Detail:    dbSignalType.Detail,
			SemVer:    dbSignalType.SemVer,
		}
	}

	responses.RespondWithJSON(w, http.StatusOK, signalTypes)
}

// AddSignalTypeToISNHandler godoc
//
//	@Summary		Add signal type to an ISN
//	@Description	Link an existing signal type to an ISN
//	@Description
//	@Description	Note: this endpoint can only be used by site admins and ISN admins.
//	@Description	ISN admins can only add signal types to ISNs they own.
//
//	@Tags			Signal Type Definitions
//
//	@Param			isn_slug	path	string										true	"ISN slug"	example(sample-isn--example-org)
//	@Param			request		body	handlers.AddSignalTypeToIsnRequest	true	"signal type details"
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal-types/add [post]
//
// Should only be used with RequireRole (siteadmin, isnadmin) middleware
func (s *SignalTypeHandler) AddSignalTypeToISNHandler(w http.ResponseWriter, r *http.Request) {
	var req AddSignalTypeToIsnRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	// Validate required fields
	if req.SignalTypeSlug == "" || req.SemVer == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "signal_type_slug and sem_ver are required")
		return
	}

	isnSlug := r.PathValue("isn_slug")

	// Get ISN by slug
	isn, err := s.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("isn_slug", isnSlug),
		)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// If the requester is an ISN admin, they must own this ISN.
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	if claims.Role == "isnadmin" && isn.UserAccountID != userAccountID {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you must be the ISN owner to add signal types")
		return
	}
	// Get signal type by slug and version (globally)
	signalType, err := s.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   req.SignalTypeSlug,
		SemVer: req.SemVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("Signal type %s/v%s not found", req.SignalTypeSlug, req.SemVer))
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("signal_type_slug", req.SignalTypeSlug),
			slog.String("sem_ver", req.SemVer),
		)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// add signal type to ISN
	err = s.queries.AddSignalTypeToIsn(r.Context(), database.AddSignalTypeToIsnParams{
		IsnID:        isn.ID,
		SignalTypeID: signalType.ID,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("isn_id", isn.ID.String()),
			slog.String("signal_type_id", signalType.ID.String()),
		)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// UpdateIsnSignalTypeStatusHandler godoc
//
//	@Summary		Update ISN signal type status
//	@Description	Enable or disable a signal type for a specific ISN
//	@Description
//	@Description	When a signal type is disabled for an ISN, signals of this type can no longer be read or written to the ISN.
//	@Description
//	@Description	Note: this endpoint can only be used by site admins and ISN admins.
//	@Description	ISN admins can only update signal types for ISNs they own.
//
//	@Param			isn_slug			path	string								true	"ISN slug"			example(sample-isn--example-org)
//	@Param			signal_type_slug	path	string								true	"signal type slug"	example(sample-signal--example-org)
//	@Param			sem_ver				path	string								true	"Sem ver"			example(0.0.1)
//	@Param			request				body	handlers.UpdateSignalTypeRequest	true	"status update request"
//
//	@Tags			Signal Type Definitions
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		401	{object}	responses.ErrorResponse
//	@Failure		403	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver} [put]
//
// Should only be used with RequireRole (isnadmin,siteadmin) middleware
func (s *SignalTypeHandler) UpdateIsnSignalTypeStatusHandler(w http.ResponseWriter, r *http.Request) {
	var req = UpdateSignalTypeRequest{}

	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	// Get ISN by slug
	isn, err := s.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "ISN not found")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("isn_slug", isnSlug),
		)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// If the requester is an ISN admin, they must own this ISN.
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "could not get claims from context")
		return
	}

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "did not receive userAccountID from middleware")
		return
	}

	if claims.Role == "isnadmin" && isn.UserAccountID != userAccountID {
		responses.RespondWithError(w, r, http.StatusForbidden, apperrors.ErrCodeForbidden, "you must be the ISN owner to update signal types")
		return
	}

	// Get signal type by slug and version
	signalType, err := s.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   signalTypeSlug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "Signal type not found")
			return
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("signal_type_slug", signalTypeSlug),
			slog.String("sem_ver", semVer),
		)
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Check body
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	// Validate that is_in_use is present
	if req.IsInUse == nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "is_in_use field is required")
		return
	}

	// Update isn_signal_types
	rowsAffected, err := s.queries.UpdateIsnSignalTypeStatus(r.Context(), database.UpdateIsnSignalTypeStatusParams{
		IsnID:        isn.ID,
		SignalTypeID: signalType.ID,
		IsInUse:      *req.IsInUse,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
			slog.String("isn_id", isn.ID.String()),
			slog.String("signal_type_id", signalType.ID.String()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	if rowsAffected == 0 {
		responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, "Signal type not available on this ISN")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
