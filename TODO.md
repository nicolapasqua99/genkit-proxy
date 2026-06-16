# TODO — future enhancements

Roadmap for the Genkit AI proxy. The current service covers the core: per-request
provider routing by model prefix, `Authorization: Bearer` credential injection, validation,
and a single-turn `POST /v1/generate`. The items below are deferred, grouped by priority.

> **Build note:** the `govulncheck` and `go-licenses` gates could not run in the build
> sandbox (network/tooling restrictions) and are expected to run in CI.

## Tier 1 — Production hardening

- [ ] **Graceful shutdown** — `cmd/app/main.go`: use `signal.NotifyContext(ctx,
  os.Interrupt, syscall.SIGTERM)`, run `ListenAndServe` in a goroutine, and call
  `srv.Shutdown(ctx)` on signal. *Why:* Cloud Run sends `SIGTERM` before reaping the
  container; today in-flight generations are cut.
- [ ] **Per-request upstream timeout** — `internal/proxy/generator.go`: wrap
  `genkit.Generate` in a `context.WithTimeout` (env-configurable). *Why:* bound latency; a
  hung provider currently occupies a goroutine until the 120s `WriteTimeout`.
- [ ] **Structured logging** — add an `slog` middleware around the mux in `cmd/app`:
  method, path, status, latency, model, request ID. **Never log the bearer token.**

## Tier 2 — Feature surface

- [ ] **Usage + finish reason in response** — extend `GenerateResponse`
  (`internal/proxy/request.go`) with `Usage{Input, Output, Total}` and `FinishReason`,
  read from the `*ai.ModelResponse` in `internal/proxy/generator.go`. Near-free and needed
  for metering/billing.
- [ ] **Generation config passthrough** — add `MaxOutputTokens` / `TopP` / `TopK` /
  `StopSequences` to `GenerateRequest`, mapped onto `ai.GenerationCommonConfig` (already
  carries these fields).
- [ ] **Streaming (SSE)** — new `POST /v1/generate/stream` backed by
  `genkit.GenerateStream`, emitting `text/event-stream` with `http.Flusher`. Biggest
  chat-UX win.
- [ ] **Multi-turn chat** — optional `Messages []Message` (role/content) on
  `GenerateRequest`, mapped via `ai.WithMessages`, alongside the existing `userMessage`
  field.

## Tier 3 — Scaling / governance / security

- [ ] **Genkit instance cache** — cache instances keyed by a hash of (provider, key) to
  avoid a fresh `genkit.Init` per request. Note the in-memory-credential tradeoff.
- [ ] **Model allowlist / per-tenant policy** — restrict which models a caller may invoke.
- [ ] **Decoupled gateway auth** — authenticate the tenant with its own key and resolve the
  provider key from Secret Manager, instead of the current raw pass-through.
- [ ] **Rate limiting, CORS, retry-with-backoff** on transient upstream errors.
