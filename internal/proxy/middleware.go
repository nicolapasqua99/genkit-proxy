package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nicolapasqua99/genkit-proxy/internal/ratelimit"
)

// contextKey is an unexported type for context keys in this package to avoid
// collisions with keys defined in other packages.
type contextKey int

const (
	requestIDKey contextKey = iota
	modelKey
)

// modelSlot is a mutable holder for per-request generation metadata. Logger
// stores a pointer to one in the request context; the proxy Handler writes the
// decoded model name (and, on success, the token usage) into it so Logger and
// the metrics Middleware can read them after the request completes.
type modelSlot struct {
	name  string
	usage *Usage
}

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

// Unwrap exposes the wrapped ResponseWriter so http.ResponseController can reach
// the underlying Flusher and deadline setter (used by the streaming handler).
func (writer *statusWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

// RateLimit returns a middleware that enforces a global per-token fixed-window
// request limit using lim. Requests without a bearer token are passed through
// unchanged (the downstream handler handles missing auth). Backend errors from
// lim are logged and the request is allowed (fail-open). Requests that exceed
// the limit receive a 429 response with a Retry-After header.
//
// A limit of zero or less disables the middleware (pass-through).
func RateLimit(lim ratelimit.Limiter, limit int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limit <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			token, err := bearerToken(r)
			if err != nil {
				// No valid bearer token — let downstream handler reject it.
				next.ServeHTTP(w, r)
				return
			}
			ctx := r.Context()
			spanCtx, span := otel.Tracer("proxy").Start(ctx, "ratelimit.check")
			allowed, retryAfter, rlErr := lim.Allow(spanCtx, sha256hex(token), limit)
			span.SetAttributes(
				attribute.String("rl.layer", "global"),
				attribute.Bool("rl.allowed", allowed),
				attribute.Int("rl.retry_after_sec", int(retryAfter.Seconds())),
			)
			span.End()
			if rlErr != nil {
				slog.WarnContext(ctx, "rate limiter error", "err", rlErr)
				next.ServeHTTP(w, r) // fail open on backend error
				return
			}
			if !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS returns a middleware that adds cross-origin resource sharing headers to
// every response. The allowOrigins string is used verbatim as the value of
// Access-Control-Allow-Origin (pass "*" to allow all origins). Methods and
// headers are hardcoded to the set needed by the proxy API. OPTIONS preflight
// requests are short-circuited with 204.
func CORS(allowOrigins string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowOrigins)
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
