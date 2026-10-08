package main

import (
	"context"
	"flag"
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
	"github.com/information-sharing-networks/signalsd/app/internal/documents"
	"github.com/information-sharing-networks/signalsd/app/internal/logger"
	"github.com/information-sharing-networks/signalsd/app/internal/publicisns"
	"github.com/information-sharing-networks/signalsd/app/internal/router"
	"github.com/information-sharing-networks/signalsd/app/internal/schemas"
	"github.com/information-sharing-networks/signalsd/app/internal/server"
	signalsd "github.com/information-sharing-networks/signalsd/app/internal/server/config"
	"github.com/information-sharing-networks/signalsd/app/internal/version"
	"github.com/jackc/pgx/v5/pgxpool"

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
//	@description	Standard error responses include a JSON response body containing `error_code` and `error_description` fields
//	@description
//	@description	The /oauth endpoints include an additional `error` field - this contains the RFC6749 standard error code.
//	@description
//	@description	## Request Limits
//	@description	All endpoints are protected by:
//	@description	- **Rate limiting**: Configurable requests per second
//	@description	- **Request size limits**: 64KB for admin/auth endpoints, 5MB for signal ingestion
//	@description
//	@description	Check the Signalsd-Max-Request-Body response header for the configured limit on signals payload.
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
//	@description	- **Web users**: (Refresh Token Grant Type) Authentication via /api/auth/login -> receive JWT access token + HTTP-only refresh cookie -> use bearer tokens for API calls
//	@description	- **Service accounts**: Clients implement OAuth Client Credentials flow -> receive JWT access token -> use bearer tokens for API calls
//	@description
//	@description	### Token Usage
//	@description	All protected API endpoints require a valid JWT access token in the Authorization header:
//	@description	```
//	@description	Authorization: Bearer <jwt-access-token>
//	@description	```
//	@description
//	@description	**Token Refresh (Service Accounts):**
//	@description	- Client calls `/oauth/token?grant_type=client_credentials` with client ID/secret
//	@description	- API validates credentials and issues new access token
//	@description	- Client receives new bearer token for subsequent API calls
//	@description
//	@description	**Login (Web Users):**
//	@description	- Client calls `/api/auth/login` with email and password and receives an access token in the response body. The refresh token is set as an HTTP-only cookie.
//	@description
//	@description	**Token Refresh (Web Users):**
//	@description	- Client calls `/oauth/token?grant_type=refresh_token` with HTTP-only refresh token cookie
//	@description	- API validates refresh token and issues new access token + rotated refresh cookie
//	@description	- Client receives new bearer token for subsequent API calls
//	@description
//	@description	**Token Lifetimes:**
//	@description	- Access tokens: 30 minutes
//	@description	- Refresh tokens: 30 days (web users only)
//	@description
//	@description	### CSRF Protection
//	@description	The refresh token used by the /oauth API endpoints is stored in an HttpOnly cookie (to prevent access by JavaScript)
//	@description	and marked with SameSite=Strict (to prevent it from being sent in cross-site requests, mitigating CSRF).
//	@description
//	@description	### CORS Protection
//	@description
//	@description	CORS is used to control which browser-based clients can make cross-origin requests to the API and read responses.
//	@description
//	@description	the `ALLOWED_ORIGINS` environment variable is used to configure the CORS rules.
//	@description
//	@description	In production, you should restrict ALLOWED_ORIGINS to trusted client origins (the server will not start if it is not set)
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
//	@description
//	@description	# Signals
//	@description
//	@description	## Content kinds
//	@description	Each signal type has a `content_kind`, which determines how its signals are sent:
//	@description	- **json**: JSON content, validated against the signal type's schema. Sent with *Submit Signals* (or *Submit Signals via Router*).
//	@description	- **document**: a file (PDF, JPEG, PNG or XML). Sent with *Upload a Document* and retrieved with *Download a Document*. Search results contain the document's metadata, not the file.
//	@description	- **event**: an immutable JSON record that a process waypoint has been reached for another signal (e.g. an export health certificate was approved for a consignment). Sent with *Submit Signals* (see Events below).
//	@description
//	@description	Search, withdrawal and batches work the same way for every kind, and search results include each signal's `content_kind`.
//	@description
//	@description	## Versions and resubmissions
//	@description	Each signal is identified by the `local_ref` the sender supplies, which must be unique for the account, ISN and signal type (sending the same `local_ref` to two ISNs creates two independent signals). When a `local_ref` is sent to the same ISN again:
//	@description	- **If something changed**, a new version of the signal is stored (json and document signals).
//	@description	- **If nothing changed** (the same content, and the same or no `correlation_id`), nothing is stored. The response contains the `signal_id`, `signal_version_id` and `version_number` of the existing latest version, with `unchanged: true`, so requests can be safely retried. JSON content is compared as JSON (key order and whitespace are ignored). Documents compare the file and its filename.
//	@description	- **If the signal was withdrawn**, it is reactivated with a new version (json and document signals).
//	@description	- **Events are never changed**: resubmitting an event with different content or a different `correlation_id`, or after it was withdrawn, fails with `resource_already_exists`. To correct an event, withdraw it and send a new event with a new `local_ref`.
//	@description
//	@description	## Recovering from failures
//	@description	Each signal in a request is stored separately, so a request that fails part way through (e.g. a timeout) can leave some signals stored and others not. Because unchanged signals are not stored again, you can recover in either of these ways:
//	@description	- **Resend the whole request.** Signals that were already stored are returned with `unchanged: true`, and only the missing or failed signals are stored. Note that any signal in the request that has been withdrawn since it was sent is reactivated.
//	@description	- **Resend only the failures.** The response lists the signals that failed, and *Get Batch Status* lists the batch's unresolved failures.
//	@description
//	@description	When a whole request fails (400, 401, 403, 413 or 500), or you get no response (e.g. a timeout), no failures are recorded in the batch, so resend the whole request once the problem is fixed.
//	@description
//	@description	A retry sent while the original request is still being processed can occasionally store an extra, identical version. This is harmless - the content doesn't change - but pollers will see one more update. To avoid it, set your client timeout above the server's request timeout: 15 seconds by default, or 2 minutes for document uploads.
//	@description
//	@description	## Correlation
//	@description	A signal can be linked to another signal in the same ISN by setting its `correlation_id` to the other signal's `signal_id`. Search with `include_correlated=true` returns the signals linked to each result, and the `correlation_id` search filter returns the signals linked to one signal.
//	@description
//	@description	Search results include each signal's `correlation_id` (null if the signal is not linked to another signal). To fetch the signal it is linked to (e.g. the consignment an event is about), search that signal's type with `signal_id=<correlation_id>`.
//	@description
//	@description	**Correlate to the entity directly.** For example, correlate a consignment's documents and events to the consignment itself, not to each other. Correlation is one level deep: `include_correlated` only returns signals that are correlated directly to the returned signal.
//	@description
//	@description	**Correlating is sharing.** Correlating your signal to a signal created by another account is like emailing that account a copy, with the extra controls that you can withdraw it or send new versions, and they see those changes. Unlike email:
//	@description	- withdrawing a signal stops further access through the service
//	@description	- if a new version changes the correlation_id, access moves with it: the owner of the newly correlated signal can see every version, and the previous owner can no longer access it through the service
//	@description
//	@description	## Who can see signals
//	@description	- Accounts with **read** access to an ISN can see every signal in it.
//	@description	- Accounts with **write-only** access can see the signals they created, and the signals other accounts have correlated to their signals (one level deep).
//	@description	- Signals in a **public** ISN can be searched by anyone.
//	@description
//	@description	Privacy between participants is set by the ISN's permissions. For example, in a network where all participants share their data with a government agency but not with each other, the participants are given write-only access and the agency is given read access.
//	@description
//	@description	## Events
//	@description	Event signals record that a process waypoint has been reached - e.g. `ehc-approved` (an export health certificate was approved) or `departed-origin` - for the signal they are correlated to. Each waypoint is its own event signal type, so recipients choose which events they receive by choosing which event types to search or poll.
//	@description	- **`correlation_id` is required**: correlate the event to the signal it is about (e.g. the consignment). Requests containing events without one are rejected.
//	@description	- **`content` must include `occurred_at`**: the time the waypoint was reached, as an RFC 3339 timestamp with a time zone offset (e.g. `2026-09-27T14:02:00Z`).
//	@description	- **`subject`**: if the event is about a specific version of a signal (e.g. version 3 of a document), name it in a `subject` field in `content`: `{"signal_id": "...", "version": 3}`. Always include the version - later versions of the document may be different.
//	@description	- **Events are immutable** (see Versions and resubmissions).
//	@description	- The service doesn't enforce an order: events are accepted in any order and can repeat (e.g. several inspections). To build a timeline, order a signal's correlated events by `occurred_at`.
//	@description
//	@description	Example: `{"local_ref": "ehc-approved-001", "correlation_id": "<consignment signal_id>", "content": {"occurred_at": "2026-09-27T14:02:00Z", "subject": {"signal_id": "<document signal_id>", "version": 3}, "certificate_no": "EHC-001"}}`
//	@description
//	@description	**Schemas for event types.** An event type's schema validates the event's `content`, as for json signal types, but:
//	@description	- the service checks `occurred_at` whatever the schema says. The schema must still allow it: declare `occurred_at` (and `subject`, if the type has one) and list it as required, especially if the schema uses `"additionalProperties": false`
//	@description	- `occurred_at` and `subject` are reserved - don't use them for anything else
//	@description	- the service doesn't check `subject` yet, so a type whose events have a subject should require it in its schema, as `{"signal_id": <uuid>, "version": <integer>}`
//	@description
//	@description	## Polling for changes
//	@description	To find out about new and changed signals, poll *Signal Search* for each signal type you are interested in:
//	@description	```
//	@description	GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search?updated_since={cursor}&include_withdrawn=true
//	@description	```
//	@description	- `updated_since` returns the signals that were created, given a new version, recorrelated or withdrawn since that time. Results are ordered by `signal_updated_at`, so the last result's `signal_updated_at` is your cursor.
//	@description	- Use `include_withdrawn=true`, so withdrawn signals are returned with `is_withdrawn: true` (otherwise they silently drop out of the results).
//	@description	- **Polling is at-least-once, so overlap your polls**: set `updated_since` to 1 minute before your cursor, and skip the results you have already seen (the same `signal_id` and `signal_updated_at`).
//	@description	- Why the overlap is needed: `signal_updated_at` is set when the service starts storing a signal, not when it finishes. A signal that was still being stored when you last polled can therefore appear with a `signal_updated_at` a little before your cursor. The 1 minute overlap makes sure your next poll picks it up.
//	@description	- Search results are not paginated, so poll often enough to keep each result set small.
//	@description
//	@description	# UI Endpoints
//	@description
//	@description	UI endpoints serve the browser-based management interface.
//	@description
//	@description	## Route types
//	@description	- **Page handlers** (`GET /admin/*`, `/search`, etc.): return a full HTML page (`200`)
//	@description	- **HTMX action handlers** (`PUT`/`POST` to `/ui-api/*`): return an HTML partial (`200`) - either a success or error alert fragment
//	@description
//	@description	- see /ui-docs for more information
//	@description
//	@description	## Auth
//	@description	All protected routes require a valid session (cookie-based). Unauthenticated requests redirect to `/login`.
//	@description	Requests with insufficient role are redirected to `/access-denied`.
//	@description
//	@description	## Role requirements
//	@description	- **Public** (`/login`, `/register`): no authentication required
//	@description	- **Authenticated**: any logged-in user (`/dashboard`, `/settings`, and `/search` for accounts with access to an ISN)
//	@description	- **`isnadmin` or `siteadmin`**: the admin dashboard, ISN creation, enabling and disabling ISNs, service account creation
//	@description	- **Admins of at least one ISN**: ISN access, the ISN's signal types, viewing signal type configuration
//	@description	- **`siteadmin` only**: account management (enabling and disabling accounts, password reset links, reissuing service account credentials), ISN ownership transfer, role management, signal type creation and schemas, signal routing rules
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

//	@tag.name			OAuth 2.0
//	@tag.description	Access token issuance and revocation. The signalsd backend acts as an OAuth 2.0 Authorization Server: service accounts use the client_credentials grant, web users use the refresh_token grant. Start here if you are connecting a system to the API.

//	@tag.name			User Authentication
//	@tag.description	Registration, login and password management for web users. A signed-in user can change their own password here; users who have forgotten their password are sent a one-time link by an admin instead (see *Generate Password Reset Link* under Account Management. Use the OAuth 2.0 endpoints to obtain access tokens.

//	@tag.name			Service Accounts
//	@tag.description	Register service accounts and manage their credentials (used for system-to-system access). Use the OAuth 2.0 endpoints to exchange service account credentials for an access token.

//	@tag.name			Signal Exchange
//	@tag.description	Submit, withdraw and search signals, and track the status of submitted batches

//	@tag.name			Signals Routing
//	@tag.description	Configure the rules used to route signals of the same type to different ISNs based on their content

//	@tag.name			Signal Types
//	@tag.description	Define the format of the data being shared in an ISN

//	@tag.name			ISN Configuration
//	@tag.description	Create and manage Information Sharing Networks (ISNs) - these endpoints can only be used by the accounts that have a siteadmin or isnadmin role Note that ISN admins can only view or update details for ISNs they created.

//	@tag.name			ISN Permissions
//	@tag.description	Grant accounts read or write access to an ISN

//	@tag.name			Account Management
//	@tag.description	Manage user accounts and service accounts

//	@tag.name			Site Admin
//	@tag.description	Service health, version and site tools. The health checks and version endpoints are public. The site reset endpoint only works in environments configured as 'dev'.

//	@tag.name			One-time Links (browser pages)
//	@tag.description	These endpoints should not be called directly. Some admin endpoints,  for insance *Register Service Account* and *Generate Password Reset Link*, return a one time link URL that the user then opens in their browser to interact with the auth system. These endpoints return the HTML used to in the one-time links. The URLs do not need an access token - the link is itself the credential - and should be treated as secrets.

//	@tag.name			UI Pages
//	@tag.description	Browser-based management interface. Page handlers return full HTML

//	@tag.name			HTMX Actions
//	@tag.description	HTMX action handlers (/ui-api/*) return HTML partials

const usageText = `Signalsd provides APIs for operating a Signals Information Sharing Network

To start the service, use the 'run' command with a service mode:

  signalsd run all           # Single service with all endpoints + UI
  signalsd run api           # API endpoints only (no UI)
  signalsd run admin         # Admin endpoints only
  signalsd run signals       # Signal exchange service (read + write)
  signalsd run signals-read  # Signal read operations only
  signalsd run signals-write # Signal write operations only

Usage:
  signalsd run MODE

Flags:
  --version   version for signalsd
`

func main() {
	showVersion := flag.Bool("version", false, "print version")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usageText) }
	flag.Parse()

	if *showVersion {
		v := version.Get()
		fmt.Printf("%s (built %s, commit %s)\n", v.Version, v.BuildDate, v.GitCommit)
		return
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(2)
	}

	switch flag.Arg(0) {
	case "run":
		runSubcmd(flag.Args()[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}
}

func runSubcmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		os.Exit(2)
	}

	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}

	mode := fs.Arg(0)
	if !signalsd.ValidServiceModes[mode] {
		fmt.Fprintf(os.Stderr, "invalid mode %q\nValid modes: all, api, admin, signals, signals-read, signals-write\n", mode)
		os.Exit(2)
	}

	if err := run(mode); err != nil {
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

	appLogger.Info("Configuration loaded",
		slog.String("ENVIRONMENT", cfg.Environment),
		slog.String("HOST", cfg.Host),
		slog.Int("PORT", cfg.Port),
		slog.String("PUBLIC_BASE_URL", cfg.PublicBaseURL),
		slog.String("LOG_LEVEL", cfg.LogLevel),
		slog.Duration("READ_TIMEOUT", cfg.ReadTimeout),
		slog.Duration("WRITE_TIMEOUT", cfg.WriteTimeout),
		slog.Duration("IDLE_TIMEOUT", cfg.IdleTimeout),
		slog.Duration("DOCUMENT_TRANSFER_TIMEOUT", cfg.DocumentTransferTimeout),
		slog.Int64("MAX_SIGNAL_PAYLOAD_SIZE", cfg.MaxSignalPayloadSize),
		slog.Int64("MAX_DOCUMENT_SIZE", cfg.MaxDocumentSize),
		slog.String("DOCUMENT_STORE", cfg.DocumentStore),
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

	if len(cfg.AllowedOrigins) == 0 || (len(cfg.AllowedOrigins) == 1 && strings.TrimSpace(cfg.AllowedOrigins[0]) == "*") {
		appLogger.Warn("env is configured to allow all origins for CORS. Use the ALLOWED_ORIGINS env variable to restrict access to specific origins")
	}

	// the mode command line param determines which endpoints should be served: all, admin, signals, signals-read, signals-write, or ui
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
	schemaCache := schemas.NewCache(queries)
	if err := schemaCache.Load(dbCtx); err != nil {
		appLogger.Error("Failed to load schema cache", slog.String("error", err.Error()))
		os.Exit(1)
	}

	appLogger.Info("Loaded signal schema cache", slog.Int("count", schemaCache.Len()))

	// set up the public ISN cache - this is used by the public signal search endpoint (this endpoint can be used by unauthenticated users)
	publicIsnCache := publicisns.NewCache(queries)
	if err := publicIsnCache.Load(dbCtx); err != nil {
		appLogger.Error("Failed to load public ISN cache", slog.String("error", err.Error()))
		os.Exit(1)
	}

	appLogger.Info("Loaded public ISN cache", slog.Int("count", publicIsnCache.Len()))

	// set up the router cache - holds compiled ISN routing config, polled for changes every CachePollInterval
	signalRouterCache := router.NewCache(queries)
	if err := signalRouterCache.Load(dbCtx); err != nil {
		appLogger.Error("Failed to load router cache", slog.String("error", err.Error()))
		os.Exit(1)
	}
	appLogger.Info("Loaded ISN router cache", slog.Int("count", signalRouterCache.Len()))

	// set up the document store - holds the content of document signals (e.g. PDFs)
	// S3 support is TODO
	var documentStore documents.Store
	switch cfg.DocumentStore {
	case signalsd.DocumentStorePostgres:
		documentStore = documents.NewPostgresStore(queries)
	default:
		appLogger.Error("unexpected DOCUMENT_STORE", slog.String("DOCUMENT_STORE", cfg.DocumentStore))
		os.Exit(1)
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
		signalRouterCache,
		documentStore,
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
