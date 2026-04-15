package handlers

// these handlers support the management of the rules to route signals to ISNs based on their content

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/information-sharing-networks/signalsd/app/internal/apperrors"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/responses"
	"github.com/information-sharing-networks/signalsd/app/internal/router"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RoutingConfigHandler struct {
	queries           *database.Queries
	pool              *pgxpool.Pool
	signalRouterCache *router.Cache
	schemaCache       *schemas.Cache
}

func NewRoutingConfigHandler(queries *database.Queries, pool *pgxpool.Pool, signalRouterCache *router.Cache, schemaCache *schemas.Cache) *RoutingConfigHandler {
	return &RoutingConfigHandler{queries: queries, pool: pool, signalRouterCache: signalRouterCache, schemaCache: schemaCache}
}

// SignalRoutingRule is the mapping between a pattern and a isn.
// When linked to a signal type/Routing field this forms part of the Isn route config
type SignalRoutingRule struct {
	MatchPattern     string `json:"match_pattern" example:"*felixstowe*"`
	Operator         string `json:"operator" enums:"matches,equals,does_not_match,does_not_equal" example:"matches"`
	IsCaseInsensitve bool   `json:"is_case_insensitive" example:"true"`
	IsnSlug          string `json:"isn_slug" example:"felixstowe-isn"`
	Sequence         int32  `json:"sequence" example:"1"`
}

// UpdateSignalRoutingConfigRequest replaces the full rule + mapping set for a signal type path
type UpdateSignalRoutingConfigRequest struct {
	RoutingField string              `json:"routing_field" example:"payload.portOfEntry"`
	RoutingRules []SignalRoutingRule `json:"routing_rules"`
}

// SignalRoutingConfigResponse contains the full set of isn routes for a signal type path
type SignalRoutingConfigResponse struct {
	SignalTypePath string              `json:"signal_type_path" example:"sample-signal-type/v1.0.0"`
	RoutingField   string              `json:"routing_field" example:"payload.PorfOfEntry"`
	RoutingRules   []SignalRoutingRule `json:"routing_rules"`
}

// GetSignalRoutingConfig godoc
//
//	@Summary		Get Signals Routing Config
//
//	@Description	Returns the Signals Routing Rules for a signal type.
//
//	@Tags			Signals Routing
//
//	@Param			signal_type_slug	path		string	true	"signal type slug"	example(sample-signal-type)
//	@Param			sem_ver				path		string	true	"version"			example(1.0.0)
//
//	@Success		200					{object}	handlers.SignalRoutingConfigResponse
//	@Failure		404					{object}	responses.ErrorResponse
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/signal-types/{signal_type_slug}/v{sem_ver}/routes [get]
//
// Should only be used with RequireRole (siteadmin) middleware.
func (h *RoutingConfigHandler) GetSignalRoutingConfig(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	routesConfig, err := h.queries.GetSignalRoutingConfigBySignalType(r.Context(), database.GetSignalRoutingConfigBySignalTypeParams{
		SignalTypeSlug: slug,
		SemVer:         semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("no routing rule found for %s/v%s", slug, semVer))
			return
		}
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	dbRules, err := h.queries.GetIsnRoutesByFieldID(r.Context(), routesConfig.ID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	res := SignalRoutingConfigResponse{}

	res.SignalTypePath = fmt.Sprintf("%s/v%s", slug, semVer)

	res.RoutingField = routesConfig.RoutingField

	rules := make([]SignalRoutingRule, len(dbRules))
	for i, rule := range dbRules {
		rules[i] = SignalRoutingRule{
			MatchPattern:     rule.MatchPattern,
			IsnSlug:          rule.IsnSlug,
			Operator:         rule.Operator,
			IsCaseInsensitve: rule.IsCaseInsensitive,
			Sequence:         rule.RuleSequence,
		}
	}
	res.RoutingRules = rules
	responses.RespondWithJSON(w, http.StatusOK, res)
}

// UpdateSignalRoutingConfig godoc
//
//	@Summary		Update Signals Routing Config
//
//	@Description	Replaces the route config for the specified signal type path
//	@Description
//	@Description	the routing_field must be a plain Dot Notation path -
//	@Description	under the covers the service uses gjson paths, however the special patern matching symbols (*?#@|!()[]%<>=) are not currently allowed.
//	@Description
//	@Description	When using the 'matches' and 'not matches' operator, any occurance of '*' and '?' in the matching pattern will be treated as a wildcard.
//	@Description	The pattern is always compared to the full contents of the specified routing field.
//	@Description
//	@Description	Patterns are matched in order according to the supplied sequence number (smallest sequence first)
//	@Description	and the first match is accepted. Where the routing field is an array, as long as one or more elements match
//	@Description	the match is accepted.
//
//	@Param			signal_type_slug	path	string										true	"signal type slug"	example(sample-signal-type)
//	@Param			sem_ver				path	string										true	"version"			example(1.0.0)
//	@Param			request				body	handlers.UpdateSignalRoutingConfigRequest	true	"routing config"
//
//	@Success		204
//	@Failure		400	{object}	responses.ErrorResponse
//	@Failure		404	{object}	responses.ErrorResponse
//
//	@Security		BearerAccessToken
//
//	@Router			/api/admin/signal-types/{signal_type_slug}/v{sem_ver}/routes [put]
//
// Should only be used with RequireRole (siteadmin) middleware.
func (h *RoutingConfigHandler) UpdateSignalRoutingConfig(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	defer r.Body.Close()
	var req UpdateSignalRoutingConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "invalid JSON body")
		return
	}

	if req.RoutingField == "" {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "routing_field is required")
		return
	}

	// prevent the use of chars with special meaning in gjson
	if strings.ContainsAny(req.RoutingField, `*?#@|!()[]%<>=`) {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "routing_field must be a plain JSON path (e.g. payload.portOfEntry) - wildcards and gjson operators are not supported")
		return
	}

	// prevent numeric path segments (gjson array index access e.g. payload.0.item)
	for seg := range strings.SplitSeq(req.RoutingField, ".") {
		if _, err := strconv.Atoi(seg); err == nil {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "routing_field must not contain numeric segments - routing by array index is not supported")
			return
		}
	}

	if len(req.RoutingRules) == 0 {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, "at least one mapping is required")
		return
	}

	// Validate routes
	for i, rule := range req.RoutingRules {

		if rule.MatchPattern == "" || rule.IsnSlug == "" {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("mapping[%d]: match_pattern and isn_slug are required", i))
			return
		}

		if !signalsd.ValidRouteMatchingOperators[rule.Operator] {
			responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("invalid route matching operator %s", rule.Operator))
			return
		}
	}

	// Resolve the signal type and all ISN slugs before starting the transaction.
	signalType, err := h.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   slug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("signal type %s/v%s not found", slug, semVer))
			return
		}
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// Validate the routing field against the cached schema for this signal type.
	// The cache is guaranteed to be populated for any existing signal type, so a
	// miss here is an unexpected internal error rather than a user error.
	signalTypePath := fmt.Sprintf("%s/v%s", slug, semVer)
	fieldExists, err := h.schemaCache.FieldPathExistsInSchema(signalTypePath, req.RoutingField)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeInternalError, "schema cache error")
		return
	}
	if !fieldExists {
		responses.RespondWithError(w, r, http.StatusBadRequest, apperrors.ErrCodeMalformedBody, fmt.Sprintf("routing_field %q is not defined in the schema for %s", req.RoutingField, signalTypePath))
		return
	}

	// check slugs exist and get the ids
	isnIDs := make([]database.Isn, len(req.RoutingRules))
	for i, rule := range req.RoutingRules {
		isn, err := h.queries.GetIsnBySlug(r.Context(), rule.IsnSlug)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("mapping[%d]: ISN %q not found", i, rule.IsnSlug))
				return
			}
			logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
		isnIDs[i] = isn
	}

	//  start transaction
	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(),
			slog.String("error", err.Error()),
		)

		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	defer func() {
		if err := tx.Rollback(r.Context()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.ContextWithLogAttrs(r.Context(),
				slog.String("error", err.Error()),
			)

		}
	}()

	txQueries := h.queries.WithTx(tx)

	// Delete the old routes
	if _, err := txQueries.DeleteSignalRoutingConfigBySignalTypeID(r.Context(), signalType.ID); err != nil {
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// create the new routing field entry
	signalRoutingConfig, err := txQueries.CreateSignalRoutingConfig(r.Context(), database.CreateSignalRoutingConfigParams{
		SignalTypeID: signalType.ID,
		RoutingField: req.RoutingField,
	})
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	for i, rule := range req.RoutingRules {
		if _, err := txQueries.CreateIsnRoute(r.Context(), database.CreateIsnRouteParams{
			SignalRoutingConfigID: signalRoutingConfig.ID,
			MatchPattern:          rule.MatchPattern,
			Operator:              rule.Operator,
			IsCaseInsensitive:     rule.IsCaseInsensitve,
			IsnID:                 isnIDs[i].ID,
			RuleSequence:          rule.Sequence,
		}); err != nil {
			logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
			responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	responses.RespondWithStatusCodeOnly(w, http.StatusNoContent)
}

// DeleteSignalRoutingConfig godoc
//
//	@Summary		Delete Signals Routing Config
//
//	@Description	Removes all routing information for a signal type version.
//
//	@Tags			Signals Routing
//
//	@Param			signal_type_slug	path	string	true	"signal type slug"	example(sample-signal-type)
//	@Param			sem_ver				path	string	true	"version"			example(1.0.0)
//
//	@Success		204
//	@Failure		404	{object}	responses.ErrorResponse
//	@Security		BearerAccessToken
//	@Router			/api/admin/signal-types/{signal_type_slug}/v{sem_ver}/routes [delete]
//
// Should only be used with RequireRole (siteadmin) middleware.
func (h *RoutingConfigHandler) DeleteSignalRoutingConfig(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("signal_type_slug")
	semVer := r.PathValue("sem_ver")

	signalType, err := h.queries.GetSignalTypeBySlugAndVersion(r.Context(), database.GetSignalTypeBySlugAndVersionParams{
		Slug:   slug,
		SemVer: semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("signal type %s/v%s not found", slug, semVer))
			return
		}
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// check there is a routing config
	_, err = h.queries.GetSignalRoutingConfigBySignalType(r.Context(), database.GetSignalRoutingConfigBySignalTypeParams{
		SignalTypeSlug: slug,
		SemVer:         semVer,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			responses.RespondWithError(w, r, http.StatusNotFound, apperrors.ErrCodeResourceNotFound, fmt.Sprintf("no routing rule found for %s/v%s", slug, semVer))
			return
		}
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	_, err = h.queries.DeleteSignalRoutingConfigBySignalTypeID(r.Context(), signalType.ID)
	if err != nil {
		logger.ContextWithLogAttrs(r.Context(), slog.String("error", err.Error()))
		responses.RespondWithError(w, r, http.StatusInternalServerError, apperrors.ErrCodeDatabaseError, "database error")
		return
	}

	// refresh the cache for this instance (polling will catch-up the other instances eventually)
	if err := h.signalRouterCache.Load(r.Context()); err != nil {
		logger.ContextWithLogAttrs(r.Context(), slog.String("router_cache_reload_error", err.Error()))
	}

	responses.RespondWithStatusCodeOnly(w, http.StatusNoContent)
}
