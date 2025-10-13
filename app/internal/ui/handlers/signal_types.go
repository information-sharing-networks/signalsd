package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// CreateSignalTypePage renders the create signal type page.
//
// Use with RequireAdminOrOwnerRole and RequireIsnAdmin middleware
func (h *HandlerService) CreateSignalTypePage(w http.ResponseWriter, r *http.Request) {
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

	component := templates.CreateSignalTypePage(isns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal type management page", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) RegisterNewSignalTypeSchemaPage(w http.ResponseWriter, r *http.Request) {
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

	// Render signal type management page
	component := templates.RegisterNewSignalTypeSchemaPage(isns)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal type management page", slog.String("error", err.Error()))
	}
}

// CreateSignalType handles the form submission to create a new signal type
// Use with RequireAdminOrOwnerRole and RequireIsnAdmin middleware
func (h *HandlerService) CreateSignalType(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	title := r.FormValue("title")
	schemaURL := r.FormValue("schema-url")
	bumpType := r.FormValue("bump-type")
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
	if isnSlug == "" || title == "" || schemaURL == "" || bumpType == "" || readmeURL == "" || detail == "" {
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
	createReq := client.CreateSignalTypeRequest{
		IsnSlug:   isnSlug,
		SchemaURL: schemaURL,
		Title:     title,
		BumpType:  bumpType,
		ReadmeURL: readmeURL,
		Detail:    detail,
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

// RegisterNewSignalTypeSchema handles the form submission to register a new schema for an existing signal type
// Use with RequireAdminOrOwnerRole and RequireIsnAdmin middleware
func (h *HandlerService) RegisterNewSignalTypeSchema(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse form data
	isnSlug := r.FormValue("isn-slug")
	slug := r.FormValue("signal-type-slug")
	schemaURL := r.FormValue("schema-url")
	bumpType := r.FormValue("bump-type")
	readmeURL := r.FormValue("readme-url")
	detail := r.FormValue("detail")

	if isnSlug == "" || slug == "" || schemaURL == "" || bumpType == "" || readmeURL == "" || detail == "" {
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
	createReq := client.RegisterNewSignalTypeSchemaRequest{
		IsnSlug:   isnSlug,
		SchemaURL: schemaURL,
		Slug:      slug,
		BumpType:  bumpType,
		ReadmeURL: readmeURL,
		Detail:    detail,
	}

	// Call the API to create the signal type
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
