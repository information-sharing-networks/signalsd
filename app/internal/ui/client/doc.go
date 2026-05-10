// Package client allows the frontend-facing server to fetch data from the signalsd API.
//
// The client owns request construction, auth headers, base URL, response decoding, and error wrapping.
// All signalsd API calls go through this client.
//
// Auth tokens are extracted from the request context by the server layer (see [auth.RequireAuth])
// and passed into client methods, which attach them as Bearer Authorization headers.
// See server/doc.go for the full BFF token management flow.
//
// All client methods accept a context.Context. The context is used
// for cancellation propagation (e.g. browser disconnect cancels the in-flight API call) and
// for forwarding the incoming request ID as an X-Request-Id header so that the UI and API
// log entries for the same user action share the same request_id.
package client
