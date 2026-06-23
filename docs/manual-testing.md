# Manual testing

How to exercise the proxy's behavior by hand. Pairs with the automated suite
(`go test -race ./...`), which already covers the same paths without a server.

The server exposes `POST /v1/generate` and `GET /healthz`, listening on `$PORT`
(default `8080`).

## Start the server

```bash
go run ./cmd/app
# or: go build -o bin/app ./cmd/app && PORT=8080 ./bin/app
```

## Checks that need no API key and no network

These requests are rejected in validation/auth, before any provider call, so
they work offline.

| Case | Command | Expected |
|------|---------|----------|
| Liveness | `curl -i localhost:8080/healthz` | `200`, empty body |
| Missing auth | `curl -i -X POST localhost:8080/v1/generate -d '{"modelName":"googleai/x","userMessage":"hi"}'` | `401` |
| Wrong scheme | `curl -i -X POST localhost:8080/v1/generate -H 'Authorization: Basic x' -d '{"modelName":"googleai/x","userMessage":"hi"}'` | `401` |
| Empty model segment | `curl -i -X POST localhost:8080/v1/generate -H 'Authorization: Bearer x' -d '{"modelName":"googleai/","userMessage":"hi"}'` | `400`, `{"error":"invalid modelName: missing model after provider prefix"}` |
| Unsupported provider | `curl -i -X POST localhost:8080/v1/generate -H 'Authorization: Bearer x' -d '{"modelName":"cohere/command","userMessage":"hi"}'` | `400` |
| Bad message role | `curl -i -X POST localhost:8080/v1/generate -H 'Authorization: Bearer x' -d '{"modelName":"googleai/x","userMessage":"hi","messages":[{"role":"assistant","content":"hey"}]}'` | `400`, `{"error":"invalid messages[0].role: must be \"user\" or \"model\""}` |
| Method not allowed | `curl -i localhost:8080/v1/generate` (GET) | `405` |

### Case-insensitive bearer scheme (RFC 7235)

A lowercase scheme should be *accepted*. To prove it without a provider call,
send a lowercase scheme with a body that fails validation: a `400` means the
token was accepted and validation ran (a rejected scheme would short-circuit to
`401`).

```bash
curl -i -X POST localhost:8080/v1/generate \
  -H 'authorization: bearer x' \
  -d '{"modelName":"nope","userMessage":"hi"}'
# expect: 400 (not 401) — the lowercase "bearer" scheme was accepted
```

## Checks that reach the provider (outbound network required)

### Error classification + sanitization — junk key

You do **not** need a valid key. A provider rejecting a bogus key is exactly
the path that previously returned a misleading `502`.

```bash
curl -i -X POST localhost:8080/v1/generate \
  -H 'Authorization: Bearer sk-totally-invalid' \
  -d '{"modelName":"openai/gpt-4o","userMessage":"hi"}'
# expect: HTTP 401
#         {"error":"upstream provider rejected the supplied credentials"}
```

The response body carries only the generic, category-based message; the raw
provider error is written to the server's log, never to the caller. The full
mapping:

| Upstream condition | Status | Client message |
|--------------------|--------|----------------|
| Bad / expired key | `401` | `upstream provider rejected the supplied credentials` |
| Permission / quota denied | `403` | `upstream provider denied access` |
| Rate limited | `429` | `upstream provider rate limit exceeded` |
| Deadline / timeout | `504` | `upstream provider request timed out` |
| Model not found | `404` | `requested model was not found` |
| Anything else | `502` | `upstream provider error` |

### Successful generation + `finishReason` — valid key

```bash
curl -s -X POST localhost:8080/v1/generate \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{"modelName":"openai/gpt-4o","userMessage":"say hi"}'
# expect: {"model":"openai/gpt-4o","output":"...","finishReason":"stop",
#          "usage":{"input":...,"output":...,"total":...}}
```

`finishReason` is omitted only when the provider reports none. On a safety
block it reads `"blocked"` and `output` is empty — that's how a caller
distinguishes "the model declined" from "the model returned an empty string".
`usage` is omitted when the provider reports no token counts.

### Structured JSON output — valid key

Request JSON by setting `responseFormat`; the parsed object comes back inline in
`data` (not as a string in `output`). Add an `outputSchema` to constrain shape.

```bash
curl -s -X POST localhost:8080/v1/generate \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{"modelName":"openai/gpt-4o","userMessage":"Where is the Eiffel Tower?","responseFormat":"json"}'
# expect: {"model":"openai/gpt-4o","data":{...},"finishReason":"stop","usage":{...}}
```

## Behaviors that are test-only

These can't easily be triggered from outside and are covered by unit tests:

- **Panic recovery** — `TestRecover` verifies a handler panic becomes a `500`
  `{"error":"internal server error"}` instead of a dropped connection.
- **Empty / safety-blocked output** — `TestHandlerServeHTTP` and the generator
  logic cover the `finishReason` propagation; a real safety block depends on the
  provider and specific prompt.

## Note for this repo's remote sandbox

Outbound calls in the "reach the provider" section depend on the environment's
network policy and may be blocked in a Claude Code web session even though they
work locally. The no-key checks above always run.
