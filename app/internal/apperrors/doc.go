// Package apperrors defines the typed errors used by signalsd handlers and
// middleware. Handlers return these errors and the responses package renders them
// as sanitised JSON to the client.
//
// Two error types are defined:
//
//   - HTTPError - generic handler errors
//
//   - OAuthError - RFC 6749 §5.2 compliant responses for the OAuth 2.0
//     token and revocation endpoints.
//
// Use the constructors (NotFound, MalformedBody, OAuthInvalidGrant, etc.)
// rather than building the structs directly. Pass nil for err when there is
// no underlying Go error to wrap (e.g validation errors)
package apperrors
