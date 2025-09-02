package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/server"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/server/isns"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/information-sharing-networks/signalsd/app/internal/version"

	_ "github.com/information-sharing-networks/signalsd/app/docs"
	// the fallback CA certs are required since the service deploys to a scratch docker image that does not include them (the certs are used when the app does external https requests to validate github hosted schemas)
	_ "golang.org/x/crypto/x509roots/fallback"
)

//	@title			Signals ISN API
//	@description	Signals ISN service API for managing Information Sharing Networks
//	@description
//	@description	## Common Error Responses
//	@description	All endpoints may return:
//	@description	- `400` Malformed request (invalid json, missing required fields, etc.)
//	@description	- `401` Unauthorized (invalid credentials)
//	@description	- `403` Forbidden (insufficient permissions)
//	@description	- `413` Request body exceeds size limit
//	@description	- `429` Rate limit exceeded
//	@description	- `500` Internal server error
//	@description
//	@description	Individual endpoints document their specific business logic errors.
//	@description
//	@description	## Request Limits
//	@description	All endpoints are protected by:
//	@description	- **Rate limiting**: Configurable requests per second (default: 100 RPS, 20 burst)
//	@description	- **Request size limits**: 64KB for admin/auth endpoints, 5MB for signal ingestion
//	@description
//	@description	Check the X-Max-Request-Body response header for the configured limit on signals payload.
//	@description
//	@description	The rate limit is set globaly and prevents abuse of the service.
//	@description	In production there will be additional protections in place such as per-IP rate limiting provided by the load balancer/reverse proxy.
//	@description
//	@description	## Authentication & Authorization
//	@description
//	@description	### OAuth
//	@description	The signalsd backend service acts as an OAuth 2.0 Authorization Server and supports web users and service accounts.
//	@description
//	@description	### Authentication Flows
//	@description	- **Web users**: (Refresh Token Grant Type) Authentication via /auth/login → receive JWT access token + HTTP-only refresh cookie → use bearer tokens for API calls
//	@description	- **Service accounts**: Clients implement OAuth Client Credentials flow → receive JWT access token → use bearer tokens for API calls
//	@description
//	@description	### Token Usage
//	@description	All protected API endpoints require a valid JWT access token in the Authorization header:
//	@description	```
//	@description	Authorization: Bearer <jwt-access-token>
//	@description	```
//	@description
//	@description	**Token Refresh (Web Users):**
//	@description	- Client calls `/oauth/token?grant_type=refresh_token` with HTTP-only refresh token cookie
//	@description	- API validates refresh token and issues new access token + rotated refresh cookie
//	@description	- Client receives new bearer token for subsequent API calls
//	@description
//	@description	**Token Refresh (Service Accounts):**
//	@description	- Client calls `/oauth/token?grant_type=client_credentials` with client ID/secret
//	@description	- API validates credentials and issues new access token
//	@description	- Client receives new bearer token for subsequent API calls
//	@description
//	@description	**Token Lifetimes:**
//	@description	- Access tokens: 30 minutes
//	@description	- Refresh tokens: 30 days (web users only)
//	@description
//	@description	### CSRF Protection
//	@description	The refresh token used by the /oauth API endpoints is stored in an HttpOnly cookie (to prevent access by JavaScript)
//	@description	and marked with SameSite=Lax (to prevent it from being sent in cross-site requests, mitigating CSRF).
//	@description
//	@description	### CORS Protection
//	@description
//	@description	CORS is used to control which browser-based clients can make cross-origin requests to the API and read responses.
//	@description
//	@description	By default the server will start with ALLOWED_ORIGINS=*
//	@description
//	@description	In production, you should restrict ALLOWED_ORIGINS to trusted client origins rather than leaving it as *.
//	@description
//	@description	## Date/Time Handling:
//	@description
//	@description	**URL Parameters**: The following ISO 8601 formats are accepted in URL query parameters:
//	@description	- 2006-01-02T15:04:05Z (UTC)
//	@description	- 2006-01-02T15:04:05+07:00 (with offset)
//	@description	- 2006-01-02T15:04:05.999999999Z (nano precision)
//	@description	- 2006-01-02 (date only, treated as start of day UTC: 2006-01-02T00:00:00Z)
//	@description
//	@description	Note: When including a timestamp with a timezone offset in a query parameter, encode the + sign as %2B (e.g. 2025-08-31T12:00:00%2B07:00). Otherwise, + may be interpreted as a space.
//	@description
//	@description	**Response Bodies**: All date/time fields in JSON responses use RFC3339 format (ISO 8601):
//	@description	- Example: "2025-06-03T13:47:47.331787+01:00"
//	@license.name	MIT

//	@servers.url			https://api.example.com
//	@servers.description	Production server
//	@servers.url			http://localhost:8080
//	@servers.description	Development server

//	@accept		json
//	@produce	json

//	@securityDefinitions.ApiKey	BearerAccessToken
//	@in							header
//	@name						Authorization
//	@description				Bearer {JWT access token}

//	@tag.name			auth
//	@tag.description	Authentication and authorization endpoints.

//	@tag.name			Site Admin
//	@tag.description	Site adminstration tools. These endpoints can only be used by the site owner or an admin.

//	@tag.name			ISN Configuration
//	@tag.description	Create and manage Information Sharing Networks (ISNs) - these endpoints can only be used by the site owner or an admin. Note that ISN admins can only view or update details for ISNs they created.

//	@tag.name			ISN Permissions
//	@tag.description	Grant accounts read or write access to an ISN

//	@tag.name			Signal Type Definitions
//	@tag.description	Define the format of the data being shared in an ISN

// @tag.name			Service Accounts
// @tag.description	Manage service account end points
func main() {
	var mode string

	cmd := &cobra.Command{
		Use:   "signalsd",
		Short: "Signalsd service for ISNs",
		Long:  `Signalsd provides APIs for operating a Signals Information Sharing Network`,
		Example: `
  signalsd --mode all           # Single service with all endpoints + UI
  signalsd --mode api           # API endpoints only (no UI)
  signalsd --mode admin         # Admin endpoints only
  signalsd --mode signals       # Signal exchange service (read + write)
  signalsd --mode signals-read  # Signal read operations only
  signalsd --mode signals-write # Signal write operations only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !signalsd.ValidServiceModes[mode] {
				return fmt.Errorf("invalid service mode '%s'. Valid modes: all, api, admin, signals, signals-read, signals-write", mode)
			}

			return run(mode)
		},
	}

	cmd.Flags().StringVarP(&mode, "mode", "m", "", "Service mode (required): all, api, admin, signals, signals-read, signals-write")
	if err := cmd.MarkFlagRequired("mode"); err != nil {
		log.Fatalf("Failed to mark mode flag as required: %v", err)
	}

	v := version.Get()
	cmd.Version = fmt.Sprintf("%s (built %s, commit %s)", v.Version, v.BuildDate, v.GitCommit)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(mode string) error {

	// get site config
	cfg, corsConfigs, err := signalsd.NewServerConfig()
	if err != nil {
		// exit with error
		log.Printf("failed to load configuration: %v", err.Error())
		os.Exit(1)
	}

	appLogger := logger.InitLogger(logger.ParseLogLevel(cfg.LogLevel), cfg.Environment)

	appLogger.Info("Configuration loaded (debug)",
		slog.String("ENVIRONMENT", cfg.Environment),
		slog.String("HOST", cfg.Host),
		slog.Int("PORT", cfg.Port),
		slog.String("LOG_LEVEL", cfg.LogLevel),
		slog.Duration("READ_TIMEOUT", cfg.ReadTimeout),
		slog.Duration("WRITE_TIMEOUT", cfg.WriteTimeout),
		slog.Duration("IDLE_TIMEOUT", cfg.IdleTimeout),
		slog.Int64("MAX_SIGNAL_PAYLOAD_SIZE", cfg.MaxSignalPayloadSize),
		slog.Int("RATE_LIMIT_RPS", int(cfg.RateLimitRPS)),
		slog.Int("RATE_LIMIT_BURST", int(cfg.RateLimitBurst)),
		slog.Int("DB_MAX_CONNECTIONS", int(cfg.DBMaxConnections)),
		slog.Int("DB_MIN_CONNECTIONS", int(cfg.DBMinConnections)),
		slog.Duration("DB_MAX_CONN_LIFETIME", cfg.DBMaxConnLifetime),
		slog.Duration("DB_MAX_CONN_IDLE_TIME", cfg.DBMaxConnIdleTime),
		slog.Duration("DB_CONNECT_TIMEOUT", cfg.DBConnectTimeout),
	)

	// cross-origin resource sharing rules for browser based access to the API (based on the ALLOWED_ORIGINS env config) - the cors middleware will ensure that only the listed partner sites can access the protected endpoints.
	appLogger.Info("CORS allowed origins", slog.Any("origins", cfg.AllowedOrigins))

	if cfg.Environment == "prod" && (len(cfg.AllowedOrigins) == 0 || (len(cfg.AllowedOrigins) == 1 && strings.TrimSpace(cfg.AllowedOrigins[0]) == "*")) {
		appLogger.Warn("production env is configured to allow all origins for CORS. Use the ALLOWED_ORIGINS env variable to restrict access to specific origins")
	}

	// the --mode command line param determines which endpoints should be served: all, admin, signals, signals-read, signals-write, or ui
	cfg.ServiceMode = mode

	// set up the pgx database connection pool
	dbCtx, dbCancel := context.WithTimeout(context.Background(), signalsd.DatabasePingTimeout)
	defer dbCancel()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		appLogger.Error("Failed to parse database URL", slog.String("error", err.Error()))
		os.Exit(1)
	}

	poolConfig.MaxConns = cfg.DBMaxConnections
	poolConfig.MinConns = cfg.DBMinConnections
	poolConfig.MaxConnLifetime = cfg.DBMaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.DBMaxConnIdleTime
	poolConfig.ConnConfig.ConnectTimeout = cfg.DBConnectTimeout

	pool, err := pgxpool.NewWithConfig(dbCtx, poolConfig)
	if err != nil {
		appLogger.Error("Unable to create connection pool", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err = pool.Ping(dbCtx); err != nil {
		appLogger.Error("Error pinging database via pool", slog.String("error", err.Error()))
		os.Exit(1)
	}

	safeURL, _ := removePasswordFromConnectionString(cfg.DatabaseURL)

	appLogger.Info("connected to PostgreSQL", slog.String("url", safeURL))

	// get the sqlc generated database queries
	queries := database.New(pool)

	// set up the signal schema cache - these schemas are stored on the database and used to validate the incoming signals (they are cached to avoid database roundtrips when validating signals)
	schemaCache := schemas.NewSchemaCache()
	if err := schemaCache.Load(dbCtx, queries); err != nil {
		appLogger.Error("Failed to load schema cache", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if schemaCache.Len() > 0 {
		appLogger.Info("Loaded signal schemas into cache", slog.Int("count", schemaCache.Len()))
	} else {
		appLogger.Info("No signal schemas defined for this site")
	}

	// set up the public ISN cache - this is used by the public signal search endpoint (this endpoint can be used by unauthenticated users)
	publicIsnCache := isns.NewPublicIsnCache()
	if err := publicIsnCache.Load(dbCtx, queries); err != nil {
		appLogger.Error("Failed to load public ISN cache", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if publicIsnCache.Len() > 0 {
		appLogger.Info("Loaded public ISNs into cache", slog.Int("count", publicIsnCache.Len()))
	} else {
		appLogger.Info("No public ISNs defined for this site")
	}

	// set up the site level rate limiter (disable if RPS <= 0) - note there are payload size limits in addition to rate limiting and these are set when the routes are created in the server package
	if cfg.RateLimitRPS <= 0 {
		appLogger.Warn("rate limiting disabled")
	}

	// set up the authentication service (this provides functions for managing logins, tokens and auth middleware)
	authService := auth.NewAuthService(cfg.SecretKey, cfg.Environment, queries)

	// run the http server
	appLogger.Info("service mode", slog.String("mode", cfg.ServiceMode))
	appLogger.Info("Starting server", slog.String("version", version.Get().Version))

	server := server.NewServer(
		pool,
		queries,
		authService,
		cfg,
		corsConfigs,
		appLogger,
		schemaCache,
		publicIsnCache,
	)

	// Set up graceful shutdown handling
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	defer server.DatabaseShutdown()

	// Run the server
	if err := server.Start(ctx); err != nil {
		appLogger.Error("Server error", slog.String("error", err.Error()))
		return err
	}

	appLogger.Info("server shutdown complete")
	return nil
}

func removePasswordFromConnectionString(connStr string) (string, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "invalid-connection-string", nil
	}

	if u.User != nil {
		username := u.User.Username()
		u.User = url.User(username)
	}

	return u.String(), nil
}
