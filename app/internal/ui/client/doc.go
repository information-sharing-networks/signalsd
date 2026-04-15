// Package client allows the frontend-facing server to fetch data from the signalsd API.
//
// The client owns request construction, auth headers, base URL, response decoding, and error wrapping.
// All signalsd API calls go through this client.
//
// Auth tokens are extracted from the request context by the server layer (see [auth.RequireAuth])
// and passed into client methods, which attach them as Bearer Authorization headers.
// See server/doc.go for the full BFF token management flow.
package client
