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
	"github.com/information-sharing-networks/signalsd/app/internal/publicisns"
	"github.com/information-sharing-networks/signalsd/app/internal/router"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
)

// routes used to check that a service mode does not serve endpoints belonging to another mode
const (
	adminRoute       = "GET /api/admin/users"
	signalReadRoute  = "GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search"
	signalWriteRoute = "POST /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals"
)

// forbiddenRoutes lists the routes each service mode must not register. Splitting the service across
// containers is only safe if a mode serves nothing outside its remit - e.g. a container started with
// signals-read must not accept signal writes.
var forbiddenRoutes = map[string][]string{
	"all":           {},
	"api":           {},
	"admin":         {signalReadRoute, signalWriteRoute},
	"signals":       {adminRoute},
	"signals-read":  {adminRoute, signalWriteRoute},
	"signals-write": {adminRoute, signalReadRoute},
}

func TestServiceModeRoutes(t *testing.T) {
	for mode := range signalsd.ValidServiceModes {
		t.Run(mode, func(t *testing.T) {
			forbidden, ok := forbiddenRoutes[mode]
			if !ok {
				t.Fatalf("service mode %q is missing from forbiddenRoutes - add it", mode)
			}

			routes := registeredRoutes(t, mode)

			// the health and version routes are registered in every mode, so a mode that registers
			// nothing else is not serving the endpoints it was asked for
			endpoints := 0
			for route := range routes {
				if !strings.Contains(route, "/health/") && !strings.Contains(route, "/version") {
					endpoints++
				}
			}
			if endpoints == 0 {
				t.Errorf("mode %q registered no routes other than health and version", mode)
			}

			for _, route := range forbidden {
				if routes[route] {
					t.Errorf("mode %q registered %q but should not serve it", mode, route)
				}
			}
		})
	}
}

// registeredRoutes builds a server in the supplied service mode and returns the set of routes it
// registered. The database pool and queries are not used when registering routes so they are left nil.
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
