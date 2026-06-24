# TODO — future enhancements

Roadmap for the Genkit AI proxy. The current service covers the core: per-request
provider routing by model prefix, `Authorization: Bearer` credential injection, validation,
and a single-turn `POST /v1/generate`. The items below are deferred, grouped by priority.

> **Build note:** `.github/workflows/ci-gate.yml` ("CI Quality Gate") runs the quality gates
> (`go build`, `golangci-lint`, `go vet`, race tests, `govulncheck`, `go-licenses`) on every pull
> request as a required status check. With direct pushes to `main` disallowed, every commit on
> `main` has passed the gate, so the auto-tag and Cloud Run deploy run only on gated code.

## Tier 0 — Correctness gaps (closest to bugs; do first)

- [x] **Upstream error classification** — `internal/proxy/proxy.go` `statusFor` collapses
  every non-validation error to `502`. A caller's bad/expired key, a provider `429`, a hung
  upstream, and an unknown model all return `502`. Map provider auth failures → `401/403`,
  rate-limit → `429` (with `Retry-After`), timeout/deadline → `504`, model-not-found →
  `404/400`. Requires typing the error in `generator.go`/`errors.go`. *Why:* a `502` tells a
  caller "our fault" when the usual cause is their own invalid key.
- [x] **Sanitize upstream error text** *(security)* — `writeError(w, …, err.Error())` returns
  raw provider/Genkit error strings to the caller, which can leak internal details in a
  multi-tenant gateway. Return categorized messages to the client; keep the detail in
  server-side logs only.
- [x] **Panic-recovery middleware** — `cmd/app/main.go` has no `recover()`. A plugin or
  Genkit panic drops the connection instead of returning a clean `500` JSON. Wrap the mux in
  a recover middleware (also the natural home for the structured-logging and request-ID
  items below).
- [x] **Empty-model-segment validation** — `internal/proxy/request.go` / `router.go`:
  `"googleai/"` passes `Validate()` (the provider matches, the model part is empty) and then
  fails upstream as a `502`. Require a non-empty model segment after the provider prefix so it
  is rejected as a `400`.
- [x] **Case-insensitive bearer scheme** — `internal/proxy/proxy.go` `bearerToken` matches
  `"Bearer "` case-sensitively, but RFC 7235 auth schemes are case-insensitive, so a valid
  `"bearer <token>"` is rejected `401`. Compare the scheme case-insensitively.
- [x] **Empty / safety-blocked output** — `internal/proxy/generator.go`: `resp.Text()` can be
  `""` (safety block, or a response carrying only tool-call/non-text parts). The caller cannot
  distinguish "model declined" from "empty". Surface the finish/block reason — couples with the
  Tier 2 usage + finish-reason item.

## Tier 1 — Production hardening

- [x] **CI quality gate** — `.github/workflows/ci-gate.yml` ("CI Quality Gate") runs `go build` /
  `golangci-lint run` / `go vet` / `gotestsum -race` / `govulncheck` / `go-licenses` on every pull
  request as a required status check. Combined with the no-direct-push rule on `main`, the auto-tag
  (`bump-version.yml`) → Cloud Run deploy path only ever runs on gated commits. *Why:* previously
  every push to `main` auto-tagged and every tag auto-deployed to Cloud Run with **no**
  test/lint/vuln gate — untested code shipped straight to production.
- [x] **Graceful shutdown** — `cmd/app/main.go`: use `signal.NotifyContext(ctx,
  os.Interrupt, syscall.SIGTERM)`, run `ListenAndServe` in a goroutine, and call
  `srv.Shutdown(ctx)` on signal. *Why:* Cloud Run sends `SIGTERM` before reaping the
  container; today in-flight generations are cut.
- [x] **Per-request upstream timeout** — `internal/proxy/generator.go`: wrap
  `genkit.Generate` in a `context.WithTimeout` (env-configurable). *Why:* bound latency; a
  hung provider currently occupies a goroutine until the 120s `WriteTimeout`.
- [x] **Structured logging** — add an `slog` middleware around the mux in `cmd/app`:
  method, path, status, latency, model, request ID. **Never log the bearer token.**
- [x] **Request ID propagation** — accept an inbound `X-Request-ID` or generate one; echo it
  in the response header and thread it through the structured logs. *Why:* correlate a caller
  request with its upstream call and log line.
- [x] **Metrics** — expose `/metrics` (OpenTelemetry / Prometheus): request count, latency
  histogram, and error rate by provider and status, via an OTel meter exported through a
  dedicated Prometheus registry (`internal/proxy/metrics.go`). Token counters (`llm_tokens_total`
  by provider and `kind` input/output) are recorded from the per-request usage threaded through
  the model slot.
- [x] **Readiness + build/version endpoints** — add `/readyz` and `/version` (git SHA / build
  time via `-ldflags -X`). *Why:* `/healthz` is liveness-only; ops needs to confirm what's
  deployed.
- [x] **Env-configurable server timeouts** — `cmd/app/main.go` hardcodes the Read / Write /
  Idle timeouts; make them (and the per-request timeout above) env-driven, and validate `PORT`.

## Tier 2 — Feature surface

- [x] **Usage + finish reason in response** — extend `GenerateResponse`
  (`internal/proxy/request.go`) with `Usage{Input, Output, Total}` and `FinishReason`,
  read from the `*ai.ModelResponse` in `internal/proxy/generator.go`. Near-free and needed
  for metering/billing.
- [x] **Generation config passthrough** — add `MaxOutputTokens` / `TopP` / `TopK` /
  `StopSequences` to `GenerateRequest`, mapped onto `ai.GenerationCommonConfig` via `configFor`
  in `internal/proxy/generator.go`. Validated with generic bounds (`maxOutputTokens >= 1`,
  `topP 0–1`, `topK >= 1`). **Provider-specific bounds remain unhandled** (as with temperature):
  the generic temperature `0–2` still passes e.g. `1.5` to Anthropic (max `1.0`); a value a
  provider rejects surfaces as the upstream error's classified status, not a pre-validation `400`.
- [x] **Structured / JSON output** — `responseFormat: "json"` (optionally with an `outputSchema`
  JSON Schema) on `GenerateRequest` maps to `ai.WithOutputFormat` / `ai.WithOutputSchema` in
  `internal/proxy/generator.go`. Valid JSON is returned inline in the response `data` field
  (`json.RawMessage`); non-JSON falls back to `output`. *Why:* the proxy previously returned plain
  text only, which blocked any app that needs machine-parseable output.
- [x] **Streaming (SSE)** — new `POST /v1/generate/stream` backed by
  `genkit.GenerateStream`, emitting `text/event-stream` with `http.Flusher`. Biggest
  chat-UX win.
- [x] **Multi-turn chat** — optional `Messages []Message` (role/content) on
  `GenerateRequest`, mapped via `ai.WithMessages`, alongside the existing `userMessage`
  field.
- [x] **Multimodal input** — `UserMessage string` precludes images/files. Design the
  `Messages` field above to carry typed parts (text / media), not just strings, so vision and
  document inputs are possible.
- [x] **Tool / function calling** — known limitation; agentic callers need multi-step
  tool round-trips. Larger design effort; named here so it isn't lost.
- [x] **Vertex AI provider** — only `googleai` is wired in `internal/proxy/router.go`; add the
  `vertexai` plugin for the enterprise GCP auth path, and document the supported-provider
  matrix.

## Tier 3 — Scaling / governance / security

- [x] **Genkit instance cache** — `internal/proxy/cache.go` `GenkitCache` memoises
  `*genkit.Genkit` instances keyed by a SHA-256 hash of (provider, apiKey), so the proxy reuses
  the provider client and connection pool instead of running `genkit.Init` per request.
  Concurrent first-hits for a key dedup onto a single build (a "future"); instances are
  initialised with a cache-lifetime context (per the audit caveat) and released on eviction or
  `Close`. Bounded by an idle TTL (background janitor + lazy expiry) **and** a max-size LRU, which
  also bound how long a tenant credential stays resident in memory — the in-memory-credential
  tradeoff. Tools switched to dynamic `ai.NewTool` so a reused instance's registry is never
  mutated. Enabled by default; configured via `GENKIT_CACHE_ENABLED` (default true),
  `GENKIT_CACHE_TTL` (default 10m), `GENKIT_CACHE_MAX_SIZE` (default 1024). Covered by
  `internal/proxy/cache_test.go`.
- [x] **`genkit.Init`-per-request global-state audit** — audited `genkit.Init` (v1.8.0).
  Findings: the registry is instance-local (`registry.New()`; GC'd with the `*Genkit`, no global
  state), the global tracer provider is set once via `sync.Once` (no per-Init accumulation), and
  the reflection-server goroutine is dev-only. The one per-Init goroutine comes from
  `signal.NotifyContext(ctx, …)` whose `stop` is discarded; it is released when the parent context
  is cancelled, which in the proxy is the per-request timeout/request context, so there is **no
  permanent leak** in the request-scoped model — cost is bounded by in-flight concurrency.
  Regression guard: `internal/proxy/init_audit_test.go` asserts no goroutine growth under repeated
  and concurrent request-scoped Init. *Caveat for the cache below:* because `stop` is discarded,
  cleanup depends on parent-ctx cancellation, so a cache must `Init` with a long-lived context and
  evict with a cancel, or each cached `(provider, key)` instance pins its signal goroutine for the
  process lifetime.
- [ ] **Model allowlist / per-tenant policy** — restrict which models a caller may invoke.
- [ ] **Decoupled gateway auth** — authenticate the tenant with its own key and resolve the
  provider key from Secret Manager, instead of the current raw pass-through.
- [x] **Rate limiting, CORS** — three-layer fixed-window rate limiting (global per-token,
  per-model/provider, per-stream) backed by in-memory or Redis (Sentinel / Cluster); CORS
  middleware with configurable `CORS_ALLOW_ORIGINS`. Retry-with-backoff on transient upstream
  errors is a follow-up item (generator layer).
- [x] **Retry-with-backoff** on transient upstream errors — `internal/proxy/retry.go`
  `RetryingGenerator` wraps the generator with exponential backoff and full jitter (capped at
  10s). Only transient errors are retried (rate-limit `429` / generic `5xx`); timeouts are not,
  since each attempt spends the per-request deadline budget. A stream is not retried once a chunk
  has been sent. Wired in `cmd/app/main.go` via `NewRetryingGenerator`, env-configurable with
  `RETRY_MAX_ATTEMPTS` (default 3) and `RETRY_BASE_BACKOFF` (default 100ms), and covered by
  `internal/proxy/retry_test.go`.
- [x] **Testing seam for `GenkitGenerator`** — `GenkitGenerator.Generate` is wholly untested
  (it needs real keys/network). Introduce a seam so error-classification and option-mapping can
  be unit-tested against a fake provider.
