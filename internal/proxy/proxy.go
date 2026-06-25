package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nicolapasqua99/genkit-proxy/internal/ratelimit"
)

// maxRequestBytes caps the size of a decoded request body.
const maxRequestBytes = 1 << 20 // 1 MiB

// HandlerRLConfig configures per-request rate limiting inside the handler.
// It is distinct from the global middleware RateLimit, which runs before body
// parsing and enforces a blanket per-token cap.
type HandlerRLConfig struct {
	// StreamLimit is the per-token requests-per-window cap for stream requests.
	// Zero disables stream-specific limiting.
	StreamLimit int
	// LimitForModel returns the per-window limit and key tag for model.
	// The tag distinguishes model from provider keys, e.g. "model:foo/bar"
	// or "provider:foo". A zero limit disables per-model/provider checking.
	LimitForModel func(model string) (limit int, keyTag string)
}

// Handler serves generation requests over HTTP.
type Handler struct {
	generator Generator
	limiter   ratelimit.Limiter
	rlCfg     HandlerRLConfig
	allowlist *ModelAllowlist
	resolver  CredentialResolver
}

// NewHandler returns a Handler that routes requests through generator.
// Pass nil for lim and a zero HandlerRLConfig to disable per-request rate
// limiting (the typical test setup). A nil allowlist permits every model.
// Credential resolution defaults to pass-through; use WithCredentialResolver to
// enable decoupled gateway auth.
func NewHandler(generator Generator, lim ratelimit.Limiter, cfg HandlerRLConfig, allowlist *ModelAllowlist) *Handler {
	return &Handler{generator: generator, limiter: lim, rlCfg: cfg, allowlist: allowlist, resolver: passthroughResolver{}}
}

// WithCredentialResolver sets the CredentialResolver used to authenticate the
// tenant and resolve the provider key, returning the Handler for chaining. It
// enables decoupled gateway auth; without it the handler passes the bearer token
// straight through.
func (handler *Handler) WithCredentialResolver(resolver CredentialResolver) *Handler {
	handler.resolver = resolver
	return handler
}

// ServeHTTP decodes a GenerateRequest, extracts the bearer credential, routes
// the request through the Generator, and writes the JSON response.
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, httpReq *http.Request) {
	if httpReq.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	apiKey, err := bearerToken(httpReq)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, err.Error())
		return
	}

	if err := handler.resolver.Authenticate(httpReq.Context(), apiKey); err != nil {
		writeAuthError(writer, httpReq.Context(), err)
		return
	}

	httpReq.Body = http.MaxBytesReader(writer, httpReq.Body, maxRequestBytes)
	dec := json.NewDecoder(httpReq.Body)
	dec.DisallowUnknownFields()

	var req GenerateRequest
	if err := dec.Decode(&req); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}

	if !handler.allowModel(writer, req.ModelName) {
		return
	}

	if !handler.checkModelLimit(writer, httpReq.Context(), apiKey, req.ModelName) {
		return
	}

	if slot := modelSlotFromContext(httpReq.Context()); slot != nil {
		slot.name = req.ModelName
	}

	providerKey, err := handler.resolver.Resolve(httpReq.Context(), apiKey, req.ModelName)
	if err != nil {
		status := statusFor(err)
		if classify(err) >= categoryUnauthenticated {
			slog.ErrorContext(httpReq.Context(), "credential resolution failed",
				"model", req.ModelName,
				"status", status,
				"err", err,
				"request_id", requestIDFromContext(httpReq.Context()),
			)
		}
		writeError(writer, status, safeMessage(err))
		return
	}

	resp, err := handler.generator.Generate(httpReq.Context(), req, providerKey)
	if err != nil {
		status := statusFor(err)
		if classify(err) >= categoryUnauthenticated {
			slog.ErrorContext(httpReq.Context(), "generate failed",
				"model", req.ModelName,
				"status", status,
				"err", err,
				"request_id", requestIDFromContext(httpReq.Context()),
			)
		}
		writeError(writer, status, safeMessage(err))
		return
	}

	if slot := modelSlotFromContext(httpReq.Context()); slot != nil {
		slot.usage = resp.Usage
	}

	writeJSON(writer, http.StatusOK, resp)
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header per RFC 7235 (scheme comparison is case-insensitive).
// Returns ErrMissingCredentials when the header is absent or malformed.
func bearerToken(httpReq *http.Request) (string, error) {
	header := httpReq.Header.Get("Authorization")
	scheme, rest, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", ErrMissingCredentials
	}
	token := strings.TrimSpace(rest)
	if token == "" {
		return "", ErrMissingCredentials
	}
	return token, nil
}

// statusFor maps a Generator error to an HTTP status code.
func statusFor(err error) int {
	switch classify(err) {
	case categoryValidation, categoryUnsupported:
		return http.StatusBadRequest
	case categoryUnauthenticated:
		return http.StatusUnauthorized
	case categoryPermissionDenied:
		return http.StatusForbidden
	case categoryRateLimit:
		return http.StatusTooManyRequests
	case categoryTimeout:
		return http.StatusGatewayTimeout
	case categoryNotFound:
		return http.StatusNotFound
	case categoryInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusBadGateway
	}
}

// errorBody is the JSON envelope for error responses.
type errorBody struct {
	Error string `json:"error"`
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, errorBody{Error: message})
}

// writeAuthError logs and writes the HTTP error for a failed gateway
// authentication (run before the body is decoded, so no model is known). The
// body carries only a safe, categorised message.
func writeAuthError(writer http.ResponseWriter, ctx context.Context, err error) {
	status := statusFor(err)
	slog.ErrorContext(ctx, "gateway authentication failed",
		"status", status,
		"err", err,
		"request_id", requestIDFromContext(ctx),
	)
	writeError(writer, status, safeMessage(err))
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

// allowModel reports whether model may be invoked under the configured
// allowlist. It writes a 403 response and returns false when the model is not
// permitted; a nil allowlist permits every model.
func (handler *Handler) allowModel(w http.ResponseWriter, model string) bool {
	if handler.allowlist.Allows(model) {
		return true
	}
	writeError(w, http.StatusForbidden, "model not permitted: "+model)
	return false
}

// checkModelLimit enforces the per-model or per-provider rate limit configured
// in HandlerRLConfig. It writes a 429 response and returns false when the limit
// is exceeded; returns true (and writes nothing) in all other cases, including
// when no limit is configured or the limiter is nil.
func (handler *Handler) checkModelLimit(w http.ResponseWriter, ctx context.Context, apiKey, modelName string) bool {
	if handler.limiter == nil || handler.rlCfg.LimitForModel == nil {
		return true
	}
	limit, keyTag := handler.rlCfg.LimitForModel(modelName)
	if limit <= 0 {
		return true
	}
	spanCtx, span := otel.Tracer("proxy").Start(ctx, "ratelimit.check")
	key := rateLimitKey(apiKey, keyTag)
	allowed, retryAfter, rlErr := handler.limiter.Allow(spanCtx, key, limit)
	layer := "provider"
	if strings.HasPrefix(keyTag, "model:") {
		layer = "model"
	}
	span.SetAttributes(
		attribute.String("rl.layer", layer),
		attribute.Bool("rl.allowed", allowed),
		attribute.Int("rl.retry_after_sec", int(retryAfter.Seconds())),
	)
	span.End()
	if rlErr != nil {
		slog.WarnContext(ctx, "rate limiter error", "err", rlErr)
		return true // fail open on backend error
	}
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return false
	}
	return true
}

// rateLimitKey returns the hashed key used by the limiter for a given bearer
// token and optional namespace tag (e.g. "model:googleai/gemini-2.5-flash").
// The tag is included in the hash input so each namespace is independent.
func rateLimitKey(token, tag string) string {
	if tag == "" {
		return sha256hex(token)
	}
	return sha256hex(token + ":" + tag)
}

// sha256hex returns the hex-encoded SHA-256 digest of s. It is used to derive
// opaque rate-limit keys from bearer tokens so raw credentials are never stored
// in memory buckets or Redis.
func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
