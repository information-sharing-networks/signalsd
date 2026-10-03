package server

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
)

// SearchSignalsPage godoc
//
//	@Summary		Search signals page
//	@Description	Renders the signal search page. Requires authentication and at least one ISN access permission.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/search [get]
func (s *Server) SearchSignalsPage(w http.ResponseWriter, r *http.Request) {
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
	isns := getIsnOptions(insPerms, false)

	templ.Handler(templates.SearchSignalsPage(s.config.Environment, isns, insPerms, nil)).ServeHTTP(w, r)
}

// GetLatestCorrelatedSignals godoc
//
//	@Summary		Get correlated signals
//	@Description	HTMX endpoint. Returns a table of signals correlated to the specified signal.
//	@Tags			HTMX Actions
//	@Param			signal_id			path	string	true	"Signal ID"
//	@Param			isn_slug			path	string	true	"ISN slug"
//	@Param			signal_type_slug	path	string	true	"Signal type slug"
//	@Param			sem_ver				path	string	true	"Semantic version"
//	@Param			count				path	int		true	"Number of correlated signals already displayed"
//	@Success		200					"HTML partial"
//	@Router			/ui-api/signals/{isn_slug}/{signal_type_slug}/v{sem_ver}/{signal_id}/correlated/{count} [get]
func (s *Server) GetLatestCorrelatedSignals(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	signalID := chi.URLParam(r, "signal_id")
	countString := chi.URLParam(r, "count")
	isnSlug := chi.URLParam(r, "isn_slug")
	signalTypeSlug := chi.URLParam(r, "signal_type_slug")
	semVer := chi.URLParam(r, "sem_ver")
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
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	count, err := strconv.Atoi(countString)
	if err != nil {
		reqLogger.Error("Failed to convert count to int", slog.String("error", err.Error()))
		return
	}

	// Perform search using ISN visibility to determine endpoint
	searchResp, err := s.apiClient.SearchSignals(r.Context(), accessTokenDetails.AccessToken, params, "private")
	if err != nil {
		reqLogger.Error("Signal search failed", slog.String("error", err.Error()))
		return
	}

	if len(*searchResp) != 1 {
		reqLogger.Error(fmt.Sprintf("Expected 1 signal, got %d", len(*searchResp)))
		return
	}

	templ.Handler(templates.CorrelatedSignalsTable(isnSlug, (*searchResp)[0], count)).ServeHTTP(w, r)
}

// DownloadDocument godoc
//
//	@Summary		Download a document
//	@Description	Streams the document stored with a document signal from the signalsd API to the browser as an attachment. Requires ISN read access.
//	@Tags			UI Pages
//	@Param			isn_slug			path	string	true	"ISN slug"
//	@Param			signal_type_slug	path	string	true	"Signal type slug"
//	@Param			sem_ver				path	string	true	"Semantic version"
//	@Param			signal_id			path	string	true	"Signal ID"
//	@Param			version				query	int		false	"the version to download (default: the latest version)"
//	@Param			include_withdrawn	query	bool	false	"'true' to download a withdrawn document"
//	@Success		200					"the document"
//	@Router			/ui-api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/{signal_id}/content [get]
func (s *Server) DownloadDocument(w http.ResponseWriter, r *http.Request) {
	reqLogger := logger.ContextRequestLogger(r.Context())

	params := client.DownloadDocumentParams{
		IsnSlug:          chi.URLParam(r, "isn_slug"),
		SignalTypeSlug:   chi.URLParam(r, "signal_type_slug"),
		SemVer:           chi.URLParam(r, "sem_ver"),
		SignalID:         chi.URLParam(r, "signal_id"),
		VersionNumber:    r.URL.Query().Get("version"),
		IncludeWithdrawn: r.URL.Query().Get("include_withdrawn") == "true",
	}

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Access token details not found in context")
		http.Error(w, "Authentication required. Please log in again.", http.StatusUnauthorized)
		return
	}

	// this endpoint is opened as a link rather than called by HTMX, so errors are returned as plain text
	res, err := s.apiClient.DownloadDocument(r.Context(), accessTokenDetails.AccessToken, params)
	if err != nil {
		reqLogger.Error("Failed to download document", slog.String("error", err.Error()))

		status := http.StatusInternalServerError
		if cerr, ok := errors.AsType[*client.ClientError](err); ok && cerr.StatusCode > 0 {
			status = cerr.StatusCode
		}
		http.Error(w, client.UserMessage(err), status)
		return
	}
	defer res.Body.Close()

	// pass on the headers that tell the browser what the document is and what to call it
	for _, header := range []string{"Content-Type", "Content-Disposition", "Content-Length", "ETag"} {
		if value := res.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, res.Body); err != nil {
		reqLogger.Error("Failed to stream document to the browser", slog.String("error", err.Error()))
	}
}

// SearchSignals godoc
//
//	@Summary		Search signals
//	@Description	HTMX endpoint. Returns a table of signals matching the search criteria. Requires ISN read access.
//	@Tags			HTMX Actions
//	@Param			isn-slug					query	string	true	"ISN slug"
//	@Param			signal-type-slug			query	string	true	"Signal type slug"
//	@Param			sem-ver						query	string	true	"Semantic version"
//	@Param			start-date					query	string	false	"Start date (ISO 8601)"
//	@Param			end-date					query	string	false	"End date (ISO 8601)"
//	@Param			account-id					query	string	false	"Filter by account ID"
//	@Param			signal-id					query	string	false	"Filter by signal ID"
//	@Param			local-ref					query	string	false	"Filter by local reference"
//	@Param			include-withdrawn			query	bool	false	"Include withdrawn signals"
//	@Param			include-correlated			query	bool	false	"Include correlated signals"
//	@Param			include-previous-versions	query	bool	false	"Include previous schema versions"
//	@Success		200							"HTML partial"
//	@Failure		400							"HTML error partial"
//	@Failure		401							"HTML error partial"
//	@Router			/ui-api/signals/search [get]
func (s *Server) SearchSignals(w http.ResponseWriter, r *http.Request) {
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
		templ.Handler(templates.ErrorAlert("Please select ISN, signal type, and version.")).ServeHTTP(w, r)
		return
	}

	// Get access token from context
	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("Access token details not found in context")
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	// Get user permissions to determine visibility of the isn being searched
	isnPerms := accessTokenDetails.IsnPerms
	if len(isnPerms) == 0 {
		templ.Handler(templates.ErrorAlert("You don't have permission to access this ISN.")).ServeHTTP(w, r)
		return
	}

	// Perform search using ISN visibility to determine endpoint
	searchResp, err := s.apiClient.SearchSignals(r.Context(), accessTokenDetails.AccessToken, params, isnPerms[params.IsnSlug].Visibility)
	if err != nil {
		reqLogger.Error("Signal search failed", slog.String("error", err.Error()))

		var msg string
		if clientError, ok := err.(*client.ClientError); ok {
			msg = clientError.UserError()
		} else {
			msg = "An error occurred. Please try again."
		}

		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.SearchResults(*searchResp, params)).ServeHTTP(w, r)
}
