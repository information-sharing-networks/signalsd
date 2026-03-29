package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

type HandlerService struct {
	AuthService *auth.AuthService
	ApiClient   *client.Client
	Environment string
}

// getIsnOptions is a helper that returns a list of ISNs for the dropdown list. The list is used as a parameter on several pages,
// Optionally the selected value can be use to trigger a cascading update of the signal type dropdown list (see SignalTypeOptionsHandler)
//
// If filterByIsnAdmin is true, only ISNs where the user is an admin are returned.
// If filterByWritePerm is true, only ISNs where the user has write permission are returned.
func (h *HandlerService) getIsnOptions(isnPerms map[string]types.IsnPerm, filterByIsnAdmin bool, filterByWritePerm bool) []types.IsnOption {
	isns := make([]types.IsnOption, 0, len(isnPerms))
	for isnSlug, perm := range isnPerms {
		if filterByIsnAdmin && !perm.CanAdminister {
			continue
		}
		if filterByWritePerm && !perm.CanWrite {
			continue
		}
		isns = append(isns, types.IsnOption{
			Slug:       isnSlug,
			IsInUse:    perm.InUse,
			Visibility: perm.Visibility,
		})
	}
	return isns
}

// RenderSignalTypeSlugOptions gets all signal types and renders the dropdown options
// optionally the calling form can specify include_inactive=true to include signal types that are not in use
func (h *HandlerService) RenderSignalTypeSlugOptions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to get accessTokenDetails from context in signal types handler")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	includeInactive := r.FormValue("include_inactive") == "true"

	// Get all signal types
	signalTypes, err := h.ApiClient.GetSignalTypes(accessTokenDetails.AccessToken, includeInactive)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// create a slice of signalTypeSlugOptions - deduplicate by slug to show each signal type once
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
	// Render signal types dropdown options
	component := templates.SignalTypeSlugOptions(signalTypeSlugs, includeInactive)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal type options", slog.String("error", err.Error()))
	}
}

// RenderSignalTypeVersionOptions gets the versions for the selected signal type and returns the dropdown options
// optionally the calling form can specify include_inactive=true to include versions that are not in use
func (h *HandlerService) RenderSignalTypeVersionOptions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	signalTypeSlug := r.FormValue("signal-type-slug")
	if signalTypeSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	includeInactive := r.FormValue("include_inactive") == "true"

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to get accessTokenDetails from context in versions handler")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Get all signal types
	signalTypes, err := h.ApiClient.GetSignalTypes(accessTokenDetails.AccessToken, includeInactive)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Find versions for the specific signal type
	versions := make([]types.VersionOption, 0)
	for _, signalType := range signalTypes {
		if signalType.Slug == signalTypeSlug {
			versions = append(versions, types.VersionOption{
				Version: signalType.SemVer,
			})
		}
	}

	// Render version dropdown options
	component := templates.SignalTypeVersionOptions(versions)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render version options", slog.String("error", err.Error()))
	}
}

// RenderSignalTypeSlugsForIsnOptions gets signal types associated with a specific ISN and renders dropdown options
// Used for the 3-stage dropdown: ISN → Signal Type → Version
func (h *HandlerService) RenderSignalTypeSlugsForIsnOptions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	isnSlug := r.FormValue("isn-slug")
	if isnSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	includeInactive := r.FormValue("include_inactive") == "true"

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to get accessTokenDetails from context")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	// Get signal types for the specific ISN
	signalTypes, err := h.ApiClient.GetSignalTypesForISN(accessTokenDetails.AccessToken, isnSlug, includeInactive)
	if err != nil {
		reqLogger.Error("Failed to get signal types for ISN", slog.String("error", err.Error()), slog.String("isn_slug", isnSlug))
		w.WriteHeader(http.StatusInternalServerError)
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

	// Render signal types dropdown options
	component := templates.SignalTypeSlugOptions(signalTypeSlugs, includeInactive)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal type options", slog.String("error", err.Error()))
	}
}
