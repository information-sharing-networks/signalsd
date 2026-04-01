package server

import (
	"fmt"
	"log/slog"
	"net/http"

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
	reqLogger := logger.ContextRequestLogger(r.Context())

	component := templates.CreateSignalTypePage(s.config.Environment, make([]types.IsnOption, 0))
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render create signal type page", slog.String("error", err.Error()))
	}
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

	// Get all signal types (include inactive for schema registration)
	signalTypes, err := s.apiClient.GetSignalTypes(accessTokenDetails.AccessToken, true)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load signal types. Please try again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
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

	component := templates.RegisterNewSignalTypeSchemaPageWithOptions(s.config.Environment, signalTypeSlugs)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render register new schema page", slog.String("error", err.Error()))
	}
}

// CreateSignalType godoc
//
//	@Summary		Create signal type
//	@Description	HTMX endpoint. Creates a new signal type at version 1.0.0. Requires siteadmin role.
//	@Tags			HTMX Actions
//	@Param			title			formData	string	true	"Signal type title"
//	@Param			detail			formData	string	true	"Description"
//	@Param			schema-url		formData	string	true	"JSON Schema URL (ignored when skip-validation is true)"
//	@Param			readme-url		formData	string	true	"Readme URL (ignored when skip-readme is true)"
//	@Param			skip-validation	formData	string	false	"'true' to skip schema validation"
//	@Param			skip-readme		formData	string	false	"'true' to skip readme requirement"
//	@Success		200				"HTML partial"
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
		component := templates.ErrorAlert("Please fill in all required fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Prepare request - create signal type with initial version 1.0.0
	createReq := client.CreateSignalTypeRequest{
		SchemaURL: schemaURL,
		Title:     title,
		ReadmeURL: readmeURL,
		Detail:    detail,
		BumpType:  "major", // Initial version is always 1.0.0
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

		component := templates.ErrorAlert(msg)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Success response
	component := templates.SignalTypeCreationSuccess(*response)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
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
		component := templates.ErrorAlert("Please fill in all required fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
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

		component := templates.ErrorAlert(msg)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Success response
	component := templates.SignalTypeCreationSuccess(*response)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
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
//	@Router			/ui-api/isn/signal-types/add [post]
func (s *Server) AddSignalTypeToIsn(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	signalTypeSlug := r.FormValue("signal-type-slug")
	semVer := r.FormValue("sem-ver")

	// Validate required fields
	if isnSlug == "" || signalTypeSlug == "" || semVer == "" {
		component := templates.ErrorAlert("Please fill in all required fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
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

		component := templates.ErrorAlert(msg)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Success response
	component := templates.SuccessAlert(fmt.Sprintf("Signal type %s/v%s associated with ISN successfully", signalTypeSlug, semVer))
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
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

	component := templates.AddSignalTypeToIsnPage(s.config.Environment, isns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render add signal type page", slog.String("error", err.Error()))
	}
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
		component := templates.ErrorAlert("You don't have permission to access any ISNs.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Convert permissions to ISN list for dropdown (only ISNs where user has admin rights)
	isns := getIsnOptions(isnPerms, true, false)

	// Render ISN signal type status management page
	component := templates.ManageIsnSignalTypesStatusPage(s.config.Environment, isns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render ISN signal type status management page", slog.String("error", err.Error()))
	}
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
		component := templates.ErrorAlert("Please fill in all fields.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to read access token from context", slog.String("component", "handlers.AdminIsnSignalTypeStatus"))

		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
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
		component := templates.ErrorAlert("Invalid action. Please select a valid action.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
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

		component := templates.ErrorAlert(msg)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Success response
	component := templates.SuccessAlert(successMsg)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render success message", slog.String("error", err.Error()))
	}
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
	signalTypes, err := s.apiClient.GetSignalTypes(accessTokenDetails.AccessToken, false)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load signal types. Please try again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	component := templates.ListSignalTypesPage(s.config.Environment, signalTypes)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal types config page", slog.String("error", err.Error()))
	}
}

func (s *Server) ToggleSkipValidation(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	skipValidation := r.FormValue("skip-validation") == "true"

	component := templates.SchemaURLInput(skipValidation)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render schema URL input", slog.String("error", err.Error()))
	}
}

func (s *Server) ToggleSkipReadme(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	skipReadme := r.FormValue("skip-readme") == "true"

	component := templates.ReadmeURLInput(skipReadme)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render readme URL input", slog.String("error", err.Error()))
	}
}
