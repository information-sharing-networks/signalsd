// Package middleware provides HTTP middleware constructors for the signalsd server.
//
// Available middleware:
//
//   - PublicCORS: allows browser-based clients on any origin to read the public endpoints (no credentials).
//     The other API endpoints have no CORS policy, so browsers block cross-origin requests to them.
//
//   - SecurityHeaders: sets defensive HTTP response headers (CSP, X-Frame-Options,
//     X-Content-Type-Options, Referrer-Policy). Adds HSTS in prod/staging environments.
//
//   - NoCache: sets Cache-Control: no-cache so browsers revalidate before reusing a cached
//     response. Used for the API documentation and OpenAPI specs, which change on every deployment.
//
//   - RequestSizeLimit: rejects bodies that exceed a configured byte limit via a 413
//     response, and advertises the limit in the Signalsd-Max-Request-Size header.
//
//   - RateLimit: enforces a global token-bucket rate limit (requests/sec + burst).
//     Disabled when requestsPerSecond <= 0.
//
//   - RequestTimeout: cancels the request context just before the server's write timeout drops the connection.
//
//   - ExtendRequestTimeout: gives the routes in a group a longer timeout than RequestTimeout (used for document uploads
//     and downloads).
package middleware
