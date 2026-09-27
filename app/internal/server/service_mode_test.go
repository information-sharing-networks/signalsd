package server

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/documents"
	"github.com/information-sharing-networks/signalsd/app/internal/publicisns"
	"github.com/information-sharing-networks/signalsd/app/internal/router"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// route groups - every registered route is classified into one of these groups by routeGroup
const (
	adminRoutes       = "admin"        // site and ISN administration, accounts and auth
	signalReadRoutes  = "signal read"  // reading signals and documents
	signalWriteRoutes = "signal write" // submitting and withdrawing signals and documents, and checking batches
	uiRoutes          = "ui"           // the web UI
	otherRoutes       = "other"        // health, version and API docs (served in every mode or not relevant to the split)
)

// TestServiceModeRoutes checks each service mode serves the route groups in its remit and no others.
//
// The service can be split across containers by service mode (e.g. one container for signals-read and another for
// signals-write). That is only safe if each mode serves nothing outside its remit - e.g. a signals-read container
// must not accept signal writes.
//
// The checks apply to every registered route (not a sample), so a route added to the wrong group is caught.
func TestServiceModeRoutes(t *testing.T) {
	expectedGroups := map[string]struct {
		serves       []string // the mode must register at least one route in each of these groups
		mustNotServe []string // the mode must not register any routes in these groups
	}{
		"all": {
			serves: []string{adminRoutes, signalReadRoutes, signalWriteRoutes, uiRoutes},
		},
		"api": {
			serves:       []string{adminRoutes, signalReadRoutes, signalWriteRoutes},
			mustNotServe: []string{uiRoutes},
		},
		"admin": {
			serves:       []string{adminRoutes},
			mustNotServe: []string{signalReadRoutes, signalWriteRoutes, uiRoutes},
		},
		"signals": {
			serves:       []string{signalReadRoutes, signalWriteRoutes},
			mustNotServe: []string{adminRoutes, uiRoutes},
		},
		"signals-read": {
			serves:       []string{signalReadRoutes},
			mustNotServe: []string{adminRoutes, signalWriteRoutes, uiRoutes},
		},
		"signals-write": {
			serves:       []string{signalWriteRoutes},
			mustNotServe: []string{adminRoutes, signalReadRoutes, uiRoutes},
		},
	}

	for mode := range signalsd.ValidServiceModes {
		t.Run(mode, func(t *testing.T) {
			expected, ok := expectedGroups[mode]
			if !ok {
				t.Fatalf("service mode %q is missing from expectedGroups - add it", mode)
			}

			// group the routes the mode registered
			routesByGroup := make(map[string][]string)
			for route := range registeredRoutes(t, mode) {
				group := routeGroup(route)
				routesByGroup[group] = append(routesByGroup[group], route)
			}

			for _, group := range expected.serves {
				if len(routesByGroup[group]) == 0 {
					t.Errorf("mode %q registered no %s routes", mode, group)
				}
			}
			for _, group := range expected.mustNotServe {
				for _, route := range routesByGroup[group] {
					t.Errorf("mode %q registered the %s route %q but should not serve it", mode, group, route)
				}
			}
		})
	}
}

// routeGroup classifies a route ("METHOD /path") into a route group
func routeGroup(route string) string {
	method, path, _ := strings.Cut(route, " ")

	switch {
	case strings.HasPrefix(path, "/static/") || strings.HasPrefix(path, "/ui-api/"):
		return uiRoutes

	// senders check the status of the batches they submitted, so the batch routes are part of signal writes
	case strings.HasPrefix(path, "/api/batches"):
		return signalWriteRoutes

	// signal exchange: the .../signals routes (for both JSON and document signals)
	case strings.HasPrefix(path, "/api/") && strings.Contains(path, "/signals"):
		if method == http.MethodGet {
			return signalReadRoutes
		}
		return signalWriteRoutes

	case strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/oauth/"):
		return adminRoutes

	default:
		return otherRoutes
	}
}

// registeredRoutes builds a server in the supplied service mode and returns the set of routes it registered ("METHOD /path").
// The database and caches are not used when registering routes, so they are created without a database.
func registeredRoutes(t *testing.T, mode string) map[string]bool {
	t.Helper()

	server := NewServer(
		nil,
		nil,
		auth.NewAuthService("test-secret-key", "test", nil),
		&signalsd.ServerEnvironment{
			ServiceMode:    mode,
			Environment:    "test",
			TrustedProxies: 1,
			WriteTimeout:   30 * time.Second,
		},
		&signalsd.CORSConfigs{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		schemas.NewCache(nil),
		publicisns.NewCache(nil),
		router.NewCache(nil),
		documents.NewPostgresStore(nil),
	)

	routes := make(map[string]bool)
	err := chi.Walk(server.router, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		routes[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk routes for mode %q: %v", mode, err)
	}

	return routes
}
