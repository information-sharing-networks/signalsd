package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// deadlineRecorder is a handler that records the deadline and context of the request it receives
type deadlineRecorder struct {
	deadline time.Time
	ctx      context.Context
}

func (d *deadlineRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.deadline, _ = r.Context().Deadline()
	d.ctx = r.Context()
}

func TestRequestTimeout(t *testing.T) {
	recorder := &deadlineRecorder{}
	handler := RequestTimeout(15 * time.Second)(recorder)

	start := time.Now()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	// the context is cancelled 1s before the write timeout
	if expected := start.Add(14 * time.Second); recorder.deadline.Sub(expected).Abs() > time.Second {
		t.Errorf("Expected a deadline of about %v, got %v", expected, recorder.deadline)
	}
}

// addContextValue is a middleware that adds a value to the request context (as the auth middleware adds the token claims)
func addContextValue(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), addedContextKey{}, "added")))
	})
}

type addedContextKey struct{}

func TestExtendRequestTimeout(t *testing.T) {
	recorder := &deadlineRecorder{}
	// ExtendRequestTimeout runs after a middleware that adds a context value
	handler := RequestTimeout(15 * time.Second)(addContextValue(ExtendRequestTimeout(2 * time.Minute)(recorder)))

	start := time.Now()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	t.Run("the deadline is replaced with the longer timeout", func(t *testing.T) {
		if expected := start.Add(2*time.Minute - time.Second); recorder.deadline.Sub(expected).Abs() > time.Second {
			t.Errorf("Expected a deadline of about %v, got %v", expected, recorder.deadline)
		}
	})

	t.Run("values added to the context after RequestTimeout are kept", func(t *testing.T) {
		if value := recorder.ctx.Value(addedContextKey{}); value != "added" {
			t.Errorf("Expected the context value to be kept, got %v", value)
		}
	})

	t.Run("the context is still cancelled if the client disconnects while the request is handled", func(t *testing.T) {
		requestContext, cancelRequest := context.WithCancel(context.Background())
		defer cancelRequest()

		cancelled := false
		handler := RequestTimeout(15 * time.Second)(ExtendRequestTimeout(2 * time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cancelRequest() // the server cancels the request context when the client disconnects
			select {
			case <-r.Context().Done():
				cancelled = true
			case <-time.After(time.Second):
			}
		})))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(requestContext))

		if !cancelled {
			t.Error("Expected the context to be cancelled when the client disconnected")
		}
	})
}
