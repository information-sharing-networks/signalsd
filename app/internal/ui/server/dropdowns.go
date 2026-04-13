package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// APISignalTypeSlugs godoc
//
//	@Summary		Signal type slugs dropdown (admin)
//	@Description	HTMX endpoint. Returns a signal type slug select element for admin forms.
//	@Description	Covers all globally-registered signal types regardless of ISN membership.
//	@Tags			HTMX Actions
//	@Success		200	"HTML partial"
//	@Router			/ui-api/options/signal-type-slugs [get]
func (s *Server) APISignalTypeSlugs(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to get accessTokenDetails from context in signal types handler")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	signalTypes, err := s.apiClient.GetSignalTypes(accessTokenDetails.AccessToken)
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

	versionsEndpoint := "/ui-api/options/signal-type-versions"

	// this will render the signal type dropdown and the version dropdown
	// the version dropdown is initially disabled and will be populated when a signal type is selected
	templ.Handler(templates.SignalTypeSlugsSelect(signalTypeSlugs, versionsEndpoint, "#isn-slug")).ServeHTTP(w, r)
}

// APISignalTypeVersions godoc
//
//	@Summary		Signal type versions dropdown (admin)
//	@Description	HTMX endpoint. Returns a version select element for a signal type. Covers all globally-registered versions.
//	@Tags			HTMX Actions
//	@Param			signal-type-slug	query	string	true	"Signal type slug"	example(sample-signal-type)
//	@Success		200					"HTML partial"
//	@Failure		400					"empty response"
//	@Router			/ui-api/options/signal-type-versions [get]
func (s *Server) APISignalTypeVersions(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	signalTypeSlug := r.FormValue("signal-type-slug")
	if signalTypeSlug == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Failed to get accessTokenDetails from context in versions handler")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	signalTypes, err := s.apiClient.GetSignalTypes(accessTokenDetails.AccessToken)
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

	templ.Handler(templates.SignalTypeVersionsSelect(versions)).ServeHTTP(w, r)
}

// TokenSignalTypeSlugs godoc
//
//	@Summary		Signal type slugs dropdown (token-scoped)
//	@Description	HTMX endpoint. Returns a signal type slug select element scoped to the user's token permissions for the selected ISN.
//	@Tags			HTMX Actions
//	@Param			isn-slug			query	string	true	"ISN slug"	example(felixstowe-isn)
//	@Param			include_inactive	query	bool	false	"Include inactive signal types"
//	@Success		200					"HTML partial"
//	@Failure		400					"empty response"
//	@Router			/ui-api/options/isn/signal-type-slugs [get]
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

	// Deduplicate by slug - the token stores one entry per signal type path (slug/vX.Y.Z).
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
	templ.Handler(templates.SignalTypeSlugsSelect(signalTypeSlugs, versionsEndpoint, "#isn-slug")).ServeHTTP(w, r)
}

// TokenSignalTypeVersions godoc
//
//	@Summary		Signal type versions dropdown (token-scoped)
//	@Description	HTMX endpoint. Returns a version select element for a signal type, scoped to the selected ISN via the user's token permissions.
//	@Tags			HTMX Actions
//	@Param			signal-type-slug	query	string	true	"Signal type slug"	example(sample-signal-type)
//	@Param			isn-slug			query	string	true	"ISN slug"			example(felixstowe-isn)
//	@Param			include_inactive	query	bool	false	"Include inactive versions"
//	@Success		200					"HTML partial"
//	@Failure		400					"empty response"
//	@Router			/ui-api/options/isn/signal-type-versions [get]
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

	templ.Handler(templates.SignalTypeVersionsSelect(versions)).ServeHTTP(w, r)
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

// getUserOptions converts raw user data to UserOption items,
// excluding the user with the specified email address.
// This is used in forms where a user should not be able to operate on themselves
// (e.g., changing their own role, disabling their own account, transferring ISN to themselves).
func getUserOptions(rawUsers []client.User, currentEmail string) []types.UserOption {
	options := make([]types.UserOption, 0, len(rawUsers))
	for _, u := range rawUsers {
		if u.Email == currentEmail {
			continue
		}
		options = append(options, types.UserOption{Email: u.Email, UserRole: u.UserRole})
	}
	return options
}

// getServiceAccountOptions converts raw service account data to ServiceAccountOption items.
func getServiceAccountOptions(rawAccounts []client.ServiceAccount) []types.ServiceAccountOption {
	options := make([]types.ServiceAccountOption, 0, len(rawAccounts))
	for _, sa := range rawAccounts {
		options = append(options, types.ServiceAccountOption{
			ClientOrganization: sa.ClientOrganization,
			ClientContactEmail: sa.ClientContactEmail,
			ClientID:           sa.ClientID,
		})
	}
	return options
}
