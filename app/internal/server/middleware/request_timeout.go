package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/information-sharing-networks/signalsd/app/internal/logger"
)

// originalRequestContextKey stores the original request context (before RequestTimeout added its deadline).
// ExtendRequestTimeout uses it to find out when the client disconnects (the request context is cancelled either way).
type originalRequestContextKey struct{}

// RequestTimeout cancels the request context 1s before the server's write timeout drops the connection,
// so in-flight database queries and goroutines are abandoned cleanly.
// writeTimeout is the server's WRITE_TIMEOUT.
func RequestTimeout(writeTimeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), writeTimeout-time.Second)
			defer cancel()

			ctx = context.WithValue(ctx, originalRequestContextKey{}, r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ExtendRequestTimeout gives the request a longer timeout than RequestTimeout - use it for routes that can take much longer
// than other requests, such as document uploads and downloads (a large document on a slow connection).
//
// It replaces the request context deadline set by RequestTimeout, and extends the connection's read and write deadlines
// (the server's READ_TIMEOUT and WRITE_TIMEOUT would otherwise drop the connection).
// The values in the request context are kept, and the request is still cancelled if the client disconnects.
func ExtendRequestTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			deadline := time.Now().Add(timeout)
			responseController := http.NewResponseController(w)
			if err := responseController.SetReadDeadline(deadline); err != nil {
				logger.ContextRequestLogger(r.Context()).Warn("could not extend the read deadline", slog.String("error", err.Error()))
			}
			if err := responseController.SetWriteDeadline(deadline); err != nil {
				logger.ContextRequestLogger(r.Context()).Warn("could not extend the write deadline", slog.String("error", err.Error()))
			}

			// a context's deadline can't be extended once it is set, so the new context keeps the values of the request
			// context but not its deadline or cancellation (WithoutCancel), with the longer timeout added
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), timeout-time.Second)
			defer cancel()

			// cancel the new context if the client disconnects - the context before RequestTimeout added its deadline
			// is only cancelled when the client disconnects (or the server shuts down)
			requestContext, ok := r.Context().Value(originalRequestContextKey{}).(context.Context)
			if !ok {
				requestContext = r.Context() // RequestTimeout is not in use
			}
			stop := context.AfterFunc(requestContext, cancel)
			defer stop()

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
