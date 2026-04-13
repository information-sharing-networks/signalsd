package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/tidwall/gjson"
	"github.com/tidwall/match"
)

// routingRule holds the matching criteria and target ISN (used to route signals based on their content)
type routingRule struct {

	// matchPattern - when operator is anything other than equals the string is treated as a glob pattern: * = any chars, ? = single char
	matchPattern string

	// operator "matches", "equals", "does_not_match", "does_not_equal"
	operator string

	// isCaseInsensitive when true do a case insensitive match
	isCaseInsensitive bool

	// isnID is the target ISN for signals matching this rule
	isnID uuid.UUID

	// IsnSlug is also held in the cache to avoid DB lookups during signal processing.
	isnSlug string
}

// signalRoutingConfig is the routing configuration for a signal type path.
// A signalRoutingConfig defines the routingField + the RoutingRules for the signal type path.
// For instance:  payload.PortOfEntry + [ "*Felistowe*",... ]
// Ordering of the routes determined by the SQL query and preserved in the slice.
type signalRoutingConfig struct {
	routingField string
	routingRules []routingRule
}

// Cache holds all the routing configs defined on the server.
// The isn route configs are loaded from the database at startup and refreshed periodically by polling.
//
// The key for SignalRoutingConfigs is the signal type path: "{signal-type-slug}/v{semver}".
type Cache struct {
	mu                   sync.RWMutex
	SignalRoutingConfigs map[string]signalRoutingConfig
	db                   *database.Queries
}

func NewCache(db *database.Queries) *Cache {
	return &Cache{
		db:                   db,
		SignalRoutingConfigs: make(map[string]signalRoutingConfig),
	}
}

// Load fetches all routing configs from the database
// and replaces the cache with the latest data
func (c *Cache) Load(ctx context.Context) error {
	rows, err := c.db.GetSignalRoutingConfigs(ctx)
	if err != nil {
		return fmt.Errorf("routing cache: failed to load rules: %w", err)
	}

	configs := make(map[string]signalRoutingConfig) // key = signal type path

	for _, row := range rows {
		key := fmt.Sprintf("%s/v%s", row.SignalTypeSlug, row.SemVer)

		config, exists := configs[key]
		if !exists {
			config = signalRoutingConfig{routingField: row.RoutingField}
		}

		if !signalsd.ValidRouteMatchingOperators[row.Operator] {
			slog.Warn("routing cache: skipping mapping with invalid operator",
				slog.String("signal_type", key),
				slog.String("operator", row.Operator),
				slog.String("pattern", row.MatchPattern),
			)
			continue
		}

		config.routingRules = append(config.routingRules, routingRule{
			matchPattern:      row.MatchPattern,
			operator:          row.Operator,
			isCaseInsensitive: row.IsCaseInsensitive,
			isnID:             row.IsnID,
			isnSlug:           row.IsnSlug,
		})

		configs[key] = config
	}

	c.mu.Lock()
	c.SignalRoutingConfigs = configs
	c.mu.Unlock()

	return nil
}

// StartPolling starts a background goroutine that reloads the routing rules from the
// database every interval. Errors are logged but do not stop the polling loop.
// The goroutine exits when ctx is cancelled.
func (c *Cache) StartPolling(ctx context.Context, interval time.Duration) {

	go func() {

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.Load(ctx); err != nil {
					slog.Error("routing cache: poll refresh failed", slog.String("error", err.Error()))
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *Cache) Len() int {
	return len(c.SignalRoutingConfigs)
}

// Resolve extracts the Routing Field from the json content and returns the target ISN
// for the first matching pattern.
//
// The Routing Field specified in the Routing Config for the supplied Signal Type Path.
//
// If the field resolves to a JSON array, the rule applies if any element matches.
//
// Returns ok=false if no rule exists for the signal type or no pattern matches field content.
// Both isnID and isnSlug are returned from the cache to avoid DB lookups during signal processing.
func (c *Cache) Resolve(signalTypePath string, content json.RawMessage) (isnID uuid.UUID, isnSlug string, ok bool) {
	c.mu.RLock()
	SignalRoutingConfig, exists := c.SignalRoutingConfigs[signalTypePath]
	c.mu.RUnlock()

	if !exists {
		return uuid.Nil, "", false
	}

	result := gjson.GetBytes(content, SignalRoutingConfig.routingField)
	if !result.Exists() {
		return uuid.Nil, "", false
	}

	for _, rule := range SignalRoutingConfig.routingRules {
		if matchesResult(rule, result) {
			return rule.isnID, rule.isnSlug, true
		}
	}

	return uuid.Nil, "", false
}

// matchesResult returns true if the route's criteria match the gjson result.
// For array results, it returns true if any element matches.
func matchesResult(r routingRule, result gjson.Result) bool {
	if result.IsArray() {
		matched := false
		result.ForEach(func(_, v gjson.Result) bool {
			if matchesString(r, v.String()) {
				matched = true
				return false // stop iteration
			}
			return true
		})
		return matched
	}
	return matchesString(r, result.String())
}

func matchesString(r routingRule, s string) bool {
	switch r.operator {
	case "equals":
		if r.isCaseInsensitive {
			return strings.EqualFold(s, r.matchPattern)
		}
		return s == r.matchPattern
	case "matches":
		if r.isCaseInsensitive {
			return match.MatchNoCase(s, r.matchPattern)
		}
		return match.Match(s, r.matchPattern)
	case "does_not_match":
		if r.isCaseInsensitive {
			return !match.MatchNoCase(s, r.matchPattern)
		}
		return !match.Match(s, r.matchPattern)
	case "does_not_equal":
		if r.isCaseInsensitive {
			return !strings.EqualFold(s, r.matchPattern)
		}
		return s != r.matchPattern
	default:
		return false
	}
}
