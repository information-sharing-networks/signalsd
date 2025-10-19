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

// getIsnOptions is a helper that returns a list of ISNs for the dropdown list. The list is used as a parater on several pages,
// Optionally the selected value can be use to trigger a cascading update of the signal type dropdown list (see SignalTypeOptionsHandler)
//
// If filterByIsnAdmin is true, only ISNs where the user is an admin are returned.
// If filterByWritePerm is true, only ISNs where the user has write permission are returned.
func (h *HandlerService) getIsnOptions(isnPerms map[string]types.IsnPerm, filterByIsnAdmin bool, filterByWritePerm bool) []types.IsnOption {
	isns := make([]types.IsnOption, 0, len(isnPerms))
	for isnSlug, perm := range isnPerms {
		if filterByIsnAdmin && !perm.IsnAdmin {
			continue
		}
		if filterByWritePerm && perm.Permission != "write" {
			continue
		}
		isns = append(isns, types.IsnOption{
			Slug:    isnSlug,
			IsInUse: true,
		})
	}
	return isns
}

// RenderSignalTypeSlugOptions gets the signal types for the selected ISN and renders the dropdown options
// optionally the calling form can specify include_inactive=true to include signal types that are not in use
func (h *HandlerService) RenderSignalTypeSlugOptions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	isnSlug := r.FormValue("isn-slug")
	if isnSlug == "" {
		component := templates.ErrorAlert("Please select an ISN first")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to get accessTokenDetails from context in signal types handler")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	includeInactive := r.FormValue("include_inactive") == "true"

	// Get signal types for the selected ISN
	signalTypes, err := h.ApiClient.GetSignalTypes(accessTokenDetails.AccessToken, isnSlug, includeInactive)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// create a slice of signalTypeSlugOptions - note we are not interested in the signal type version at this stage so return a deduplicated list of slugs
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

	isnSlug := r.FormValue("isn-slug")
	signalTypeSlug := r.FormValue("signal-type-slug")
	if isnSlug == "" || signalTypeSlug == "" {
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

	// Get signal types for the selected ISN
	signalTypes, err := h.ApiClient.GetSignalTypes(accessTokenDetails.AccessToken, isnSlug, includeInactive)
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
