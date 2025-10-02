package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// SearchSignalsPage renders the signal search page
// ISN access is validated by RequireIsnAccess middleware
func (h *HandlerService) SearchSignalsPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	// Get ISN permissions from context - middleware ensures this exists
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	insPerms := accessTokenDetails.IsnPerms

	if len(insPerms) == 0 {
		reqLogger.Error("user does not have permission to access any ISNs")
		return
	}

	// Convert permissions to ISN list for dropdown
	isns := h.getIsnOptions(insPerms, false, false)

	// Render search page
	component := templates.SignalSearchPage(isns, insPerms, nil)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render signal search page", slog.String("error", err.Error()))
	}
}

func (h *HandlerService) GetLatestCorrelatedSignals(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	signalID := chi.URLParam(r, "signalID")
	countString := chi.URLParam(r, "count")
	isnSlug := chi.URLParam(r, "isnSlug")
	signalTypeSlug := chi.URLParam(r, "signalTypeSlug")
	semVer := chi.URLParam(r, "semVer")
	params := client.SignalSearchParams{
		IsnSlug:           isnSlug,
		SignalTypeSlug:    signalTypeSlug,
		SemVer:            semVer,
		SignalID:          signalID,
		IncludeCorrelated: true,
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Access token details not found in context")

		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	count, err := strconv.Atoi(countString)
	if err != nil {
		reqLogger.Error("Failed to convert count to int", slog.String("error", err.Error()))
		return
	}

	// Perform search using ISN visibility to determine endpoint
	searchResp, err := h.ApiClient.SearchSignals(accessTokenDetails.AccessToken, params, "private")
	if err != nil {
		reqLogger.Error("Signal search failed", slog.String("error", err.Error()))

		return
	}

	if len(*searchResp) != 1 { // todo handle error
		reqLogger.Error(fmt.Sprintf("Expected 1 signal, got %d", len(*searchResp)))
		return
	}

	// render correlated signals table
	component := templates.CorrelatedSignalsTable((*searchResp)[0], count)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render correlated signals table", slog.String("error", err.Error()))
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

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Access token details not found in context")

		component := templates.ErrorAlert("Authentication required. Please log in again.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Get user permissions to determine visibility of the isn being searched
	isnPerms := accessTokenDetails.IsnPerms
	if len(isnPerms) == 0 {
		component := templates.ErrorAlert("You don't have permission to access this ISN.")
		if err := component.Render(r.Context(), w); err != nil {
			reqLogger.Error("Failed to render error alert", slog.String("error", err.Error()))
		}
		return
	}

	// Perform search using ISN visibility to determine endpoint
	searchResp, err := h.ApiClient.SearchSignals(accessTokenDetails.AccessToken, params, isnPerms[params.IsnSlug].Visibility)
	if err != nil {
		reqLogger.Error("Signal search failed", slog.String("error", err.Error()))

		var msg string
		if clientError, ok := err.(*client.ClientError); ok {
			msg = clientError.UserError()
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
	component := templates.SearchResults(*searchResp, params.IsnSlug, params.SignalTypeSlug, params.SemVer)
	if err := component.Render(r.Context(), w); err != nil {
		reqLogger.Error("Failed to render search results", slog.String("error", err.Error()))
	}
}
