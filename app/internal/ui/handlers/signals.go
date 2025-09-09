package handlers

import (
	"log/slog"
	"net/http"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/config"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// SearchSignalsPage renders the signal search page
// ISN access is validated by RequireIsnAccess middleware
func (h *HandlerService) SearchSignalsPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Get ISN permissions from cookie - middleware ensures this exists
	isnPerms, err := h.AuthService.GetIsnPermsFromCookie(r)
	if err != nil {
		reqLogger.Error("failed to read IsnPerms from cookie", slog.String("error", err.Error()))
		return
	}

	// Convert permissions to ISN list for dropdown
	isns := h.getIsnOptions(isnPerms, false, false)

	// Render search page
	component := templates.SignalSearchPage(isns, isnPerms, nil)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal search page", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) SearchSignals(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Parse search parameters
	params := client.SignalSearchParams{
		IsnSlug:                 r.FormValue("isn-slug"),
		SignalTypeSlug:          r.FormValue("signal-type-slug"),
		SemVer:                  r.FormValue("sem-ver"),
		StartDate:               r.FormValue("start-date"),
		EndDate:                 r.FormValue("end-date"),
		AccountID:               r.FormValue("account-id"),
		SignalID:                r.FormValue("signal-id"),
		LocalRef:                r.FormValue("local-ref"),
		IncludeWithdrawn:        r.FormValue("include-withdrawn") == "true",
		IncludeCorrelated:       r.FormValue("include-correlated") == "true",
		IncludePreviousVersions: r.FormValue("include-previous-versions") == "true",
	}

	// Validate required parameters
	if params.IsnSlug == "" || params.SignalTypeSlug == "" || params.SemVer == "" {
		component := templates.ErrorAlert("Please select ISN, signal type, and version.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get access token from cookie
	accessTokenCookie, err := r.Cookie(config.AccessTokenCookieName)
	if err != nil {
		reqLogger.Error("Access token not found", slog.String("error", err.Error()))

		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}
	accessToken := accessTokenCookie.Value

	// Get user permissions to determine visibility of the isn being searched
	isnPerms, err := h.AuthService.GetIsnPermsFromCookie(r)
	if err != nil {
		component := templates.ErrorAlert("You don't have permission to access this ISN.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Perform search using ISN visibility to determine endpoint
	searchResp, err := h.ApiClient.SearchSignals(accessToken, params, isnPerms[params.IsnSlug].Visibility)
	if err != nil {
		reqLogger.Error("Signal search failed", slog.String("error", err.Error()))

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

	// Render search results
	component := templates.SearchResults(*searchResp)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render search results", slog.String("error", err.Error()))
	}
}
