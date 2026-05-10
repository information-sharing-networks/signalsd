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
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/utils"
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
	Slug   string `json:"slug" example:"sample-signal"`
	SemVer string `json:"sem_ver" example:"0.0.1"`
}

type AddSignalTypeToIsnRequest struct {
	SignalTypeSlug string `json:"signal_type_slug" example:"sample-signal"` // signal type slug
	SemVer         string `json:"sem_ver" example:"0.0.1"`                  // signal type version
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
	SemVer    string    `json:"sem_ver" example:""`
}

// CreateSignalType godoc
//
//	@Summary		Create Signal Type
//
//	@Description	Signal types specify a record that can be shared on the site
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
//	@Description	- Use this endpoint to create the first version - the bump_type (major/minor/patch) determines the initial semver (, 0.1.0 or 0.0.1)
//	@Description
//	@Description	After creating a signal type, use the AddSignalTypeToIsn endpoint to link it to one or more ISNs.
//	@Description
//	@Description	Signal type definitions are referred to like this: /api/signal-types/{signal_type_slug}/v{sem_ver}
//	@Description
//	@Description	Note: this endpoint can only be used by site admins
//
//	@Tags			Signal Types
//
//	@Param			request	body		handlers.CreateSignalTypeRequest	true	"signal type details"
//
//	@Success		201		{object}	handlers.NewSignalTypeResponse
//	@Failure		400		{object}	responses.ErrorResponse
//	@Failure		403		{object}	responses.ErrorResponse
//	@Failure		409		{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/signal-types [post]
//
// Should only be used with RequireRole (siteadmin) middleware
func (s *SignalTypeHandler) CreateSignalType(w http.ResponseWriter, r *http.Request) error {
	var req CreateSignalTypeRequest
	var slug string
	var semVer string
	var err error
	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return apperrors.MalformedBody("invalid JSON body", err)
	}

	// validate fields
	if req.SchemaURL == "" ||
		req.Title == "" ||
		req.BumpType == "" ||
		req.ReadmeURL == "" ||
		req.Detail == "" {
		return apperrors.MalformedBody("you must supply all the fields: schema URL, title, version, readme URL and detail", nil)
	}

	req.SchemaURL = strings.TrimSpace(req.SchemaURL)
	req.ReadmeURL = strings.TrimSpace(req.ReadmeURL)

	// check for valid github url formats
	if err := utils.ValidateGithubFileURL(req.SchemaURL, "schema"); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("schema_url", req.SchemaURL),
		)

		return apperrors.MalformedBody("invalid schema URL", err)
	}
	if err := utils.ValidateGithubFileURL(req.ReadmeURL, "readme"); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("readme_url", req.ReadmeURL),
		)

		return apperrors.MalformedBody("invalid readme URL", err)
	}

	// Check that the readme file exists on GitHub
	if req.ReadmeURL != signalsd.SkipReadmeURL {
		if err := utils.CheckGithubFileExists(req.ReadmeURL); err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("readme_url", req.ReadmeURL),
			)

			return apperrors.MalformedBody("readme file not accessible", err)
		}
	}

	// generate slug.
	slug, err = utils.GenerateSlug(req.Title)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("title", req.Title),
		)

		return apperrors.InternalError("internal server error", err)
	}

	// check if there is already a signal type with this slug (the query below returns currentSignalType.semver == "0.0.0" if there is no existing version)
	currentSignalType, err := s.queries.GetLatestSlugVersion(r.Context(), slug)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("slug", slug),
		)

		return apperrors.InternalError("database error", err)
	}

	//  if this slug has already been used then reject the request
	if currentSignalType.SemVer != "0.0.0" {
		return apperrors.AlreadyExists("This signal type slug is already in use", nil)
	}

	//  increment the semver using the supplied bump instruction supplied in the req
	semVer, err = utils.IncrementSemVer(req.BumpType, currentSignalType.SemVer)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("current_version", currentSignalType.SemVer),
		)

		return apperrors.InternalError("internal server error", err)
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
				slog.String("schema_url", req.SchemaURL),
			)

			return apperrors.MalformedBody("could not fetch schema from GitHub", err)
		}
	}

	_, err = schemas.ValidateAndCompileSchema(req.SchemaURL, schemaContent)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("schema_url", req.SchemaURL),
		)

		return apperrors.MalformedBody("invalid JSON schema", err)
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
			slog.String("slug", slug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	return responses.JSON(w, http.StatusCreated, NewSignalTypeResponse{
		Slug:   returnedSignalType.Slug,
		SemVer: returnedSignalType.SemVer,
	})
}

// RegisterNewSignalTypeSchema godoc
//
//	@Summary		Register a New Schema
//	@Description	Registers a new schema for an existing signal type
//	@Description
//	@Description	You must specify a schema_url that has not been previously registered for this signal type.
//	@Description
//	@Description	Use the bump_type (major/minor/patch) parameter to determine how the version number should be incremented.
//	@Description
//	@Description	Note: this endpoint can only be used by site admins
//
//	@Tags			Signal Types
//
//	@Param			signal_type_slug	path		string										true	"signal type slug"	example(sample-signal-type)
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
func (s *SignalTypeHandler) RegisterNewSignalTypeSchema(w http.ResponseWriter, r *http.Request) error {

	slug := r.PathValue("signal_type_slug")

	var req RegisterNewSignalTypeSchemaRequest

	var semVer string

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return apperrors.MalformedBody("invalid JSON body", err)
	}

	// validate fields
	if req.SchemaURL == "" ||
		req.BumpType == "" ||
		req.ReadmeURL == "" ||
		req.Detail == "" {
		return apperrors.MalformedBody("you must supply all the fields: schema URL, slug, version, readme URL and detail", nil)
	}

	req.SchemaURL = strings.TrimSpace(req.SchemaURL)
	req.ReadmeURL = strings.TrimSpace(req.ReadmeURL)

	// check for valid github url formats
	if err := utils.ValidateGithubFileURL(req.SchemaURL, "schema"); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("schema_url", req.SchemaURL),
		)

		return apperrors.MalformedBody("invalid schema URL", err)
	}
	if err := utils.ValidateGithubFileURL(req.ReadmeURL, "readme"); err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("readme_url", req.ReadmeURL),
		)

		return apperrors.MalformedBody("invalid readme URL", err)
	}

	// Check that the readme file exists on GitHub
	if req.ReadmeURL != signalsd.SkipReadmeURL {
		if err := utils.CheckGithubFileExists(req.ReadmeURL); err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("readme_url", req.ReadmeURL),
			)

			return apperrors.MalformedBody("readme file not accessible", err)
		}
	}

	//  if this is the first version then the query below returns currentSignalType.semver == "0.0.0"
	currentSignalType, err := s.queries.GetLatestSlugVersion(r.Context(), slug)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return apperrors.InternalError("database error", err)
	}

	// if there is no existing schemas for this signal type, reject the request.
	if currentSignalType.SemVer == "0.0.0" {
		return apperrors.NotFound("signal type not found", nil)
	}
	//... check the signal type was not previously registered with this schema
	exists, err := s.queries.ExistsSignalTypeWithSlugAndSchema(r.Context(), database.ExistsSignalTypeWithSlugAndSchemaParams{
		Slug:      slug,
		SchemaURL: req.SchemaURL,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("slug", slug),
		)

		return apperrors.InternalError("database error", err)
	}
	if exists {
		return apperrors.AlreadyExists("This schema has already been registered for this signal type", nil)
	}

	//  increment the semver using the supplied bump instruction supplied in the req
	semVer, err = utils.IncrementSemVer(req.BumpType, currentSignalType.SemVer)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("current_version", currentSignalType.SemVer),
		)

		return apperrors.InternalError("internal server error", err)
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
				slog.String("schema_url", req.SchemaURL),
			)

			return apperrors.MalformedBody("could not fetch schema from GitHub", err)
		}
	}

	_, err = schemas.ValidateAndCompileSchema(req.SchemaURL, schemaContent)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("schema_url", req.SchemaURL),
		)

		return apperrors.MalformedBody("invalid JSON schema", err)
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
			slog.String("slug", slug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	return responses.JSON(w, http.StatusCreated, NewSignalTypeResponse{
		Slug:   returnedSignalType.Slug,
		SemVer: returnedSignalType.SemVer,
	})
}

// UpdateSignalType godoc
//
//	@Summary		Update a Signal Type
//	@Description	Update the description or link to the readme file
//	@Description
//	@Description	Note: this endpoint can only be used by site admins
//
//	@Param			signal_type_slug	path	string								true	"signal type slug"	example(sample-signal-type)
//	@Param			sem_ver				path	string								true	"version"			example(1.0.0)
//	@Param			request				body	handlers.UpdateSignalTypeRequest	true	"signal type details to be updated"
//
//	@Tags			Signal Types
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
func (s *SignalTypeHandler) UpdateSignalType(w http.ResponseWriter, r *http.Request) error {

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
			return apperrors.NotFound(fmt.Sprintf("No signal type found for %s/v%s", slug, semVer), nil)
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type_slug", slug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	//check body
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		return apperrors.MalformedBody("invalid JSON body", err)
	}

	if req.Detail == nil &&
		req.ReadmeURL == nil {
		return apperrors.MalformedBody("no updateable fields found in body of request", nil)
	}
	// prepare struct for update
	if req.ReadmeURL != nil {
		if err := utils.ValidateGithubFileURL(*req.ReadmeURL, "readme"); err != nil {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("readme_url", *req.ReadmeURL),
			)

			return apperrors.MalformedBody("invalid readme URL", err)
		}

		// Check that the readme file exists on GitHub
		if *req.ReadmeURL != signalsd.SkipReadmeURL {
			if err := utils.CheckGithubFileExists(*req.ReadmeURL); err != nil {
				logger.ContextWithLogAttrs(r.Context(),
					slog.String("readme_url", *req.ReadmeURL),
				)

				return apperrors.MalformedBody("readme file not accessible", err)
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
			slog.String("signal_type_id", signalType.ID.String()),
		)

		return apperrors.DatabaseError("database error", err)
	}
	if rowsAffected != 1 {
		return apperrors.DatabaseError("database error - more than one signal type deleted", nil)
	}
	return responses.NoContent(w, http.StatusNoContent)
}

// DeleteSignalType godoc
//
//	@Summary		Delete a Signal Type
//	@Description	Only signal types that have never been referenced by signals can be deleted
//
//	@Param			signal_type_slug	path	string	true	"signal type slug"	example(sample-signal-type)
//	@Param			sem_ver				path	string	true	"version"			example(1.0.0)
//
//	@Tags			Signal Types
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
func (s *SignalTypeHandler) DeleteSignalType(w http.ResponseWriter, r *http.Request) error {

	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	// check signal type exists
	signalType, err := s.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   signalTypeSlug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound(fmt.Sprintf("No signal type found for %s/v%s", signalTypeSlug, semVer), nil)
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type_slug", signalTypeSlug),
		)

		return apperrors.DatabaseError("database error", err)
	}

	// check if signal type is being used by any signals
	hasSignals, err := s.queries.CheckSignalTypeHasSignals(r.Context(), signalType.ID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type_id", signalType.ID.String()),
		)

		return apperrors.DatabaseError("database error", err)
	}

	if hasSignals {
		return &apperrors.HTTPError{
			Status:  http.StatusConflict,
			Code:    apperrors.ErrCodeResourceInUse,
			Message: fmt.Sprintf("Cannot delete signal type %s/v%s: it is being used by existing signals", signalTypeSlug, semVer),
		}
	}

	// delete the signal type
	rowsAffected, err := s.queries.DeleteSignalType(r.Context(), signalType.ID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type_id", signalType.ID.String()),
		)

		return apperrors.DatabaseError("database error", err)
	}

	if rowsAffected == 0 {
		return apperrors.NotFound(fmt.Sprintf("Signal type %s/v%s not found", signalTypeSlug, semVer), nil)
	}

	return responses.NoContent(w, http.StatusNoContent)
}

// GetSignalType godoc
//
//	@Summary		Get a Signal Type
//	@Description	Returns the signal type details.
//	@Description	This endpoint can be used by anyone registered with the site
//
//	@Tags			Signal Types
//
//	@Param			signal_type_slug	path		string	true	"signal type slug"	example(sample-signal-type)
//	@Param			sem_ver				path		string	true	"version"			example(1.0.0)
//
//	@Success		200					{object}	handlers.SignalTypeDetail
//	@Failure		400					{object}	responses.ErrorResponse
//	@Failure		404					{object}	responses.ErrorResponse
//	@Failure		500					{object}	responses.ErrorResponse
//
//	@Router			/api/admin/signal-types/{signal_type_slug}/v{sem_ver} [get]
func (s *SignalTypeHandler) GetSignalType(w http.ResponseWriter, r *http.Request) error {

	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	// check signal def exists
	dbSignalType, err := s.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   signalTypeSlug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound(fmt.Sprintf("No signal type found for %s/v%s", signalTypeSlug, semVer), nil)
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type_slug", signalTypeSlug),
		)

		return apperrors.DatabaseError("database error", err)
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
	return responses.JSON(w, http.StatusOK, signalType)
}

// GetSignalTypes godoc
//
//	@Summary		Get Signal Types
//	@Description	Get details for all the signal types defined on the ISN.
//	@Description	This endpoint can only be used by any account registered with the site
//
//	@Tags			Signal Types
//
//	@Success		200	{array}	handlers.SignalTypeDetail
//
//	@Router			/api/admin/signal-types [get]
func (s *SignalTypeHandler) GetSignalTypes(w http.ResponseWriter, r *http.Request) error {

	var dbSignalTypes []database.SignalType
	var err error

	dbSignalTypes, err = s.queries.GetSignalTypes(r.Context())
	if err != nil {
		return apperrors.DatabaseError("database error", err)
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

	return responses.JSON(w, http.StatusOK, signalTypes)
}

// AddSignalTypeToISN godoc
//
//	@Summary		Add a Signal Type to an ISN
//	@Description	Link an existing signal type to an ISN
//	@Description
//	@Description	Note: this endpoint can only be used by site admins and ISN admins.
//	@Description	ISN admins can only add signal types to ISNs they own.
//
//	@Tags			ISN Configuration
//
//	@Param			isn_slug	path	string								true	"ISN slug"	example(sample-isn)
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
func (s *SignalTypeHandler) AddSignalTypeToISN(w http.ResponseWriter, r *http.Request) error {
	var req AddSignalTypeToIsnRequest

	defer r.Body.Close()

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return apperrors.MalformedBody("invalid JSON body", err)
	}

	// Validate required fields
	if req.SignalTypeSlug == "" || req.SemVer == "" {
		return apperrors.MalformedBody("signal_type_slug and sem_ver are required", nil)
	}

	isnSlug := r.PathValue("isn_slug")

	// Get ISN by slug
	isn, err := s.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound("ISN not found", nil)
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("isn_slug", isnSlug),
		)
		return apperrors.DatabaseError("database error", err)
	}

	// If the requester is an ISN admin, they must own this ISN.
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		return apperrors.InternalError("could not get claims from context", nil)
	}

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		return apperrors.InternalError("did not receive userAccountID from middleware", nil)
	}

	if claims.Role == "isnadmin" && isn.UserAccountID != userAccountID {
		return apperrors.Forbidden("you must be the ISN owner to add signal types", nil)
	}
	// Get signal type by slug and version (globally)
	signalType, err := s.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   req.SignalTypeSlug,
		SemVer: req.SemVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound(fmt.Sprintf("Signal type %s/v%s not found", req.SignalTypeSlug, req.SemVer), nil)
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type_slug", req.SignalTypeSlug),
			slog.String("sem_ver", req.SemVer),
		)
		return apperrors.DatabaseError("database error", err)
	}

	// add signal type to ISN
	err = s.queries.AddSignalTypeToIsn(r.Context(), database.AddSignalTypeToIsnParams{
		IsnID:        isn.ID,
		SignalTypeID: signalType.ID,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("isn_id", isn.ID.String()),
			slog.String("signal_type_id", signalType.ID.String()),
		)
		return apperrors.DatabaseError("database error", err)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// UpdateIsnSignalTypeStatus godoc
//
//	@Summary		Update ISN Signal Type Status
//	@Description	Enable or disable a signal type for a specific ISN
//	@Description
//	@Description	When a signal type is disabled for an ISN, signals of this type can no longer be read or written to the ISN.
//	@Description
//	@Description	Note: this endpoint can only be used by site admins and ISN admins.
//	@Description	ISN admins can only update signal types for ISNs they own.
//
//	@Param			isn_slug			path	string								true	"ISN slug"			example(sample-isn)
//	@Param			signal_type_slug	path	string								true	"signal type slug"	example(sample-signal-type)
//	@Param			sem_ver				path	string								true	"version"			example(1.0.0)
//	@Param			request				body	handlers.UpdateSignalTypeRequest	true	"status update request"
//
//	@Tags			ISN Configuration
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
func (s *SignalTypeHandler) UpdateIsnSignalTypeStatus(w http.ResponseWriter, r *http.Request) error {
	var req = UpdateSignalTypeRequest{}

	isnSlug := r.PathValue("isn_slug")
	signalTypeSlug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	// Get ISN by slug
	isn, err := s.queries.GetIsnBySlug(r.Context(), isnSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound("ISN not found", nil)
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("isn_slug", isnSlug),
		)
		return apperrors.DatabaseError("database error", err)
	}

	// If the requester is an ISN admin, they must own this ISN.
	claims, ok := auth.ContextClaims(r.Context())
	if !ok {
		return apperrors.InternalError("could not get claims from context", nil)
	}

	userAccountID, ok := auth.ContextAccountID(r.Context())
	if !ok {
		return apperrors.InternalError("did not receive userAccountID from middleware", nil)
	}

	if claims.Role == "isnadmin" && isn.UserAccountID != userAccountID {
		return apperrors.Forbidden("you must be the ISN owner to update signal types", nil)
	}

	// Get signal type by slug and version
	signalType, err := s.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   signalTypeSlug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperrors.NotFound("Signal type not found", nil)
		}
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("signal_type_slug", signalTypeSlug),
			slog.String("sem_ver", semVer),
		)
		return apperrors.DatabaseError("database error", err)
	}

	// Check body
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&req)
	if err != nil {
		return apperrors.MalformedBody("invalid JSON body", err)
	}

	// Validate that is_in_use is present
	if req.IsInUse == nil {
		return apperrors.MalformedBody("is_in_use field is required", nil)
	}

	// Update isn_signal_types
	rowsAffected, err := s.queries.UpdateIsnSignalTypeStatus(r.Context(), database.UpdateIsnSignalTypeStatusParams{
		IsnID:        isn.ID,
		SignalTypeID: signalType.ID,
		IsInUse:      *req.IsInUse,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("isn_id", isn.ID.String()),
			slog.String("signal_type_id", signalType.ID.String()),
		)

		return apperrors.DatabaseError("database error", err)
	}

	if rowsAffected == 0 {
		return apperrors.NotFound("Signal type not available on this ISN", nil)
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
