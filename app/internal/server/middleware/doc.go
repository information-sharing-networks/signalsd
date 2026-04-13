// Package middleware provides HTTP middleware constructors for the signalsd server.
//
// Available middleware:
//
//   - CORS: wraps a pre-built [cors.Middleware] to handle cross-origin requests.
//
//   - SecurityHeaders: sets defensive HTTP response headers (CSP, X-Frame-Options,
//     X-Content-Type-Options, Referrer-Policy). Adds HSTS in prod/staging environments.
//
//   - RequestSizeLimit: rejects bodies that exceed a configured byte limit via a 413
//     response, and advertises the limit in the Signalsd-Max-Request-Size header.
//
//   - RateLimit: enforces a global token-bucket rate limit (requests/sec + burst).
//     Disabled when requestsPerSecond <= 0.
package middleware
