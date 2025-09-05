package handlers

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

type HandlerService struct {
	AuthService *auth.AuthService
	ApiClient   *client.Client
	Environment string
}

// RenderError displays an error message inline to the user
func (h *HandlerService) RenderError(w http.ResponseWriter, r *http.Request, msg string) {
	component := templates.ErrorAlert(msg)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger := logger.ContextRequestLogger(r.Context())
		reqLogger.Error("Failed to render error", slog.String("error", err.Error()))
	}
}

// HandleGetSignalTypes handles the form submission to get signal types for the selected ISN
func (h *HandlerService) HandleGetSignalTypes(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	isnSlug := r.FormValue("isn_slug")
	if isnSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get permissions data from cookie
	permsCookie, err := r.Cookie(config.IsnPermsCookieName)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Decode base64 cookie value
	decodedPerms, err := base64.StdEncoding.DecodeString(permsCookie.Value)
	if err != nil {
		reqLogger.Error("Failed to decode permissions cookie in signal types handler",
			slog.String("error", err.Error()),
			slog.String("cookie_value", permsCookie.Value))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var perms map[string]types.IsnPerm
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		reqLogger.Error("Failed to parse permissions JSON in signal types handler",
			slog.String("error", err.Error()),
			slog.String("json_data", string(decodedPerms)))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Get signal types for the selected ISN
	isnPerm, exists := perms[isnSlug]
	if !exists {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Parse signal type paths to extract unique signal types
	signalTypeMap := make(map[string]bool)
	for _, path := range isnPerm.SignalTypePaths {
		// Path format: "signal-type-slug/v1.0.0"
		parts := strings.Split(path, "/v")
		if len(parts) == 2 {
			signalTypeMap[parts[0]] = true
		}
	}

	// Convert to slice of SignalTypeDropdown
	signalTypes := make([]types.SignalTypeDropdown, 0, len(signalTypeMap))
	for signalType := range signalTypeMap {
		signalTypes = append(signalTypes, types.SignalTypeDropdown{
			Slug: signalType,
		})
	}

	// Render signal types dropdown options
	component := templates.SignalTypeOptions(signalTypes)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal type options", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) HandleGetSignalVersions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	isnSlug := r.FormValue("isn_slug")
	signalTypeSlug := r.FormValue("signal_type_slug")
	if isnSlug == "" || signalTypeSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Get permissions data from cookie
	permsCookie, err := r.Cookie(config.IsnPermsCookieName)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Decode base64 cookie value
	decodedPerms, err := base64.StdEncoding.DecodeString(permsCookie.Value)
	if err != nil {
		reqLogger.Error("Failed to decode permissions cookie in versions handler",
			slog.String("error", err.Error()),
			slog.String("cookie_value", permsCookie.Value))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var perms map[string]types.IsnPerm
	if err := json.Unmarshal(decodedPerms, &perms); err != nil {
		reqLogger.Error("Failed to parse permissions JSON in versions handler",
			slog.String("error", err.Error()),
			slog.String("json_data", string(decodedPerms)))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Get signal types for the selected ISN
	isnPerm, exists := perms[isnSlug]
	if !exists {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Find versions for the specific signal type
	versions := make([]types.VersionDropdown, 0)
	for _, path := range isnPerm.SignalTypePaths {
		// Path format: "signal-type-slug/v1.0.0"
		parts := strings.Split(path, "/v")
		if len(parts) == 2 && parts[0] == signalTypeSlug {
			versions = append(versions, types.VersionDropdown{
				Version: parts[1],
			})
		}
	}

	// Render version dropdown options
	component := templates.VersionOptions(versions)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render version options", slog.String("error", err.Error()))
	}
}

// HandleAccessDenied handles access denied for both HTMX and direct requests
// Always redirects to an access denied page for consistent UX
func (h *HandlerService) HandleAccessDenied(w http.ResponseWriter, r *http.Request, pageTitle, message string) {
	if r.Header.Get("HX-Request") == "true" {
		// HTMX request - redirect to access denied page
		w.Header().Set("HX-Redirect", "/access-denied?title="+pageTitle+"&message="+message)
	} else {
		// Direct navigation - render access denied page
		component := templates.AccessDeniedPage(pageTitle, message)
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger := logger.ContextRequestLogger(r.Context())
			reqLogger.Error("Failed to render access denied page", slog.String("error", err.Error()))
		}
	}
}
