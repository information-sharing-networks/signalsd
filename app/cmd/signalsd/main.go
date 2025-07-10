package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/server"
	"github.com/information-sharing-networks/signalsd/app/internal/server/isns"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	signalsd "github.com/information-sharing-networks/signalsd/app"
	"github.com/information-sharing-networks/signalsd/app/internal/version"

	_ "github.com/information-sharing-networks/signalsd/app/docs"
)

//	@title			Signals ISN API
//	@description	Signals ISN service API for managing Information Sharing Networks
//	@description
//	@description	## Common Error Responses
//	@description	All endpoints may return:
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
//	@description	This API serves as an OAuth 2.0 Authorization Server for multiple client applications. The server supports web users and service accounts.
//	@description
//	@description	### Authentication Flows
//	@description	- **Web users**: Direct authentication via /auth/login → receive JWT access token + HTTP-only refresh cookie → use bearer tokens for API calls
//	@description	- **Service accounts**: Clients implement OAuth Client Credentials flow → receive JWT access token → use bearer tokens for API calls
//	@description
//	@description	### Token Usage
//	@description	All protected API endpoints require valid JWT access tokens in the Authorization header:
//	@description	```
//	@description	Authorization: Bearer <jwt-access-token>
//	@description	```
//	@description
//	@description	**Token Refresh (Web Users):**
//	@description	- Client calls `/oauth/token?grant_type=refresh_token` with both bearer token AND HTTP-only cookie
//	@description	- API validates both credentials and issues new access token + rotated refresh cookie
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
//	@description	The /oauth API endpoints use a http-only cookie to exchange refresh tokens but also require a bearer token, preventing CSRF attacks.
//	@description
//	@description	### CORS Protection
//	@description 	By default the server will start with ALLOWED_ORIGINS=*
//	@description
//	@description	This should not be used in production - you must to specify the list of client origins that are allowed to access the API in the ALLOWED_ORIGINS environment variable before starting the server.
//	@description
//	@description	## Date/Time Handling:
//	@description
//	@description	**URL Parameters**: The following ISO 8601 formats are accepted in URL query parameters:
//	@description	- 2006-01-02T15:04:05Z (UTC)
//	@description	- 2006-01-02T15:04:05+07:00 (with offset)
//	@description	- 2006-01-02T15:04:05.999999999Z (nano precision)
//	@description	- 2006-01-02 (date only, treated as start of day UTC: 2006-01-02T00:00:00Z)
//	@description
//	@description	Note: If the timestamp contains a timezone offset (as in +07:00), the + must be percent-encoded as %2B in the query.
//	@description
//	@description	**Response Bodies**: All date/time fields in JSON responses use RFC3339 format (ISO 8601):
//	@description	- Example: "2025-06-03T13:47:47.331787+01:00"
//	@license.name	MIT

//	@servers.url		https://api.example.com
//	@servers.description	Production server
//	@servers.url		http://localhost:8080
//	@servers.description	Development server

//	@accept		json
//	@produce	json

//	@securityDefinitions.ApiKey	BearerAccessToken
//	@in							header
//	@name						Authorization
//	@description				Bearer {JWT access token}

//	@tag.name			auth
//	@tag.description	Authentication and authorization endpoints.

//	@tag.name			Site admin
//	@tag.description	Site adminstration tools

//	@tag.name			ISN configuration
//	@tag.description	Manage the Information Sharing Networks that are used to exchange signals between participating users.

//	@tag.name			ISN Permissions
//	@tag.description	Grant accounts read or write access to an ISN

//	@tag.name			ISN details
//	@tag.description	View information about the configured ISNs

//	@tag.name			Signal types
//	@tag.description	Define the format of the data being shared in an ISN

// @tag.name			Service accounts
// @tag.description		Manage service account end points
func main() {
	var mode string

	cmd := &cobra.Command{
		Use:   "signalsd",
		Short: "Signalsd service for ISNs",
		Long:  `Signalsd provides APIs for operating a Signals Information Sharing Network`,
		Example: `
  signalsd --mode all           # Single service with all endpoints
  signalsd --mode admin         # Admin service only
  signalsd --mode signals       # Signal exchange service (read + write)
  signalsd --mode signals-read  # Signal read operations only
  signalsd --mode signals-write # Signal write operations only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate mode
			if !signalsd.ValidServiceModes[mode] {
				return fmt.Errorf("invalid service mode '%s'. Valid modes: all, admin, signals, signals-read, signals-write", mode)
			}

			return runServer(mode)
		},
	}

	// Add flags
	cmd.Flags().StringVarP(&mode, "mode", "m", "", "Service mode (required): all, admin, signals, signals-read, or signals-write")
	if err := cmd.MarkFlagRequired("mode"); err != nil {
		log.Fatalf("Failed to mark mode flag as required: %v", err)
	}

	// Version flag is handled automatically by Cobra
	v := version.Get()
	cmd.Version = fmt.Sprintf("%s (built %s, commit %s)", v.Version, v.BuildDate, v.GitCommit)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServer(mode string) error {
	serverLogger := logger.InitServerLogger()

	cfg, corsConfigs, err := signalsd.NewServerConfig(serverLogger)
	if err != nil {
		serverLogger.Fatal().Err(err).Msg("Failed to load configuration")
	}
	cfg.ServiceMode = mode

	httpLogger := logger.InitHttpLogger(cfg.LogLevel, cfg.Environment)

	ctx, cancel := context.WithTimeout(context.Background(), signalsd.DatabasePingTimeout)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		serverLogger.Fatal().Err(err).Msg("Failed to parse database URL")
	}

	poolConfig.MaxConns = cfg.DBMaxConnections
	poolConfig.MinConns = cfg.DBMinConnections
	poolConfig.MaxConnLifetime = cfg.DBMaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.DBMaxConnIdleTime
	poolConfig.ConnConfig.ConnectTimeout = cfg.DBConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		serverLogger.Fatal().Err(err).Msg("Unable to create connection pool")
	}

	if err = pool.Ping(ctx); err != nil {
		serverLogger.Fatal().Err(err).Msg("Error pinging database via pool")
	}

	safeURL, _ := removePasswordFromConnectionString(cfg.DatabaseURL)

	serverLogger.Info().Msgf("connected to PostgreSQL at %v", safeURL)

	queries := database.New(pool)

	if err := schemas.LoadSchemaCache(ctx, queries); err != nil {
		serverLogger.Fatal().Msgf("Failed to load schema cache: %v", err)
	}
	serverLogger.Info().Msg("Schema cache loaded")

	if err := isns.LoadPublicISNCache(ctx, queries); err != nil {
		serverLogger.Fatal().Msgf("Failed to load public ISN cache: %v", err)
	}

	if isns.PublicISNCount() >= 0 {
		serverLogger.Info().Msg("Public ISN cache loaded")
	}

	if cfg.RateLimitRPS <= 0 {
		serverLogger.Warn().Msg("rate limiting disabled")
	}

	authService := auth.NewAuthService(cfg.SecretKey, cfg.Environment, queries)
	router := chi.NewRouter()

	server := server.NewServer(pool, queries, authService, cfg, corsConfigs, serverLogger, httpLogger, router)

	serverLogger.Info().Msgf("CORS allowed origins: %v", cfg.AllowedOrigins)

	if cfg.Environment == "prod" && (len(cfg.AllowedOrigins) == 0 || (len(cfg.AllowedOrigins) == 1 && strings.TrimSpace(cfg.AllowedOrigins[0]) == "*")) {
		serverLogger.Warn().Msg("production env is configured to allow all origins for CORS. Use the ALLOWED_ORIGINS env variable to restrict access to specific origins")
	}

	serverLogger.Info().Msgf("service mode: %s", cfg.ServiceMode)
	serverLogger.Info().Msgf("Starting server (version: %s)", version.Get().Version)

	server.Run()

	serverLogger.Info().Msg("server shutdown complete")
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
