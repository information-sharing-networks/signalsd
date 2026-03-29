package handlers

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

// CreateSignalTypePage renders the create signal type page.
//
// Use with RequireAdminOrOwnerRole middleware
func (h *HandlerService) CreateSignalTypePage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	component := templates.CreateSignalTypePage(h.Environment, make([]types.IsnOption, 0))
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render create signal type page", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) RegisterNewSignalTypeSchemaPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Get all signal types (include inactive for schema registration)
	signalTypes, err := h.ApiClient.GetSignalTypes(accessTokenDetails.AccessToken, true)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		component := templates.ErrorAlert("Failed to load signal types. Please try again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Deduplicate by slug to show each signal type once
	signalTypeSlugs := make([]types.SignalTypeSlugOption, 0, len(signalTypes))
	seen := make(map[string]bool)

	for _, signalType := range signalTypes {
		if !seen[signalType.Slug] {
			seen[signalType.Slug] = true
			signalTypeSlugs = append(signalTypeSlugs, types.SignalTypeSlugOption{
				Slug: signalType.Slug,
			})
		}
	}

	component := templates.RegisterNewSignalTypeSchemaPageWithOptions(h.Environment, signalTypeSlugs)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render register new schema page", slog.String("error", err.Error()))
	}
}

// CreateSignalType handles the form submission to create a new signal type
// Use with RequireAdminOrOwnerRole middleware
func (h *HandlerService) CreateSignalType(w http.ResponseWriter, r *http.Request) {
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
	response, err := h.ApiClient.CreateSignalType(accessTokenDetails.AccessToken, createReq)
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

// RegisterNewSignalTypeSchema handles the form submission to create a new version of an existing signal type
// Use with RequireAdminOrOwnerRole middleware
func (h *HandlerService) RegisterNewSignalTypeSchema(w http.ResponseWriter, r *http.Request) {
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
	response, err := h.ApiClient.RegisterNewSignalTypeSchema(accessTokenDetails.AccessToken, createReq)
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

// AddSignalTypeToIsn handles the form submission to add a signal type to an ISN
// Use with RequireAdminOrOwnerRole and RequireIsnAdmin middleware
func (h *HandlerService) AddSignalTypeToIsn(w http.ResponseWriter, r *http.Request) {
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
	err := h.ApiClient.AddSignalTypeToIsn(accessTokenDetails.AccessToken, isnSlug, associateReq)
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

// AddSignalTypeToIsnPage renders the page to add a signal type to an ISN
// Use with RequireAdminOrOwnerRole and RequireIsnAdmin middleware
func (h *HandlerService) AddSignalTypeToIsnPage(w http.ResponseWriter, r *http.Request) {
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
	isns := h.getIsnOptions(isnPerms, true, false)

	component := templates.AddSignalTypeToIsnPage(h.Environment, isns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render add signal type page", slog.String("error", err.Error()))
	}
}

// IsnSignalTypeStatusPage renders the ISN-level signal type status management page
func (h *HandlerService) IsnSignalTypeStatusPage(w http.ResponseWriter, r *http.Request) {
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
	isns := h.getIsnOptions(isnPerms, true, false)

	// Render ISN signal type status management page
	component := templates.IsnSignalTypeStatusPage(h.Environment, isns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render ISN signal type status management page", slog.String("error", err.Error()))
	}
}

// AdminIsnSignalTypeStatus handles the form submission to enable or disable signal types for an ISN
func (h *HandlerService) AdminIsnSignalTypeStatus(w http.ResponseWriter, r *http.Request) {
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
	err := h.ApiClient.UpdateIsnSignalTypeStatus(accessTokenDetails.AccessToken, isnSlug, signalTypeSlug, semVer, isInUse)
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

func (h *HandlerService) ToggleSkipValidation(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	skipValidation := r.FormValue("skip-validation") == "true"

	component := templates.SchemaURLInput(skipValidation)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render schema URL input", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) ToggleSkipReadme(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	skipReadme := r.FormValue("skip-readme") == "true"

	component := templates.ReadmeURLInput(skipReadme)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render readme URL input", slog.String("error", err.Error()))
	}
}
