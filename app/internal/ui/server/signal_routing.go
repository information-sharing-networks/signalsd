package server

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/client"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/templates"
	"github.com/information-sharing-networks/signalsd/app/internal/ui/types"
)

// ManageSignalRoutingPage godoc
//
//	@Summary		Manage ISN routing page
//	@Description	Renders the Signals Routing Rules management page. Requires siteadmin role.
//	@Tags			UI Pages
//	@Success		200	"HTML page"
//	@Router			/admin/signal-types/routing [get]
func (s *Server) ManageSignalRoutingPage(w http.ResponseWriter, r *http.Request) {
	reqLogger := s.logger.With(slog.String("handler", "ManageSignalRoutingPage"))

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	// Signal types are site-level — load all registered types (deduplicated by slug).
	signalTypes, err := s.apiClient.GetSignalTypes(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("failed to get signal types", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("Failed to load signal types. Please try again.")).ServeHTTP(w, r)
		return
	}

	seen := make(map[string]bool)
	slugs := make([]types.SignalTypeSlug, 0, len(signalTypes))
	for _, st := range signalTypes {
		if !seen[st.Slug] {
			seen[st.Slug] = true
			slugs = append(slugs, types.SignalTypeSlug{Slug: st.Slug})
		}
	}

	templ.Handler(templates.ManageSignalRoutingPage(s.config.Environment, slugs)).ServeHTTP(w, r)
}

// loadIsnOptions fetches all active ISNs and converts them to IsnOption for template use.
func (s *Server) loadIsnOptions(accessToken string) ([]types.IsnOption, error) {
	isns, err := s.apiClient.GetIsns(accessToken, false)
	if err != nil {
		return nil, err
	}
	options := make([]types.IsnOption, len(isns))
	for i, isn := range isns {
		options[i] = types.IsnOption{Slug: isn.Slug}
	}
	return options, nil
}

// renderRoutingForm fetches existing routing rules and ISN options, then renders the routing form.
func (s *Server) renderRoutingForm(w http.ResponseWriter, r *http.Request, slug, semVer string) {
	reqLogger := s.logger.With(slog.String("handler", "renderRoutingForm"))

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	existing, err := s.apiClient.GetIsnRouting(accessTokenDetails.AccessToken, slug, semVer)
	if err != nil {
		reqLogger.Error("failed to load routing rules", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("Failed to load routing rules. Please try again.")).ServeHTTP(w, r)
		return
	}

	isnOptions, err := s.loadIsnOptions(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("failed to load ISNs", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("Failed to load ISNs. Please try again.")).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.SignalRoutingForm(slug, semVer, existing, isnOptions)).ServeHTTP(w, r)
}

// SaveSignalRoutingConfig godoc
//
//	@Summary		Save Signals Routing Rules
//	@Description	HTMX endpoint. Saves routing rules for a signal type version.
//	@Tags			HTMX Actions
//	@Param			signal-type-slug	formData	string		true	"signal type slug"				example(sample-signal-type)
//	@Param			sem-ver				formData	string		true	"version"						example(1.0.0)
//	@Param			routing-field		formData	string		true	"gjson path to routing field"	example(payload.portOfEntry)
//	@Param			match-patterns		formData	[]string	true	"match pattern per row"			example(*felixstowe*)
//	@Param			operators			formData	[]string	true	"operator per row"				enums(matches,equals,does_not_match,does_not_equal)
//	@Param			case-insensitive	formData	[]string	true	"'true' or 'false' per row"
//	@Param			isn-slugs			formData	[]string	true	"target ISN slug per row"	example(felixstowe-isn)
//	@Success		200					"HTML partial"
//	@Failure		400					"HTML error partial"
//	@Failure		401					"HTML error partial"
//	@Router			/ui-api/signal-types/routing [put]
func (s *Server) SaveSignalRoutingConfig(w http.ResponseWriter, r *http.Request) {
	reqLogger := s.logger.With(slog.String("handler", "SaveIsnRouting"))

	if err := r.ParseForm(); err != nil {
		templ.Handler(templates.ErrorAlert("Invalid form data.")).ServeHTTP(w, r)
		return
	}

	slug := strings.TrimSpace(r.FormValue("signal-type-slug"))
	semVer := strings.TrimSpace(r.FormValue("sem-ver"))
	if slug == "" || semVer == "" {
		templ.Handler(templates.ErrorAlert("Signal type and version are required.")).ServeHTTP(w, r)
		return
	}

	routingField := strings.TrimSpace(r.FormValue("routing-field"))
	if routingField == "" {
		templ.Handler(templates.ErrorAlert("Routing field is required.")).ServeHTTP(w, r)
		return
	}

	patterns := r.Form["match-patterns"]
	operators := r.Form["operators"]
	caseInsensitiveFlags := r.Form["case-insensitive"]
	isnSlugs := r.Form["isn-slugs"]

	routingRules := make([]client.SignalRoutingRule, 0, len(patterns))
	var sequence int32
	for i := range patterns {
		pattern := strings.TrimSpace(patterns[i])
		isn := strings.TrimSpace(isnSlugs[i])
		if pattern == "" || isn == "" {
			reqLogger.Error("pattern or isn is empty")
			continue // skip empty rows
		}
		sequence++
		routingRules = append(routingRules, client.SignalRoutingRule{
			MatchPattern:     pattern,
			Operator:         strings.TrimSpace(operators[i]),
			IsCaseInsensitve: caseInsensitiveFlags[i] == "true",
			IsnSlug:          isn,
			Sequence:         sequence,
		})
	}

	if len(routingRules) == 0 {
		templ.Handler(templates.ErrorAlert("At least one mapping is required.")).ServeHTTP(w, r)
		return
	}

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	result, err := s.apiClient.UpdateSignalRoutingConfig(accessTokenDetails.AccessToken, slug, semVer, client.UpdateSignalRoutingConfigRequest{
		RoutingField: routingField,
		RoutingRules: routingRules,
	})
	if err != nil {
		reqLogger.Error("failed to save routing rules", slog.String("error", err.Error()))
		msg := "An error occurred saving the routing rules. Please try again."
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		}
		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	templ.Handler(templates.SignalRoutingSaveSuccess(result)).ServeHTTP(w, r)
}

// DeleteSignalRoutingConfig godoc
//
//	@Summary		Delete Signals Routing Rules
//	@Description	HTMX endpoint. Deletes all routing rules for a signal type version.
//	@Tags			HTMX Actions
//	@Param			signal-type-slug	formData	string	true	"signal type slug"	example(sample-signal-type)
//	@Param			sem-ver				formData	string	true	"version"			example(1.0.0)
//	@Success		200					"HTML partial"
//	@Failure		400					"HTML error partial"
//	@Failure		401					"HTML error partial"
//	@Router			/ui-api/signal-types/routing [delete]
func (s *Server) DeleteSignalRoutingConfig(w http.ResponseWriter, r *http.Request) {
	reqLogger := s.logger.With(slog.String("handler", "DeleteSignalRoutingConfig"))

	slug := r.FormValue("signal-type-slug")
	semVer := r.FormValue("sem-ver")
	if slug == "" || semVer == "" {
		templ.Handler(templates.ErrorAlert("Signal type and version are required.")).ServeHTTP(w, r)
		return
	}

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		templ.Handler(templates.ErrorAlert("Authentication required. Please log in again.")).ServeHTTP(w, r)
		return
	}

	if err := s.apiClient.DeleteSignalRoutingConfig(accessTokenDetails.AccessToken, slug, semVer); err != nil {
		reqLogger.Error("failed to delete routing rules", slog.String("error", err.Error()))
		msg := "An error occurred. Please try again."
		if ce, ok := err.(*client.ClientError); ok {
			msg = ce.UserError()
		}
		templ.Handler(templates.ErrorAlert(msg)).ServeHTTP(w, r)
		return
	}

	s.renderRoutingForm(w, r, slug, semVer)
}

// LoadSignalRoutingForm godoc
//
//	@Summary		Load signals routing form
//	@Description	HTMX endpoint. Renders the routing rule form for the selected signal type version.
//	@Description	Returns empty 200 if either param is missing (htmx cascade not yet complete).
//	@Tags			HTMX Actions
//	@Param			signal-type-slug	query	string	true	"signal type slug"	example(sample-signal-type)
//	@Param			sem-ver				query	string	true	"version"			example(1.0.0)
//	@Success		200					"HTML partial"
//	@Router			/ui-api/signal-types/routing [get]
func (s *Server) LoadSignalRoutingForm(w http.ResponseWriter, r *http.Request) {
	slug := r.FormValue("signal-type-slug")
	semVer := r.FormValue("sem-ver")
	if slug == "" || semVer == "" {
		return
	}
	s.renderRoutingForm(w, r, slug, semVer)
}

// RemoveRoutingRow godoc
//
//	@Summary		Remove routing mapping row
//	@Description	HTMX endpoint. Returns empty content; the caller uses hx-swap="outerHTML" to remove the row.
//	@Tags			HTMX Actions
//	@Success		200	"empty"
//	@Router			/ui-api/signal-types/routing/remove-row [get]
func (s *Server) RemoveRoutingRow(w http.ResponseWriter, r *http.Request) {
	// Returning 200 with empty body causes the targeted <tr> to be replaced with nothing.
	w.WriteHeader(http.StatusOK)
}

// AddRoutingRow godoc
//
//	@Summary		Add routing mapping row
//	@Description	HTMX endpoint. Returns a new empty mapping row to append to the form table.
//	@Description	The optional count param is used to set the initial sequence label in the rendered row.
//	@Tags			HTMX Actions
//	@Success		200	"HTML partial"
//	@Router			/ui-api/signal-types/routing/add-row [get]
func (s *Server) AddRoutingRow(w http.ResponseWriter, r *http.Request) {
	reqLogger := s.logger.With(slog.String("handler", "AddRoutingRow"))

	countStr := r.URL.Query().Get("count")
	count, err := strconv.Atoi(countStr)
	if err != nil || count < 0 {
		count = 0
	}

	accessTokenDetails, ok := auth.ContextAccessTokenDetails(r.Context())
	if !ok {
		reqLogger.Error("failed to read accessTokenDetails from context")
		return
	}

	isnOptions, err := s.loadIsnOptions(accessTokenDetails.AccessToken)
	if err != nil {
		reqLogger.Error("failed to load ISNs", slog.String("error", err.Error()))
		templ.Handler(templates.ErrorAlert("Failed to load ISNs. Please try again.")).ServeHTTP(w, r)
		return
	}
	templ.Handler(templates.RoutingMappingRow(count+1, client.SignalRoutingRule{}, isnOptions)).ServeHTTP(w, r)
}
