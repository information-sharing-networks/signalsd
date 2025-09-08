package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// HandleSignalSearch renders the signal search page
// ISN access is validated by RequireIsnAccess middleware
func (h *HandlerService) HandleSignalSearch(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Get ISN permissions from cookie - middleware ensures this exists
	isnPerms, err := h.AuthService.GetIsnPermsFromCookie(r)
	if err != nil {
		reqLogger.Error("failed to read IsnPerms from cookie", slog.String("error", err.Error()))
		return
	}

	// Convert permissions to ISN list for dropdown
	isns := make([]types.IsnDropdown, 0, len(isnPerms))
	for isnSlug := range isnPerms {
		isns = append(isns, types.IsnDropdown{
			Slug:    isnSlug,
			IsInUse: true,
		})
	}

	// Render search page
	component := templates.SignalSearchPage(isns, isnPerms, nil)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal search page", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) HandleSearchSignals(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse search parameters
	params := types.SignalSearchParams{
		IsnSlug:                 r.FormValue("isn_slug"),
		SignalTypeSlug:          r.FormValue("signal_type_slug"),
		SemVer:                  r.FormValue("sem_ver"),
		StartDate:               r.FormValue("start_date"),
		EndDate:                 r.FormValue("end_date"),
		AccountID:               r.FormValue("account_id"),
		SignalID:                r.FormValue("signal_id"),
		LocalRef:                r.FormValue("local_ref"),
		IncludeWithdrawn:        r.FormValue("include_withdrawn") == "true",
		IncludeCorrelated:       r.FormValue("include_correlated") == "true",
		IncludePreviousVersions: r.FormValue("include_previous_versions") == "true",
	}

	// Validate required parameters
	if params.IsnSlug == "" || params.SignalTypeSlug == "" || params.SemVer == "" {
		h.RenderError(w, r, "Please select ISN, signal type, and version.")
		return
	}

	//todo make helper
	// Get user permissions to validate ISN access and determine visibility
	isnPerm, err := h.AuthService.CheckIsnPermission(r, params.IsnSlug)
	if err != nil {
		h.RenderError(w, r, "You don't have permission to access this ISN.")
		return
	}

	// Get access token from cookie
	accessTokenCookie, err := r.Cookie(config.AccessTokenCookieName)
	if err != nil {
		reqLogger.Error("Access token not found", slog.String("error", err.Error()))
		h.RenderError(w, r, "Authentication required. Please log in again.")
		return
	}
	accessToken := accessTokenCookie.Value

	// Perform search using ISN visibility to determine endpoint
	searchResp, err := h.ApiClient.SearchSignals(accessToken, params, isnPerm.Visibility)
	if err != nil {
		reqLogger.Error("Signal search failed", slog.String("error", err.Error()))

		if ce, ok := err.(*client.ClientError); ok {
			h.RenderError(w, r, ce.UserError())
		} else {
			h.RenderError(w, r, "An error occurred. Please try again.")
		}
		return
	}

	// Render search results
	component := templates.SearchResults(*searchResp)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render search results", slog.String("error", err.Error()))
	}
}
