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

// SignalTypeOptionsHandler gets the signal types for the selected ISN and returns the dropdown options
func (h *HandlerService) SignalTypeOptionsHandler(w http.ResponseWriter, r *http.Request) {
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

// SignalTypeVersionOptionsHandler gets the versions for the selected signal type and returns the dropdown options
func (h *HandlerService) SignalTypeVersionOptionsHandler(w http.ResponseWriter, r *http.Request) {
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
	component := templates.SignalTypeVersionOptions(versions)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render version options", slog.String("error", err.Error()))
	}
}
