package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// CreateSignalTypePage godoc
//
//	@Summary		Create signal type page
//	@Description	Renders the create signal type form. Requires siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/signal-types/create [get]
func (s *Server) CreateSignalTypePage(w http.ResponseWriter, r *http.Request) {
	templ.Handler(templates.CreateSignalTypePage(s.config.Environment)).ServeHTTP(w, r)
}

// RegisterNewSignalTypeSchemaPage godoc
//
//	@Summary		Register new signal type schema page
//	@Description	Renders the form to register a new schema version for an existing signal type. Requires siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/signal-types/register-new-schema [get]
func (s *Server) RegisterNewSignalTypeSchemaPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Get all signal types
	signalTypes, err := s.apiClient.GetSignalTypes(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("Failed to load signal types. Please try again.")).ServeHTTP(w, r)
		return
	}

	// Deduplicate by slug to show each signal type once
	signalTypeSlugs := make([]types.SignalTypeSlug, 0, len(signalTypes))
	seen := make(map[string]bool)

	for _, signalType := range signalTypes {
		if !seen[signalType.Slug] {
			seen[signalType.Slug] = true
			signalTypeSlugs = append(signalTypeSlugs, types.SignalTypeSlug{
				Slug: signalType.Slug,
			})
		}
	}

	templ.Handler(templates.RegisterNewSignalTypeSchemaPage(s.config.Environment, signalTypeSlugs)).ServeHTTP(w, r)
}

// CreateSignalType godoc
//
//	@Summary		Create signal type
//	@Description	HTMX endpoint. Creates a new signal type at version . Requires siteadmin role.
//	@Tags			HTMX Actions
//	@Param			title			formData	string	true	"Signal type title"
//	@Param			detail			formData	string	true	"Description"
//	@Param			schema-url		formData	string	true	"JSON Schema URL (ignored when skip-validation is true)"
//	@Param			readme-url		formData	string	true	"Readme URL (ignored when skip-readme is true)"
//	@Param			skip-validation	formData	string	false	"'true' to skip schema validation"
//	@Param			skip-readme		formData	string	false	"'true' to skip readme requirement"
//	@Success		200				"HTML partial"
//	@Failure		400				"HTML error partial"
//	@Failure		401				"HTML error partial"
//	@Router			/ui-api/signal-types/create [post]
func (s *Server) CreateSignalType(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	title := r.FormValue("title")
	schemaURL := r.FormValue("schema-url")
	readmeURL := r.FormValue("readme-url")
	detail := r.FormValue("detail")
	skipValidation := r.FormValue("skip-validation") == "true"
	skipReadme := r.FormValue("skip-readme") == "true"

	if skipValidation {
		schemaURL = signalsd.SkipValidationURL
	}
	if skipReadme {
		readmeURL = signalsd.SkipReadmeURL
	}

	// Validate required fields
	if title == "" || schemaURL == "" || readmeURL == "" || detail == "" {
		templ.Handler(templates.ErrorAlert("Please fill in all required fields.")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	// Prepare request - create signal type with initial version
	createReq := client.CreateSignalTypeRequest{
		SchemaURL: schemaURL,
		Title:     title,
		ReadmeURL: readmeURL,
		Detail:    detail,
		BumpType:  "major", // Initial version is always
	}

	// Call the API to create the signal type
	response, err := s.apiClient.CreateSignalType(accessTokenDetails.AccessToken, createReq)
	if err != nil {
		reqLogger.Error("Failed to create signal type", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.SignalTypeCreationSuccess(*response)).ServeHTTP(w, r)
}

// RegisterNewSignalTypeSchema godoc
//
//	@Summary		Register new signal type schema version
//	@Description	HTMX endpoint. Registers a new schema version for an existing signal type. Requires siteadmin role.
//	@Tags			HTMX Actions
//	@Param			signal-type-slug	formData	string	true	"Signal type slug"
//	@Param			schema-url			formData	string	true	"JSON Schema URL"
//	@Param			readme-url			formData	string	true	"Readme URL"
//	@Param			detail				formData	string	true	"Description of changes"
//	@Param			bump-type			formData	string	true	"'major', 'minor', or 'patch'"
//	@Success		200					"HTML partial"
//	@Failure		400					"HTML error partial"
//	@Failure		401					"HTML error partial"
//	@Router			/ui-api/signal-types/register-new-schema [put]
func (s *Server) RegisterNewSignalTypeSchema(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	slug := r.FormValue("signal-type-slug")
	schemaURL := r.FormValue("schema-url")
	bumpType := r.FormValue("bump-type")
	readmeURL := r.FormValue("readme-url")
	detail := r.FormValue("detail")

	if slug == "" || schemaURL == "" || bumpType == "" || readmeURL == "" || detail == "" {
		templ.Handler(templates.ErrorAlert("Please fill in all required fields.")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	// Prepare request - create new version of signal type globally
	createReq := client.RegisterNewSignalTypeSchemaRequest{
		SchemaURL: schemaURL,
		Slug:      slug,
		BumpType:  bumpType,
		ReadmeURL: readmeURL,
		Detail:    detail,
	}

	// Call the API to create the new version
	response, err := s.apiClient.RegisterNewSignalTypeSchema(accessTokenDetails.AccessToken, createReq)
	if err != nil {
		reqLogger.Error("Failed to register new schema for signal type", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.SignalTypeCreationSuccess(*response)).ServeHTTP(w, r)
}

// AddSignalTypeToIsn godoc
//
//	@Summary		Add signal type to ISN
//	@Description	HTMX endpoint. Associates a signal type version with an ISN. Requires isnadmin role for the target ISN.
//	@Tags			HTMX Actions
//	@Param			isn-slug			formData	string	true	"ISN slug"
//	@Param			signal-type-slug	formData	string	true	"Signal type slug"
//	@Param			sem-ver				formData	string	true	"Semantic version"
//	@Success		200					"HTML partial"
//	@Failure		400					"HTML error partial"
//	@Failure		401					"HTML error partial"
//	@Router			/ui-api/isn/signal-types/add [post]
func (s *Server) AddSignalTypeToIsn(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	signalTypeSlug := r.FormValue("signal-type-slug")
	semVer := r.FormValue("sem-ver")

	// Validate required fields
	if isnSlug == "" || signalTypeSlug == "" || semVer == "" {
		templ.Handler(templates.ErrorAlert("Please fill in all required fields.")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	// Prepare request
	associateReq := client.AddSignalTypeToIsnRequest{
		SignalTypeSlug: signalTypeSlug,
		SemVer:         semVer,
	}

	// Call the API to add the signal type to the ISN
	err := s.apiClient.AddSignalTypeToIsn(accessTokenDetails.AccessToken, isnSlug, associateReq)
	if err != nil {
		reqLogger.Error("Failed to add signal type to the ISN", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.SuccessAlert(fmt.Sprintf("Signal type %s/v%s associated with ISN successfully", signalTypeSlug, semVer))).ServeHTTP(w, r)
}

// AddSignalTypeToIsnPage godoc
//
//	@Summary		Add signal type to ISN page
//	@Description	Renders the form to associate a signal type version with an ISN. Requires isnadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/isn/signal-types/add [get]
func (s *Server) AddSignalTypeToIsnPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	isnPerms := accessTokenDetails.IsnPerms

	if len(isnPerms) == 0 {
		reqLogger.Error("user does not have permission to access any ISNs")
		return
	}
	// populate the isn dropdown list with ISNs where the user is an admin
	isns := getIsnOptions(isnPerms, true, false)

	templ.Handler(templates.AddSignalTypeToIsnPage(s.config.Environment, isns)).ServeHTTP(w, r)
}

// ManageIsnSignalTypesStatusPage godoc
//
//	@Summary		Manage ISN signal type status page
//	@Description	Renders the form to enable or disable signal types within an ISN. Requires isnadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/isn/signal-types/manage [get]
func (s *Server) ManageIsnSignalTypesStatusPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	isnPerms := accessTokenDetails.IsnPerms

	if len(isnPerms) == 0 {
		templ.Handler(templates.ErrorAlert("You don't have permission to access any ISNs.")).ServeHTTP(w, r)
		return
	}

	// Convert permissions to ISN list for dropdown (only ISNs where user has admin rights)
	isns := getIsnOptions(isnPerms, true, false)

	templ.Handler(templates.ManageIsnSignalTypesStatusPage(s.config.Environment, isns)).ServeHTTP(w, r)
}

// ManageIsnSignalTypesStatus godoc
//
//	@Summary		Enable or disable a signal type for an ISN
//	@Description	HTMX endpoint. Enables or disables a specific signal type version within an ISN. Requires isnadmin role.
//	@Tags			HTMX Actions
//	@Param			isn-slug			formData	string	true	"ISN slug"
//	@Param			signal-type-slug	formData	string	true	"Signal type slug"
//	@Param			sem-ver				formData	string	true	"Semantic version"
//	@Param			action				formData	string	true	"'enable' or 'disable'"
//	@Success		200					"HTML partial"
//	@Failure		400					"HTML error partial"
//	@Failure		401					"HTML error partial"
//	@Router			/ui-api/isn/signal-types/manage [put]
func (s *Server) ManageIsnSignalTypesStatus(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	signalTypeSlug := r.FormValue("signal-type-slug")
	semVer := r.FormValue("sem-ver")
	action := r.FormValue("action")

	// Validate required fields
	if isnSlug == "" || signalTypeSlug == "" || semVer == "" || action == "" {
		templ.Handler(templates.ErrorAlert("Please fill in all fields.")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to read access token from context", slog.String("component", "handlers.AdminIsnSignalTypeStatus"))
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	// Determine the new status based on action
	var isInUse bool
	var successMsg string

	switch action {
	case "disable":
		isInUse = false
		successMsg = fmt.Sprintf("Signal type %s/v%s disabled for this ISN successfully", signalTypeSlug, semVer)
	case "enable":
		isInUse = true
		successMsg = fmt.Sprintf("Signal type %s/v%s enabled for this ISN successfully", signalTypeSlug, semVer)
	default:
		templ.Handler(templates.ErrorAlert("Invalid action. Please select a valid action.")).ServeHTTP(w, r)
		return
	}

	// Call the API to update ISN signal type status
	err := s.apiClient.UpdateIsnSignalTypeStatus(accessTokenDetails.AccessToken, isnSlug, signalTypeSlug, semVer, isInUse)
	if err != nil {
		reqLogger.Error("Failed to update ISN signal type status", slog.String("error", err.Error()))

		var msg string
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.SuccessAlert(successMsg)).ServeHTTP(w, r)
}

// ListSignalTypesPage godoc
//
//	@Summary		List signal types page
//	@Description	Renders a read-only report of all registered signal types. Requires isnadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/signal-types/list [get]
func (s *Server) ListSignalTypesPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Fetch all signal types for the report
	signalTypes, err := s.apiClient.GetSignalTypes(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("Failed to load signal types. Please try again.")).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.ListSignalTypesPage(s.config.Environment, signalTypes)).ServeHTTP(w, r)
}

// ToggleSkipValidation godoc
//
//	@Summary		Toggle schema validation input
//	@Description	HTMX endpoint. Returns a schema URL input field, enabled or disabled based on the skip-validation flag.
//	@Tags			HTMX Actions
//	@Param			skip-validation	query	bool	false	"'true' to render the field as disabled"
//	@Success		200				"HTML partial"
//	@Router			/ui-api/signal-types/toggle-skip-validation [get]
func (s *Server) ToggleSkipValidation(w http.ResponseWriter, r *http.Request) {
	skipValidation := r.FormValue("skip-validation") == "true"
	templ.Handler(templates.SchemaURLInput(skipValidation)).ServeHTTP(w, r)
}

// ToggleSkipReadme godoc
//
//	@Summary		Toggle readme URL input
//	@Description	HTMX endpoint. Returns a readme URL input field, enabled or disabled based on the skip-readme flag.
//	@Tags			HTMX Actions
//	@Param			skip-readme	query	bool	false	"'true' to render the field as disabled"
//	@Success		200			"HTML partial"
//	@Router			/ui-api/signal-types/toggle-skip-readme [get]
func (s *Server) ToggleSkipReadme(w http.ResponseWriter, r *http.Request) {
	skipReadme := r.FormValue("skip-readme") == "true"
	templ.Handler(templates.ReadmeURLInput(skipReadme)).ServeHTTP(w, r)
}
