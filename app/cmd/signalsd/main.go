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

	// the fallback CA certs are required since the service deploys to a scratch docker image that does not include them (the certs are used when the app does external https requests to validate github hosted schemas)
	_ "golang.org/x/crypto/x509roots/fallback"
)

//	@title			Signals ISN API
//	@description	Signals ISN service API for managing Information Sharing Networks
//	@description
//	@description	## Key concepts
//	@description	- **Information Sharing Network (ISN)**: a network set up for one purpose, such as sharing consignment data between traders and ports. Its members are accounts, each with its own access to the ISN (see _Who can see signals_), and it has the _signal types_ its members exchange.
//	@description	- **Signal**: one piece of information shared through an ISN. There are three kinds (see _Content kinds_): a **json** record about something (e.g. a consignment), a **document** (e.g. a certificate PDF), and an **event** recording that something happened to another signal (e.g. a certificate was approved for a consignment). json signals and documents can be updated as more information becomes available, while events never change. Signals can be linked to each other, e.g. a consignment's documents and events are linked to the consignment (see _Correlation_).
//	@description	- **Signal type**: one type of signal, identified by a slug and a version (e.g. `consignment/v1.0.0`). Each signal type has one content kind, and json and event signal types usually have a JSON schema that the content is checked against. New signal types can be added at any time without affecting existing ones.
//	@description
//	@description	The examples in these docs use consignments moving through ports, but the service is generic: the ISNs, signal types and schemas are defined by each network, so signals can carry any data the participants want to share.
//	@description
//	@description	## API documentation
//	@description	- [/docs](/docs): using the API - authentication, finding ISNs, and submitting and searching signals
//	@description	- [/admin-docs](/admin-docs): administering the service - ISNs and their permissions, signal types, routing, and accounts
//	@description	- [/ui-docs](/ui-docs): the browser-based management interface
//	@description
//	@description	## Common Error Responses
//	@description	All endpoints may return:
//	@description	- `400` Malformed request (invalid json, missing required fields, etc.)
//	@description	- `401` Unauthorized (invalid credentials)
//	@description	- `403` Forbidden (insufficient permissions)
//	@description	- `404` Resource not found (e.g. an ISN or signal type that doesn't exist or is not in use)
//	@description	- `413` Request body exceeds size limit
//	@description	- `429` Rate limit exceeded
//	@description	- `500` Internal server error
//	@description	- `504` Request timed out (`timeout`)
//	@description
//	@description	Individual endpoints document their specific business logic errors.
//	@description
//	@description	Standard error responses include a JSON response body containing `error_code`, `message` and `request_id` fields.
//	@description
//	@description	The /oauth endpoints return `error` (the RFC6749 standard error code), `error_code` and `error_description` fields instead.
//	@description
//	@description	## Request Limits
//	@description	The endpoints are protected by:
//	@description	- **Rate limiting**: Configurable requests per second (all endpoints)
//	@description	- **Request size limits** (configurable - these are the defaults): 64KB for admin/auth endpoints, 5MB for json and event signal submissions, 20MB for document uploads
//	@description
//	@description	Check the `Signalsd-Max-Request-Size` response header for the configured size limit of the endpoint called.
//	@description
//	@description	The rate limit is set globally and prevents abuse of the service.
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
//	@description	- Client calls `POST /oauth/token` with `grant_type=client_credentials` in the form body, and the client ID/secret as form fields or HTTP Basic auth
//	@description	- API validates credentials and issues new access token
//	@description	- Client receives new bearer token for subsequent API calls
//	@description
//	@description	**Login (Web Users):**
//	@description	- Client calls `/api/auth/login` with email and password and receives an access token in the response body. The refresh token is set as an HTTP-only cookie.
//	@description
//	@description	**Token Refresh (Web Users):**
//	@description	- Client calls `POST /oauth/token` with `grant_type=refresh_token` in the form body (the HTTP-only refresh token cookie is sent automatically)
//	@description	- API validates refresh token and issues new access token + rotated refresh cookie
//	@description	- Client receives new bearer token for subsequent API calls
//	@description
//	@description	**Token Lifetimes:**
//	@description	- Access tokens: 30 minutes
//	@description	- Refresh tokens: 30 days (web users only). Each refresh issues a new refresh token valid for another 30 days, so users only need to log in again if they don't use the service for 30 days.
//	@description
//	@description	Each login or refresh revokes the user's other refresh tokens, so a user has one active session at a time.
//	@description
//	@description	The account's role and ISN permissions are included in the access token when it is issued. Changes to ISN permissions (and disabling an account) take effect when the account next gets an access token - up to 30 minutes later.
//	@description	The exception is the admin endpoints, which check the account's current role and status on every request, so role changes and disabled accounts take effect on them straight away.
//	@description
//	@description	Changing or resetting a password ends all the user's sessions (their refresh tokens are revoked), so they will need to log in again.
//	@description
//	@description	### CSRF Protection
//	@description	The refresh token used by the /oauth API endpoints is stored in an HttpOnly cookie (to prevent access by JavaScript)
//	@description	and marked with SameSite=Strict (to prevent it from being sent in cross-site requests, mitigating CSRF).
//	@description	The server also rejects POST, PUT and DELETE requests that the browser reports as coming from another origin, since SameSite treats sibling subdomains as the same site.
//	@description
//	@description	The other API endpoints are authenticated with bearer tokens, which browsers do not send automatically, so they are not open to CSRF.
//	@description
//	@description	## Browser Integrations
//	@description	Browser-based apps on other sites can only call the public endpoints directly.
//	@description
//	@description	The other endpoints have no CORS policy, so web pages on other origins can't use them: the browser won't send an access token to them or let the page read the response.
//	@description	Instead, call the API from your app's backend (or a small proxy) and pass the results to the browser. This is the "backend for frontend" pattern used by the signalsd UI.
//	@description
//	@description	Server-side clients (scripts, services etc) are not affected by CORS and can call any endpoint they have access to.
//	@description
//	@description	## Date/Time Handling
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
//	@description	- **json**: JSON content, validated against the signal type's schema (unless the signal type was set up to skip validation). Sent with *Submit Signals* (or *Submit Signals via Router*).
//	@description	- **document**: a file (PDF, JPEG, PNG or XML). Sent with *Upload a Document* and retrieved with *Download a Document*. Search results contain the document's metadata, not the file.
//	@description	- **event**: an immutable JSON record that a process waypoint has been reached for another signal (e.g. an export health certificate was approved for a consignment). Sent with *Submit Signals* (see Events below).
//	@description
//	@description	Search, withdrawal and batches work the same way for every kind, and search results include each signal's `content_kind`.
//	@description
//	@description	## Versions and resubmissions
//	@description	A signal describes one thing, such as a consignment or a certificate. As more data emerges or corrections are made, you send the signal again and the service stores it as a new version of the same signal, so recipients see the latest version and can see its history.
//	@description
//	@description	You give each signal a `local_ref`, usually its ID in your own system, and send the same `local_ref` every time you send that signal. A `local_ref` is unique for your account, the ISN and the signal type version (sending the same `local_ref` to two ISNs or to two different signal types, creates two independent signals).
//	@description
//	@description	The service gives each signal a `signal_id` when it is first stored. Other participants use it to refer to your signal (e.g. when they want to link to it - see Correlation below).
//	@description
//	@description	When a `local_ref` is sent to the same ISN again:
//	@description	- **If something changed**, a new version of the signal is stored (json and document signals).
//	@description	- **If nothing changed** (the same content, and the same or no `correlation_id`), nothing is stored. The response contains the `signal_id`, `signal_version_id` and `version_number` of the existing latest version, with `unchanged: true`, so requests can be safely retried. JSON content is compared as JSON (key order and whitespace are ignored). Documents compare the file and its filename.
//	@description	- **If the signal was withdrawn**, it is reactivated with a new version (json and document signals).
//	@description	- **Events are never changed**: resubmitting an event with different content or a different `correlation_id`, or after it was withdrawn, fails with `resource_already_exists`. To correct an event, withdraw it and send a new event with a new `local_ref`.
//	@description
//	@description	## Batches
//	@description	Each request also has a `batch_ref`: a label you choose to track your requests, e.g. one per scheduled run. Requests sent with the same `batch_ref` are added to the same batch, and *Get Batch Status* reports what was stored and what failed in it. The `batch_ref` doesn't affect how signals are stored.
//	@description
//	@description	## Correlation
//	@description	A signal can be linked to another signal in the same ISN by setting its `correlation_id` to the other signal's `signal_id`.
//	@description
//	@description	Search results include each signal's `correlation_id` (null if the signal is not linked to another signal).
//	@description
//	@description	Search with `include_correlated=true` returns the signals linked to each result, whatever their type, and the `correlation_id` search filter returns the signals linked to one signal. To fetch the signal a result is linked to, search with `signal_id={correlation_id}` (see *Following signals* below).
//	@description
//	@description	**Correlate to the entity directly.** For example, correlate a consignment's documents and events to the consignment itself, not to each other. Correlation is one level deep: `include_correlated` only returns signals that are correlated directly to the returned signal.
//	@description
//	@description	**Correlating is sharing.** Correlating your signal to a signal created by another account is like emailing that account a copy, but with the extra controls that you can withdraw it or send new versions, and they see those changes. Unlike email:
//	@description	- withdrawing a signal stops further access through the service
//	@description	- if a new version changes the correlation_id, access moves with it: the owner of the newly correlated signal can see every version, and the previous owner can no longer access it through the service
//	@description
//	@description	## Routing
//	@description	Some networks have one ISN per destination, e.g. one per port. Instead of choosing the ISN yourself, you can send signals to the router (*Submit Signals via Router* or *Upload a Document via Router*), which chooses the ISN for each signal:
//	@description	- **A signal with a `correlation_id`** goes to the ISN of the signal it is linked to. Events always have one, and routed documents must have one, so they always go to the ISN of the signal they are about.
//	@description	- **A json signal without one** is matched against its signal type's routing rules, e.g. on a port code in its content. The rules are set up by the service administrators (see [/admin-docs](/admin-docs)).
//	@description
//	@description	The response gives the `isn_slug` each signal was sent to. Combined with *Searching without naming the ISN*, this means your code doesn't need to know which ISNs exist: when a new port joins and your account is given access to its ISN, signals are routed to it and your searches find the responses, with no change to your code.
//	@description
//	@description	A `local_ref` is unique within an ISN, so a resent signal only updates the original if it is routed to the same ISN (see *Submit Signals via Router*).
//	@description
//	@description	## Partial success and resending
//	@description	Signals in a request are validated and stored one at a time: the valid signals are stored and the ones that fail (e.g. content that doesn't match the schema, or an unknown `correlation_id`) are listed in the response with an error code. The response status tells you whether all, some or none of the signals were stored (see each endpoint for the details).
//	@description
//	@description	This is common when testing a new signal type, where some records in a large payload match the schema and others don't. Fix the failed signals and resend them. Signals that are resent unchanged are not stored again, so you can do this in either of these ways:
//	@description	- **Resend the whole request.** Signals that were already stored are returned with `unchanged: true`, and only the fixed signals are stored. Note that any json signal in the request that has been withdrawn since it was sent is reactivated (withdrawn events are not reactivated - they fail with `resource_already_exists`).
//	@description	- **Resend only the failures.** *Get Batch Status* lists the batch's unresolved failures.
//	@description
//	@description	When a whole *Submit Signals* request fails (an error response such as 400, 401, 403, 404, 413 or 500), or you get no response, nothing is recorded in the batch, so resend the whole request once the problem is fixed.
//	@description
//	@description	**Timeouts.** If the server's request timeout is reached partway through a request, the signals already processed stay stored and the rest are returned as `database_error` failures (which may not be recorded in the batch), so resend the whole request.
//	@description
//	@description	## Who can see signals
//	@description	- Accounts with **read** access to an ISN can see every signal in it.
//	@description	- Accounts with **write-only** access can see the signals they created, and the signals other accounts have correlated to their signals (one level deep).
//	@description	- Signals in a **public** ISN can be searched by anyone, using *Signal Search (public ISNs)* (`/api/public/isn/...`). Downloading a document still needs an access token and access to the ISN.
//	@description
//	@description	An account can have different access in each ISN. Searches across several ISNs apply each ISN's rule to the signals in it.
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
//	@description	## Searching without naming the ISN
//	@description	When receiving a signal_id without details of the ISN it is in you can use the following endpoints:
//	@description	```
//	@description	GET /api/signal-types/{signal_type_slug}/v{sem_ver}/signals/search
//	@description	GET /api/signals/search
//	@description	```
//	@description	These searches cover every ISN your account is a member of. Each result includes its `isn_slug`, `signal_type_slug` and `sem_ver`, so you can build further URLs (e.g. *Download a Document*) from it.
//	@description
//	@description	## Polling for changes
//	@description	To find out about new and changed signals, poll the signal types you are interested in, or every signal type:
//	@description	```
//	@description	GET /api/signal-types/{signal_type_slug}/v{sem_ver}/signals/search?updated_since={cursor}&include_withdrawn=true
//	@description	GET /api/signals/search?updated_since={cursor}&include_withdrawn=true
//	@description	```
//	@description	- `updated_since` returns the signals that changed (a new version was received, or the signal was withdrawn) since that time. Results are ordered by `signal_updated_at` across all your ISNs, so the last result's `signal_updated_at` is your cursor.
//	@description	- Use `include_withdrawn=true`, so withdrawn signals are returned with `is_withdrawn: true` (otherwise they silently drop out of the results).
//	@description	- **Polling is at-least-once, so overlap your polls**: set `updated_since` to 1 minute before your cursor, and skip the results you have already seen (the same `signal_id` and `signal_updated_at`). This is necessary because `signal_updated_at` is set when the service starts storing a signal, not when it finishes, so a signal that was still being stored when you last polled can appear with a `signal_updated_at` a little before your cursor. The 1 minute overlap makes sure your next poll picks it up.
//	@description	- Search results are not paginated, so poll often enough to keep each result set small.
//	@description
//	@description	To poll a single ISN, use *Signal Search (private ISNs)* in the same way. The same rules apply, with results ordered by `signal_updated_at` within that ISN:
//	@description	```
//	@description	GET /api/isn/{isn_slug}/signal-types/{signal_type_slug}/v{sem_ver}/signals/search?updated_since={cursor}&include_withdrawn=true
//	@description	```
//	@description
//	@description	## Following signals
//	@description	**You received an event and need the signal it is about.** For example, an `ehc-approved` event is about a consignment, and you want the consignment with its documents and other events. The event's `correlation_id` is the consignment's `signal_id`:
//	@description	```
//	@description	GET /api/signals/search?signal_id={correlation_id}&include_correlated=true&include_withdrawn=true
//	@description	```
//	@description	The result is the consignment, with everything linked to it in. If the consignment was withdrawn, it is returned with `is_withdrawn: true`. Write-only accounts get an empty result if the consignment belongs to another account (see Who can see signals). The same search tells you what any `signal_id` is.
//	@description
//	@description	**You sent signals through the router and want the responses.** For example, you send consignments through the router, and the ports send back advice events linked to them. Poll the advice event type: the events from every port are returned, and each event's `correlation_id` is the `signal_id` the router returned for your consignment.
//	@description	```
//	@description	GET /api/signal-types/{advice_type}/v{sem_ver}/signals/search?updated_since={cursor}&include_withdrawn=true
//	@description	```
//	@description
//	@description	**You keep a copy of the data.** For example, you store every consignment, and the documents and events linked to them. Poll every signal type, so you need one cursor rather than one for each signal type:
//	@description	```
//	@description	GET /api/signals/search?updated_since={cursor}&include_withdrawn=true
//	@description	```
//	@description	- store each result by `signal_id`, and use its `correlation_id` to attach it to the signal it is linked to
//	@description	- if a new version links a signal to a different signal, it is returned with the new `correlation_id`
//	@description	- a linked signal can arrive before the signal it is linked to, so keep it until that signal arrives
//	@description	- signals of new signal types are returned too: skip the types you don't need
//	@description
//	@description	**You want to know when anything about one signal changes.** For example, you want an alert when a consignment gets a new version, or when a document or event is linked to it. Linking a signal to a consignment changes the linked signal, not the consignment, so polling the consignment type with `updated_since` doesn't show new documents or events. Instead, poll the consignment with everything linked to it, and compare the result with the previous poll:
//	@description	```
//	@description	GET /api/signals/search?signal_id={signal_id}&include_correlated=true&include_withdrawn=true
//	@description	```
//	@description	- a `signal_id` that wasn't there before: a signal was linked to the consignment
//	@description	- a different `signal_updated_at`: a new version, or a withdrawal (`is_withdrawn: true`)
//	@description	- a `signal_id` that is no longer there: the signal was linked to a different signal (or can no longer be seen)
//	@description
//	@description	Each poll returns the whole set, so there is no cursor to keep.
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
