// Package server provides the main HTTP server implementation for signalsd.
//
// The Server type coordinates all service components including database access,
// authentication, caching, and routing. It manages both the signalsd API and
// optionally the integrated UI server.
package server
