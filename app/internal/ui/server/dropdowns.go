package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// APISignalTypeSlugs gets all signal types from the API and renders the Signal Type and version dropdowns.
// Used by admin-only forms that need all globally-registered signal types regardless of ISN membership
// or user permissions.
func (s *Server) APISignalTypeSlugs(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to get accessTokenDetails from context in signal types handler")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	includeInactive := r.FormValue("include_inactive") == "true"

	signalTypes, err := s.apiClient.GetSignalTypes(accessTokenDetails.AccessToken, includeInactive)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Deduplicate by slug to show each signal type once
	signalTypeSlugs := make([]types.SignalTypeSlug, 0, len(signalTypes))
	seen := make(map[string]bool)
	for _, signalType := range signalTypes {
		if !seen[signalType.Slug] {
			seen[signalType.Slug] = true
			signalTypeSlugs = append(signalTypeSlugs, types.SignalTypeSlug{Slug: signalType.Slug})
		}
	}

	versionsEndpoint := fmt.Sprintf("/ui-api/options/signal-type-versions?include_inactive=%v", includeInactive)

	// this will render the signal type dropdown and the version dropdown
	// the version dropdown is initially disabled and will be populated when a signal type is selected
	component := templates.SignalTypeSlugsSelect(signalTypeSlugs, versionsEndpoint)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal type options", slog.String("error", err.Error()))
	}
}

// APISignalTypeVersions returns versions for a signal type from the API.
// Used by admin-only forms (e.g. AddSignalTypeToIsn) that need all globally-registered
// versions regardless of ISN membership or user permissions.
// This endpoint is called by the SignalTypeSlugs handler via HTMX.
func (s *Server) APISignalTypeVersions(w http.ResponseWriter, r *http.Request) {
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

	signalTypes, err := s.apiClient.GetSignalTypes(accessTokenDetails.AccessToken, includeInactive)
	if err != nil {
		reqLogger.Error("Failed to get signal types", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	versions := make([]types.SignalTypeVersion, 0)
	for _, st := range signalTypes {
		if st.Slug == signalTypeSlug {
			versions = append(versions, types.SignalTypeVersion{Version: st.SemVer})
		}
	}

	component := templates.SignalTypeVersionsSelect(versions)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render version options", slog.String("error", err.Error()))
	}
}

// TokenSignalTypeSlugs returns signal type slug options for a selected ISN.
// Signal types are derived from the user's token (IsnPerms) so the list is
// limited to what the user actually has access to.
// Used by user-facing forms (e.g. signal search)
func (s *Server) TokenSignalTypeSlugs(w http.ResponseWriter, r *http.Request) {
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

	isnPerm, exists := accessTokenDetails.IsnPerms[isnSlug]
	if !exists {
		reqLogger.Error("ISN not found in token permissions", slog.String("isn_slug", isnSlug))
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// Deduplicate by slug — the token stores one entry per signal type path (slug/vX.Y.Z).
	seen := make(map[string]bool)
	signalTypeSlugs := make([]types.SignalTypeSlug, 0, len(isnPerm.SignalTypes))
	for _, st := range isnPerm.SignalTypes {
		if !includeInactive && !st.InUse {
			continue
		}
		if !seen[st.Slug] {
			seen[st.Slug] = true
			signalTypeSlugs = append(signalTypeSlugs, types.SignalTypeSlug{Slug: st.Slug})
		}
	}

	versionsEndpoint := fmt.Sprintf("/ui-api/options/isn/signal-type-versions?include_inactive=%v", includeInactive)
	component := templates.SignalTypeSlugsSelect(signalTypeSlugs, versionsEndpoint)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal type options", slog.String("error", err.Error()))
	}
}

// TokenSignalTypeVersions returns versions for a signal type scoped to a specific ISN.
// Versions are read from the user's token (IsnPerms)
func (s *Server) TokenSignalTypeVersions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	signalTypeSlug := r.FormValue("signal-type-slug")
	isnSlug := r.FormValue("isn-slug")
	if signalTypeSlug == "" || isnSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	includeInactive := r.FormValue("include_inactive") == "true"

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to get accessTokenDetails from context in versions-for-isn handler")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	isnPerm, exists := accessTokenDetails.IsnPerms[isnSlug]
	if !exists {
		reqLogger.Error("ISN not found in token permissions", slog.String("isn_slug", isnSlug))
		w.WriteHeader(http.StatusForbidden)
		return
	}

	versions := make([]types.SignalTypeVersion, 0)
	for _, st := range isnPerm.SignalTypes {
		if st.Slug != signalTypeSlug {
			continue
		}
		if !includeInactive && !st.InUse {
			continue
		}
		versions = append(versions, types.SignalTypeVersion{Version: st.SemVer})
	}

	component := templates.SignalTypeVersionsSelect(versions)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render version options", slog.String("error", err.Error()))
	}
}

// getIsnOptions returns ISN slugs from the user's token claims, filtered by permission.
// If filterByIsnAdmin is true, only ISNs where the user has admin rights are returned.
// If filterByWritePerm is true, only ISNs where the user has write permission are returned.
func getIsnOptions(isnPerms map[string]types.IsnPerm, filterByIsnAdmin bool, filterByWritePerm bool) []types.IsnOption {
	isns := make([]types.IsnOption, 0, len(isnPerms))
	for isnSlug, perm := range isnPerms {
		if filterByIsnAdmin && !perm.CanAdminister {
			continue
		}
		if filterByWritePerm && !perm.CanWrite {
			continue
		}
		isns = append(isns, types.IsnOption{Slug: isnSlug})
	}
	return isns
}
