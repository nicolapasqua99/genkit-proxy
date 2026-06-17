package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// contextKey is an unexported type for context keys in this package to avoid
// collisions with keys defined in other packages.
type contextKey int

const (
	requestIDKey contextKey = iota
	modelKey
)

// modelSlot is a mutable holder for the model name. Logger stores a pointer to
// one in the request context; the proxy Handler writes the decoded model name
// into it so Logger can include it in the access log entry after the request
// completes.
type modelSlot struct{ name string }

// Recover wraps next so that a panic in a handler is logged and converted into
// a clean 500 JSON response instead of dropping the connection. If the response
// has already started (headers written), only logs — the status cannot be
// changed at that point. Re-panics on http.ErrAbortHandler to preserve stdlib
// semantics.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, httpReq *http.Request) {
		tracked := &statusWriter{ResponseWriter: writer}
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				slog.ErrorContext(httpReq.Context(), "panic recovered",
					"err", recovered,
					"request_id", requestIDFromContext(httpReq.Context()),
				)
				if !tracked.wroteHeader {
					writeJSON(tracked, http.StatusInternalServerError, errorBody{Error: "internal server error"})
				}
			}
		}()
		next.ServeHTTP(tracked, httpReq)
	})
}

// RequestID reads X-Request-ID from the inbound request (or generates a UUID
// v4), stores it in the request context, and echoes it as X-Request-ID in the
// response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, httpReq *http.Request) {
		id := httpReq.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.New().String()
		}
		writer.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(httpReq.Context(), requestIDKey, id)
		next.ServeHTTP(writer, httpReq.WithContext(ctx))
	})
}

// Logger logs each completed request with method, path, status, latency,
// request ID, and model using slog. It never logs the Authorization header.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, httpReq *http.Request) {
		start := time.Now()
		slot := &modelSlot{}
		ctx := context.WithValue(httpReq.Context(), modelKey, slot)
		tracked := &statusWriter{ResponseWriter: writer}
		next.ServeHTTP(tracked, httpReq.WithContext(ctx))

		code := tracked.code
		if code == 0 {
			code = http.StatusOK
		}
		attrs := []any{
			"method", httpReq.Method,
			"path", httpReq.URL.Path,
			"status", code,
			"latency_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFromContext(ctx),
		}
		if slot.name != "" {
			attrs = append(attrs, "model", slot.name)
		}
		slog.InfoContext(ctx, "request", attrs...)
	})
}

// requestIDFromContext returns the request ID stored by RequestID, or an empty
// string when no ID is present.
func requestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// modelSlotFromContext returns the model slot stored by Logger, or nil when the
// Logger middleware is not in the chain.
func modelSlotFromContext(ctx context.Context) *modelSlot {
	slot, _ := ctx.Value(modelKey).(*modelSlot)
	return slot
}

// statusWriter tracks whether any response bytes have been sent and the first
// status code written, so middleware can avoid writing a second status after the
// body has started.
type statusWriter struct {
	http.ResponseWriter
	code        int
	wroteHeader bool
}

func (writer *statusWriter) WriteHeader(code int) {
	if !writer.wroteHeader {
		writer.code = code
		writer.wroteHeader = true
		writer.ResponseWriter.WriteHeader(code)
	}
}

func (writer *statusWriter) Write(data []byte) (int, error) {
	writer.wroteHeader = true
	return writer.ResponseWriter.Write(data)
}
