# genkit-proxy

A model-agnostic AI HTTP gateway built on [Firebase Genkit](https://firebase.google.com/docs/genkit).
It exposes a single `POST /v1/generate` endpoint and forwards each request to
Google AI, OpenAI, or Anthropic — chosen from the model-name prefix — using the
API key supplied per request in the `Authorization` header. Credentials are never
stored or shared: a fresh, single-provider Genkit plugin is built for every
request to keep tenant keys isolated. The service listens on `$PORT` (default
`8080`) and is ready to run on Cloud Run.

## Features

- **One unified endpoint** for multiple LLM providers — callers speak a single request/response shape.
- **Provider routing by model prefix** — `googleai/…`, `openai/…`, `anthropic/…` select the backend.
- **Per-request credentials** — the bearer token is passed straight through to the upstream provider; nothing is configured server-side.
- **Safe error handling** — upstream/provider failures are classified and reduced to generic messages so internal details never leak; caller mistakes are reported verbatim.
- **Observability** — structured `log/slog` logging with a per-request ID (`X-Request-ID`, UUID v4 fallback).
- **Production lifecycle** — panic recovery, configurable HTTP timeouts, and graceful shutdown on `SIGINT`/`SIGTERM`.

### Supported providers

| Provider | Model prefix | Example model |
|----------|--------------|---------------|
| Google AI | `googleai` | `googleai/gemini-2.5-flash` |
| OpenAI | `openai` | `openai/gpt-4o` |
| Anthropic | `anthropic` | `anthropic/claude-3-5-sonnet` |

## API

### `POST /v1/generate`

Requires an `Authorization: Bearer <api-key>` header carrying the upstream
provider's API key.

**Request body**

```json
{
  "modelName": "googleai/gemini-2.5-flash",
  "userMessage": "Say hello.",
  "systemPrompt": "You are a concise assistant.",
  "temperature": 0.7
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `modelName` | yes | Provider-prefixed model identifier; the prefix selects the provider. |
| `userMessage` | yes | The user prompt. |
| `systemPrompt` | no | Optional system instruction. |
| `temperature` | no | Sampling randomness, `0`–`2`. Provider default when omitted. |

**Response body**

```json
{
  "model": "googleai/gemini-2.5-flash",
  "output": "Hello!",
  "finishReason": "stop"
}
```

`output` may be empty when the model returned no text (for example a safety
block); inspect `finishReason` in that case. Common reasons: `stop`, `length`,
`blocked`, `interrupted`, `other`, `unknown`.

**Example**

```bash
curl -sS http://localhost:8080/v1/generate \
  -H "Authorization: Bearer $PROVIDER_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"modelName":"googleai/gemini-2.5-flash","userMessage":"Say hello."}'
```

### Errors

Errors are returned as JSON:

```json
{ "error": "upstream provider rejected the supplied credentials" }
```

| Status | Cause |
|--------|-------|
| `400` | Invalid request (bad JSON, missing field, bad temperature) or unsupported provider. |
| `401` | Missing/malformed bearer token, or upstream rejected the credentials. |
| `403` | Upstream provider denied access. |
| `404` | Requested model not found. |
| `405` | Wrong HTTP method. |
| `429` | Upstream rate limit exceeded. |
| `500` | Recovered panic in the handler. |
| `502` | Other upstream provider error. |
| `504` | Upstream request timed out. |

### Operational endpoints

| Endpoint | Purpose |
|----------|---------|
| `GET /healthz` | Liveness probe (always `200`). |
| `GET /readyz` | Readiness probe (always `200`). |
| `GET /version` | Returns `{"version","buildTime"}`, embedded at build time. |

## Configuration

All configuration is via environment variables; every variable is optional and
falls back to the default below. Duration values use Go's
[`time.ParseDuration`](https://pkg.go.dev/time#ParseDuration) format (e.g.
`30s`, `2m`, `500ms`). LLM credentials are **not** configured here — they are
supplied per request in the `Authorization` header.

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP listen port. |
| `READ_HEADER_TIMEOUT` | `10s` | Max time to read request headers. |
| `READ_TIMEOUT` | `30s` | Max time to read the request. |
| `WRITE_TIMEOUT` | `120s` | Max time to write the response. |
| `IDLE_TIMEOUT` | `60s` | Max keep-alive idle time. |
| `GENERATE_TIMEOUT` | `30s` | Max time for the upstream generation call. |

## Quick start

Requires Go (the version is pinned in `go.mod` / `.go-version`;
`GOTOOLCHAIN=auto` downloads it automatically on first use).

```bash
go run ./cmd/app          # starts the server on :8080
```

Then call it (see the `curl` example above). Optionally install the dev tooling
once per machine with `make tools`.

## Development

Common commands (each has a `make` wrapper):

```bash
go build ./...                  # make build
golangci-lint run ./...         # make lint
golangci-lint fmt               # make fmt        (check-only: make fmt-check)
gotestsum -- -race ./...        # make test-race
govulncheck ./...               # make vuln
go-licenses check ./...         # make licenses
air                             # make watch — live reload
make ci                         # full gate: fmt-check, vet, lint, test-race, vuln
```

See [`docs/manual-testing.md`](docs/manual-testing.md) for offline validation
checks and provider integration testing with `curl`.

### Project layout

```
cmd/app/            Server binary: config loading, routing, lifecycle.
internal/proxy/     Core gateway:
  proxy.go            HTTP handler — decode, authorize, respond.
  generator.go        Genkit-backed generation per request.
  router.go           Provider selection and plugin construction.
  request.go          Request/response types and validation.
  errors.go           Error classification and client-safe messages.
  middleware.go       Recover, RequestID, and Logger middleware.
```

Conventions for commits, branches, and PRs live in `CLAUDE.md` and the
`.claude/` directory.

## Deployment

The multi-stage [`Dockerfile`](Dockerfile) builds a static binary into a
distroless `nonroot` image and embeds the version via the `VCS_REF` and
`BUILD_TIME` build args. On a pushed `v*.*.*` tag, `.github/workflows/release.yml`
deploys to Google Cloud Run; `.github/workflows/bump-version.yml` derives and
pushes the next version tag from Conventional Commit messages on `main`.
