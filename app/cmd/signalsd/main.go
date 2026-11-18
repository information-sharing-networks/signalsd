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
)

//	@title			Signals ISN API
//	@description	Share signals - records, documents and events - between the members of Information Sharing Networks.
//	@description
//	@description	## Key concepts
//	@description	- **Information Sharing Network (ISN)**: a network set up for one purpose, such as sharing consignment data between traders, ports and government agencies. Its members are accounts, each with its own access rights (see *Who can see signals*), and it defines the signal types those members can exchange.
//	@description	- **Signal type**: identified by a slug and a version (e.g. `consignment/v1.0.0`). Each signal type has one of three content kinds: a **json** record typically describes an entity (e.g. a consignment), a **document** is a file (e.g. a certificate PDF), and an **event** records that something happened to another signal (e.g. a certificate was approved). json and event types usually have a JSON schema that the content is checked against. New signal types can be added at any time without affecting existing ones.
//	@description	- **Signal**: one piece of information shared through an ISN. Each signal belongs to one signal type. json signals and documents can be updated with new versions, while events never change. Signals can be linked to each other, e.g. a consignment's documents and events are linked to the consignment (see *Correlation*).
//	@description
//	@description	The examples below use consignments moving through ports, but the service is generic: each network defines its own ISNs, signal types and schemas.
//	@description
//	@description	## API documentation
//	@description	- [/docs](/docs): using the API - authentication, finding ISNs, and submitting and searching signals
//	@description	- [/admin-docs](/admin-docs): administering the service - ISNs and their permissions, signal types, routing, and accounts
//	@description	- [/ui-docs](/ui-docs): the browser-based management interface
//	@description
//	@description	# Authentication
//	@description	Protected endpoints need a JWT access token in the Authorization header:
//	@description	```
//	@description	Authorization: Bearer <access-token>
//	@description	```
//	@description
//	@description	**Service accounts**
//	@description	- **Get a token**: `POST /oauth/token` with `grant_type=client_credentials`, and the client ID and secret as form fields or HTTP Basic auth.
//	@description	- **Renew it**: request a new token in the same way.
//	@description	- **Lifetime**: access tokens last 30 minutes.
//	@description
//	@description	**Web users**
//	@description	- **Get a token**: `POST /api/auth/login` with email and password. The access token is in the response body, and a refresh token is set as an HTTP-only cookie.
//	@description	- **Renew it**: `POST /oauth/token` with `grant_type=refresh_token` (the cookie is sent automatically). The refresh token is rotated.
//	@description	- **Lifetime**: access tokens last 30 minutes. Refresh tokens last 30 days from the last refresh, so users only log in again after 30 days without using the service.
//	@description	- **Sessions**: each login or refresh revokes the user's other refresh tokens, so a user has one session at a time. Changing or resetting a password ends the session (the refresh token is revoked), so the user needs to log in again.
//	@description
//	@description	For both kinds of account, the access token holds the account's role and ISN permissions when it was issued, so permission changes and disabled accounts take effect when the account next gets a token (within 30 minutes). The admin endpoints check the account's current role and status on every request, so changes apply to them straight away.
//	@description
//	@description	# Signals
//	@description
//	@description	## Content kinds
//	@description	Each signal type's `content_kind` decides how its signals are sent:
//	@description	- **json**: sent with *Submit Signals* or *Submit Signals via Router*. The content is validated against the signal type's schema (unless the type was set up to skip validation).
//	@description	- **document**: a PDF, JPEG, PNG or XML file, sent with *Upload a Document* or *Upload a Document via Router* and fetched with *Download a Document*. Search results contain the document's metadata, not the file.
//	@description	- **event**: sent with *Submit Signals*, and validated like json (see *Events* for the extra rules).
//	@description
//	@description	Search, withdrawal and batches work the same way for every kind.
//	@description
//	@description	## References and versions
//	@description	A signal describes one thing, such as a consignment or a certificate. When it changes, you send it again and the service stores a new version of the same signal, so recipients see the latest version and its history.
//	@description	- **`local_ref`**: your reference for the signal, usually its ID in your own system. Send the same `local_ref` every time you send the signal. It is unique for your account, the ISN and the signal type version, so sending the same `local_ref` to two ISNs or two signal types creates two separate signals.
//	@description	- **`signal_id`**: the service's ID for the signal, given when it is first stored. Other participants use it to refer to your signal, e.g. to link to it (see *Correlation*).
//	@description	- **`issuer_ref`** (optional, documents only): the reference given by whoever issued the thing the document represents, e.g. an export health certificate number. It doesn't need to be unique. Recipients can find documents with the `issuer_ref` search filter.
//	@description
//	@description	When you send a `local_ref` to the same ISN again:
//	@description	- **If something changed**, a new version is stored.
//	@description	- **If nothing changed** (the same content, and the same or no `correlation_id`), nothing is stored. The response returns the existing latest version with `unchanged: true`, so requests can be safely retried. JSON is compared as JSON (key order and whitespace are ignored). Documents compare the file, its filename and its `issuer_ref`.
//	@description	- **If the signal was withdrawn**, it is reactivated with a new version.
//	@description	- **Events never change**: resending an event with different content or a different `correlation_id`, or after it was withdrawn, fails with `resource_already_exists`. To correct an event, withdraw it and send a new event with a new `local_ref`.
//	@description
//	@description	## Correlation
//	@description	Link a signal to another signal in the same ISN by setting its `correlation_id` to the other signal's `signal_id`. Search results include each signal's `correlation_id` (null if it isn't linked to another signal).
//	@description	- **Link to the entity directly**: e.g. link a consignment's documents and events to the consignment, not to each other. Correlation is one level deep.
//	@description	- **Linking is sharing**: linking your signal to another account's signal is like emailing that account a copy, except that they see your later versions, and withdrawing the signal stops their access through the service. If a new version changes the `correlation_id`, access moves with it: the owner of the newly linked signal can see every version, and the previous owner can no longer see it.
//	@description
//	@description	## Events
//	@description	An event records that a process waypoint was reached for the signal it is linked to, e.g. `ehc-approved` (an export health certificate was approved) or `departed-origin`. Each waypoint is its own event type, so recipients choose which events they receive by choosing which event types to search or poll.
//	@description	- **`correlation_id` is required**: link the event to the signal it is about (e.g. the consignment). Requests containing events without one are rejected.
//	@description	- **`content` must include `occurred_at`**: when the waypoint was reached, as an RFC 3339 timestamp with a time zone offset (e.g. `2026-09-27T14:02:00Z`).
//	@description	- **`subject`**: if the event is about a specific version of a signal (e.g. version 3 of a document), name it in a `subject` field in `content`: `{"signal_id": "...", "version": 3}`. Always include the version, as later versions may be different.
//	@description	- **Order isn't enforced**: events are accepted in any order and can repeat (e.g. several inspections). To build a timeline, order a signal's linked events by `occurred_at`.
//	@description
//	@description	**Schemas for event types** validate the event's `content`, as for json types, but:
//	@description	- the service checks `occurred_at` whatever the schema says. The schema must still allow it: declare `occurred_at` (and `subject`, if the type has one) and list it as required, especially if the schema uses `"additionalProperties": false`
//	@description	- `occurred_at` and `subject` are reserved - don't use them for anything else
//	@description	- the service doesn't check `subject` yet, so a type whose events have a subject should require it in its schema, as `{"signal_id": <uuid>, "version": <integer>}`
//	@description
//	@description	## Who can see signals
//	@description	- Accounts with **read** access to an ISN can see every signal in it.
//	@description	- Accounts with **write-only** access can see the signals they created, and the signals other accounts have linked to their signals (one level deep).
//	@description	- Signals in a **public** ISN can be searched by anyone, with *Signal Search (public ISNs)* (`/api/public/isn/...`). Downloading a document still needs an access token and access to the ISN.
//	@description
//	@description	An account can have different access in each ISN, and searches across several ISNs apply each ISN's rule to the signals in it.
//	@description
//	@description	Privacy between participants is set by the ISN's permissions. For example, in a network where all participants share their data with a government agency but not with each other, the participants are given write-only access and the agency is given read access.
//	@description
//	@description	## Routing
//	@description	Some networks have one ISN per destination, e.g. one per port. Instead of choosing the ISN yourself, you can send signals to the router (*Submit Signals via Router* or *Upload a Document via Router*), which chooses the ISN for each signal:
//	@description	- **A signal with a `correlation_id`** goes to the ISN of the signal it is linked to.
//	@description	- **A json signal without one** is matched against its signal type's routing rules, e.g. on a port code in its content. The rules are set up by the service administrators (see [/admin-docs](/admin-docs)).
//	@description
//	@description	The response gives the `isn_slug` each signal was sent to.
//	@description
//	@description	Combined with the searches across your ISNs (see *Searching*), this means your code doesn't need to know which ISNs exist: when a new port joins and your account is given access to its ISN, signals are routed to it and your searches find the responses.
//	@description
//	@description	## Batches, partial success and resending
//	@description	Each request has a `batch_ref`: a label you choose to track your requests, e.g. one per scheduled run. Requests with the same `batch_ref` are added to the same batch, and *Get Batch Status* reports what was stored and what failed in it. The `batch_ref` doesn't affect how signals are stored.
//	@description
//	@description	Signals in a request are validated and stored one at a time: the valid signals are stored, and the ones that fail (e.g. content that doesn't match the schema, or an unknown `correlation_id`) are listed in the response with an error code. The response status tells you whether all, some or none of the signals were stored (see each endpoint).
//	@description
//	@description	Fix the failed signals and resend them in either of these ways:
//	@description	- **Resend the whole request.** Signals that were already stored are returned with `unchanged: true`, and only the fixed signals are stored. Note that any json signal in the request that has been withdrawn since it was sent is reactivated (withdrawn events fail with `resource_already_exists`).
//	@description	- **Resend only the failures.** *Get Batch Status* lists the batch's unresolved failures.
//	@description
//	@description	If the whole request fails (an error status such as 400, 401, 403, 404, 413 or 500), or you get no response, nothing is recorded in the batch, so resend the whole request once the problem is fixed. If the request times out partway through, the signals already stored stay stored and the rest are returned as `timeout` failures (not recorded in the batch). Resend the whole request, or just the signals that timed out.
//	@description
//	@description	# Searching
//	@description	There are three search routes for private ISNs. The ISN and signal type in the path limit every filter, so e.g. a `local_ref` search on `consignment/v1.0.0` only finds consignments.
//	@description
//	@description	**One ISN, one signal type:**
//	@description	```
//	@description	GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search
//	@description	```
//	@description
//	@description	**Every ISN you are a member of, one signal type:**
//	@description	```
//	@description	GET /api/signal-types/{signal_type_slug}/v{sem_ver}/signals/search
//	@description	```
//	@description
//	@description	**Every ISN you are a member of, all signal types:**
//	@description	```
//	@description	GET /api/signals/search
//	@description	```
//	@description
//	@description	Each result includes its `isn_slug`, `signal_type_slug` and `sem_ver`, so you can build further URLs from it (e.g. *Download a Document*). To search one ISN across all signal types, use `/api/signals/search` and filter the results on `isn_slug`.
//	@description
//	@description	Searches return the latest version of each signal. Use `include_previous_versions=true` to add earlier versions, `include_correlated=true` to add the signals linked to each result, and `include_withdrawn=true` to include withdrawn signals. The filters are described on each search endpoint.
//	@description
//	@description	## Polling for changes
//	@description	To find out about new and changed signals, poll any of the search routes with `updated_since`, e.g.:
//	@description	```
//	@description	GET /api/signals/search?updated_since={cursor}&include_withdrawn=true
//	@description	```
//	@description	- `updated_since` returns the signals that changed (a new version was received, or the signal was withdrawn) since that time. Results are ordered by `signal_updated_at` across the ISNs searched, so the last result's `signal_updated_at` is your cursor.
//	@description	- Use `include_withdrawn=true`, so withdrawn signals are returned with `is_withdrawn: true` (otherwise they silently drop out of the results).
//	@description	- **Polling is at-least-once, so overlap your polls**: set `updated_since` to 1 minute before your cursor, and skip the results you have already seen (the same `signal_id` and `signal_updated_at`). `signal_updated_at` is set when the service starts storing a signal, not when it finishes, so a signal that was still being stored when you last polled can appear with a `signal_updated_at` a little before your cursor.
//	@description	- Search results are not paginated, so poll often enough to keep each result set small.
//	@description
//	@description	## Following signals
//	@description	**You received an event and need the signal it is about.** For example, an `ehc-approved` event is about a consignment, and you want the consignment with its documents and other events. The event's `correlation_id` is the consignment's `signal_id`:
//	@description	```
//	@description	GET /api/signals/search?signal_id={correlation_id}&include_correlated=true&include_withdrawn=true
//	@description	```
//	@description	The result is the consignment with everything linked to it (with `is_withdrawn: true` if it was withdrawn). The same search tells you what any `signal_id` is. Write-only accounts get an empty result if the consignment belongs to another account (see *Who can see signals*).
//	@description
//	@description	**You sent signals through the router and want the responses.** For example, you send consignments through the router, and the ports send back advice events linked to them. Poll the advice event type: the events from every port are returned, and each event's `correlation_id` is the `signal_id` the router returned for your consignment.
//	@description	```
//	@description	GET /api/signal-types/{advice_type}/v{sem_ver}/signals/search?updated_since={cursor}&include_withdrawn=true
//	@description	```
//	@description
//	@description	**You keep a copy of the data.** For example, you store every consignment, and the documents and events linked to them. Poll every signal type, so you need one cursor rather than one per signal type:
//	@description	```
//	@description	GET /api/signals/search?updated_since={cursor}&include_withdrawn=true
//	@description	```
//	@description	- store each result by `signal_id`, and use its `correlation_id` to attach it to the signal it is linked to (a new version can change the `correlation_id`)
//	@description	- a linked signal can arrive before the signal it is linked to, so keep it until that signal arrives
//	@description	- signals of new signal types are returned too: skip the types you don't need
//	@description
//	@description	**You want to know when anything about one signal changes.** For example, you want an alert when a consignment gets a new version, or when a document or event is linked to it. Linking a signal changes the linked signal, not the consignment, so polling the consignment type with `updated_since` doesn't show new documents or events. Instead, poll the consignment with everything linked to it, and compare the result with the previous poll (there is no cursor to keep):
//	@description	```
//	@description	GET /api/signals/search?signal_id={signal_id}&include_correlated=true&include_withdrawn=true
//	@description	```
//	@description	- a new `signal_id`: a signal was linked to the consignment
//	@description	- a different `signal_updated_at`: a new version, or a withdrawal (`is_withdrawn: true`)
//	@description	- a missing `signal_id`: the signal was linked to a different signal (or can no longer be seen)
//	@description
//	@description	# Reference
//	@description
//	@description	## Error responses
//	@description	All endpoints may return:
//	@description	- `400` malformed request (invalid json, missing required fields, etc.)
//	@description	- `401` unauthorized (invalid or expired credentials)
//	@description	- `403` forbidden (insufficient permissions)
//	@description	- `404` resource not found (e.g. an ISN or signal type that doesn't exist or is not in use)
//	@description	- `413` request body exceeds the size limit
//	@description	- `429` rate limit exceeded
//	@description	- `500` internal server error
//	@description	- `504` request timed out (`timeout`)
//	@description
//	@description	Error responses have a JSON body with `error_code`, `message` and `request_id` fields. The /oauth endpoints return `error` (the RFC 6749 error code), `error_code` and `error_description` instead. Individual endpoints document their specific errors.
//	@description
//	@description	## Request limits
//	@description	- **Rate limit**: a configurable limit on requests per second, applied across all endpoints.
//	@description	- **Request size** (configurable - these are the defaults): 64KB for admin and auth endpoints, 5MB for json and event submissions, 20MB for document uploads. The `Signalsd-Max-Request-Size` response header gives the limit for the endpoint called.
//	@description
//	@description	## Dates and times
//	@description	Query parameters accept these ISO 8601 formats:
//	@description	- `2006-01-02T15:04:05Z` (UTC)
//	@description	- `2006-01-02T15:04:05+07:00` (with offset - encode the `+` as `%2B`, otherwise it may be read as a space)
//	@description	- `2006-01-02T15:04:05.999999999Z` (nanosecond precision)
//	@description	- `2006-01-02` (date only, treated as the start of the day in UTC)
//	@description
//	@description	Dates and times in response bodies use RFC 3339, e.g. `2025-06-03T13:47:47.331787+01:00`.
//	@description
//	@description	## Browser apps
//	@description	Browser-based apps on other sites can only call the public endpoints directly. The other endpoints have no CORS policy, so the browser won't send an access token to them or let the page read the response. Call the API from your app's backend (or a small proxy) instead - the "backend for frontend" pattern used by the signalsd UI. Server-side clients are not affected by CORS.
//	@description
//	@description	The refresh token cookie is HttpOnly and SameSite=Strict, and the server rejects POST, PUT and DELETE requests that the browser reports as coming from another origin. The other endpoints use bearer tokens, which browsers don't send automatically, so they are not open to CSRF.
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
//	@tag.description	Registration, login and password management for web users. A signed-in user can change their own password here; users who have forgotten their password are sent a one-time link by an admin instead (see *Generate Password Reset Link* in the admin API docs). Use the OAuth 2.0 endpoints to obtain access tokens.

//	@tag.name			Service Accounts
//	@tag.description	Manage a service account's own credentials (used for system-to-system access). Service accounts are registered by an admin (see the admin API docs). Use the OAuth 2.0 endpoints to exchange service account credentials for an access token.

//	@tag.name			ISN Discovery
//	@tag.description	Find the Information Sharing Networks (ISNs) and the signal types available in them. Any authenticated account can use these endpoints.

//	@tag.name			Submit Signals
//	@tag.description	Submit json and event signals, upload documents, and withdraw signals

//	@tag.name			Search Signals
//	@tag.description	Search for signals and download documents

//	@tag.name			Batches
//	@tag.description	Track the status of submitted batches

//	@tag.name			ISN Configuration
//	@tag.description	Create and manage Information Sharing Networks (ISNs) and the signal types used in them. These endpoints can only be used by accounts that have a siteadmin or isnadmin role, and ISN admins can only update the ISNs they own.

//	@tag.name			ISN Permissions
//	@tag.description	Grant accounts read or write access to an ISN

//	@tag.name			Signal Types
//	@tag.description	Define the format of the data being shared in an ISN

//	@tag.name			Signals Routing
//	@tag.description	Configure the rules used to route signals of the same type to different ISNs based on their content

//	@tag.name			Service Account Admin
//	@tag.description	Register service accounts and reissue their credentials

//	@tag.name			Account Management
//	@tag.description	Manage user accounts and service accounts

//	@tag.name			Site Admin
//	@tag.description	Service health, version and site tools. The health checks and version endpoints are public. The site reset endpoint only works in environments configured as 'dev'.

//	@tag.name			One-time Links (browser pages)
//	@tag.description	These endpoints should not be called directly. Some admin endpoints, for instance *Register Service Account* and *Generate Password Reset Link*, return a one time link URL that the user then opens in their browser to interact with the auth system. These endpoints return the HTML used in the one-time links. The URLs do not need an access token - the link is itself the credential - and should be treated as secrets.

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
	cfg, err := signalsd.NewServerConfig()
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
