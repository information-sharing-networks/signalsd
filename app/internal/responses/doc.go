// Package responses handles the formatting and writing of HTTP responses for handlers.
//
// Handlers return errors from the apperrors package, which responses translates into
// sanitised JSON responses with appropriate HTTP status codes.
//
// Detailed error messages are logged on the server.
package responses
