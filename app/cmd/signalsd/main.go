package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/information-sharing-networks/signalsd/app/internal/auth"
	"github.com/information-sharing-networks/signalsd/app/internal/database"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/server"
	"github.com/information-sharing-networks/signalsd/app/internal/server/schemas"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	signalsd "github.com/information-sharing-networks/signalsd/app"
	"github.com/information-sharing-networks/signalsd/app/internal/version"

	_ "github.com/information-sharing-networks/signalsd/app/docs"
)

//	@title			Signals ISN API
//	@version		1.0
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
//	@description	### Authentication Flow
//	@description	- **Web users**: Login → get JWT + refresh cookie → use JWT for API calls
//	@description	- **Service accounts**: Authenticate with Client credentials → get JWT → use JWT for API calls → re-authenticate when expired
//	@description
//	@description	### Authorization
//	@description	All protected API endpoints expect valid JWT access tokens containing user identity and permissions.
//	@description
//	@description	tokens should be supplied using:
//	@description	**Authorization header**: `Bearer <token>`
//	@description
//	@description	**Token refresh:**
//	@description	- **Web users**: Refresh tokens (HTTP-only cookies) automatically renew access tokens
//	@description	- **Service accounts**: Must re-authenticate with client credentials when tokens expire
//	@description
//	@description	Access tokens expire in 30 minutes
//	@description
//	@description	Refresh tokens expire in 30 days (web users only)
//  @description
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
//	@tag.description	Authentication and authorization endpoints. Web users get JWT + refresh tokens, service accounts use client credentials to get JWT access tokens.

//	@tag.name			Site admin
//	@tag.description	Site adminstration tools

//	@tag.name			ISN configuration
//	@tag.description	Manage the Information Sharing Networks that are used to exchange signals between participating users.

//	@tag.name			ISN Permissions
//	@tag.description	Grant accounts read or write access to an ISN

//	@tag.name			ISN view
//	@tag.description	View information about the configured ISNs

//	@tag.name			Signal definitions
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
	cmd.MarkFlagRequired("mode")

	// Version flag is handled automatically by Cobra
	v := version.Get()
	cmd.Version = fmt.Sprintf("%s (built %s, commit %s)", v.Version, v.BuildDate, v.GitCommit)

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runServer(mode string) error {
	serverLogger := logger.InitServerLogger()

	cfg := signalsd.NewServerConfig(serverLogger)
	cfg.ServiceMode = mode

	httpLogger := logger.InitHttpLogger(cfg.LogLevel, cfg.Environment)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		serverLogger.Fatal().Err(err).Msg("Failed to parse database URL")
	}

	// perf testing config
	if cfg.Environment == "perf" {
		poolConfig.MaxConns = 50
		poolConfig.MinConns = 10
		poolConfig.MaxConnLifetime = 30 * time.Minute
		poolConfig.MaxConnIdleTime = 15 * time.Minute
		poolConfig.ConnConfig.ConnectTimeout = 5 * time.Second
	}
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
		serverLogger.Fatal().Err(err).Msg("Failed to load schema cache")
	}
	serverLogger.Info().Msg("Schema cache loaded")

	if cfg.RateLimitRPS <= 0 {
		serverLogger.Warn().Msg("rate limiting disabled")
	}

	authService := auth.NewAuthService(cfg.SecretKey, cfg.Environment, queries)
	router := chi.NewRouter()

	server := server.NewServer(pool, queries, authService, cfg, serverLogger, httpLogger, router)

	serverLogger.Info().Msgf("CORS allowed origins: %v", cfg.AllowedOrigins)
	serverLogger.Info().Msgf("service mode: %s", cfg.ServiceMode)
	serverLogger.Info().Msgf("Starting server (version: %s)", version.Get().Version)

	server.Run()

	serverLogger.Info().Msg("server shutdown complete")
	return nil
}

func removePasswordFromConnectionString(connStr string) (string, error) {
	u, _ := url.Parse(connStr)
	if u.User != nil {
		username := u.User.Username()
		u.User = url.User(username)
	}

	return u.String(), nil
}
