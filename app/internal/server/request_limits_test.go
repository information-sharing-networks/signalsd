package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	signalsd "github.com/information-sharing-networks/signalsd/app"
)

func TestRequestSizeLimits(t *testing.T) {
	// Create router with realistic route structure
	router := chi.NewRouter()

	// API routes with 64KB limit
	router.Group(func(r chi.Router) {
		r.Use(RequestSizeLimit(signalsd.DefaultAPIRequestSize))
		r.Post("/api/isn", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	// Signal routes with 50MB limit
	router.Group(func(r chi.Router) {
		r.Use(RequestSizeLimit(50 * 1024 * 1024))
		r.Post("/api/signals", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	tests := []struct {
		name     string
		path     string
		bodySize int64
		wantCode int
	}{
		// API endpoints (64KB limit)
		{"API normal request", "/api/isn", 2 * 1024, http.StatusOK},
		{"API oversized request", "/api/isn", 128 * 1024, 413}, // Request Entity Too Large

		// Signal endpoints (50MB limit)
		{"Signal normal request", "/api/signals", 1024 * 1024, http.StatusOK},
		{"Signal large request", "/api/signals", 10 * 1024 * 1024, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.Repeat("x", int(tt.bodySize))
			req := httptest.NewRequest("POST", tt.path, bytes.NewReader([]byte(body)))
			req.ContentLength = tt.bodySize

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tt.wantCode {
				t.Errorf("got status %d, want %d", rr.Code, tt.wantCode)
			}

			// Verify header is always set
			if header := rr.Header().Get("X-Max-Request-Size"); header == "" {
				t.Error("X-Max-Request-Size header not set")
			}
		})
	}
}
