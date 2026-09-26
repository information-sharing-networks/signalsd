// Package integration contains integration tests for signalsd.
//
// These tests run against a live database and verify the end-to-end behavior of
// authentication, authorization, signal processing, and API functionality.
//
//   - env_setup.go starts the in-process test server and creates a fresh database for each test
//   - helpers_data.go (records created directly in the database)
//   - helpers_auth.go (access tokens and login),
//   - helpers_responses.go (response checks)
//   - helpers_signals.go (the signal APIs)
package integration
