# Architecture

`genkit-proxy` is a model-agnostic AI HTTP gateway built on
[Firebase Genkit](https://firebase.google.com/docs/genkit). It exposes a single
generation endpoint, selects an LLM provider from the model-name prefix, and
forwards the request using the API key supplied per request — credentials are
never stored server-side. This document describes the runtime structure; see
also the [API reference](api.md), [error handling](error-handling.md),
[observability](observability.md), and [deployment](deployment.md).

## System context

```mermaid
graph LR
    client["API client"] -->|"POST /v1/generate<br/>Authorization: Bearer &lt;key&gt;"| proxy
    scraper["Prometheus / scraper"] -->|"GET /metrics"| proxy
    probe["Cloud Run / k8s probes"] -->|"GET /healthz, /readyz"| proxy

    subgraph host["Cloud Run (listens on $PORT)"]
        proxy["genkit-proxy"]
    end

    proxy -->|"googleai/*"| google["Google AI"]
    proxy -->|"openai/*"| openai["OpenAI"]
    proxy -->|"anthropic/*"| anthropic["Anthropic"]
```

The caller speaks one request/response shape regardless of provider. The bearer
token is passed straight through to the upstream provider; nothing is configured
server-side.

## Components

The codebase is one binary (`cmd/app`) wrapping one core package
(`internal/proxy`). Each file has a single responsibility.

```mermaid
graph TD
    subgraph cmd["cmd/app"]
        main["main.go<br/>mux, middleware chain, lifecycle"]
        config["config.go<br/>env-var config + defaults"]
    end

    subgraph pkg["internal/proxy"]
        handler["proxy.go<br/>Handler, bearerToken, statusFor"]
        mw["middleware.go<br/>Recover, RequestID, Logger"]
        metrics["metrics.go<br/>Metrics (OTel + Prometheus)"]
        gen["generator.go<br/>Generator, GenkitGenerator"]
        router["router.go<br/>providerOf, pluginFor"]
        req["request.go<br/>GenerateRequest/Response, Validate"]
        errs["errors.go<br/>classify, safeMessage"]
    end

    main --> config
    main --> handler
    main --> mw
    main --> metrics
    main --> gen

    handler --> req
    handler --> errs
    handler --> mw
    handler -->|"Generator interface"| gen
    gen --> router
    req --> router
    router --> errs
    metrics --> mw

    gen -->|"genkit.Generate"| genkit["firebase/genkit + plugins"]
    router --> genkit
    errs -->|typed errors| sdks["genkit core · openai-go · genai"]
    metrics --> otel["otel + prometheus"]
    mw --> uuid["google/uuid"]
```

Key abstraction: `Handler` depends on the `Generator` interface
(`generator.go:14`), not on Genkit directly, so the HTTP layer is tested with a
fake generator while `GenkitGenerator` carries the real upstream wiring.

## Middleware stack

`main.go:65` wraps the mux in four middlewares. They are listed outermost first;
a request passes through them top-to-bottom and the response unwinds
bottom-to-top.

```mermaid
flowchart TB
    in(["incoming request"]) --> recover
    recover["Recover<br/>panic → 500 JSON"] --> reqid
    reqid["RequestID<br/>set/echo X-Request-ID in ctx"] --> logger
    logger["Logger<br/>install modelSlot, time + access log"] --> metricsmw
    metricsmw["Metrics.Middleware<br/>count + latency, provider label"] --> mux
    mux["http.ServeMux<br/>route by method + path"] --> h["Handler / probe handlers"]
```

**Ordering matters.** `Logger` installs a mutable `modelSlot` in the request
context; the `Handler` writes the decoded model name into it, and both `Logger`
and `Metrics.Middleware` read it afterward (for the `model` log field and the
`provider` metric label). `Metrics.Middleware` therefore runs *inside* `Logger`
and reuses its slot — falling back to its own slot only if `Logger` is absent
(`metrics.go:95-103`).

## Request lifecycle

The full internal path of a `POST /v1/generate` call, with the success and error
branches:

```mermaid
sequenceDiagram
    autonumber
    participant C as Client
    participant MW as Middleware chain
    participant H as Handler (proxy.go)
    participant R as Router (router.go)
    participant G as GenkitGenerator
    participant K as Genkit
    participant P as Provider

    C->>MW: POST /v1/generate + Bearer key + JSON
    MW->>H: ServeHTTP (ctx: request_id, modelSlot)
    H->>H: method == POST?
    H->>H: bearerToken() — parse Authorization
    H->>H: decode body (≤ 1 MiB, DisallowUnknownFields)
    H->>R: req.Validate() → providerOf(modelName)
    H->>H: modelSlot.name = modelName
    H->>G: Generate(ctx, req, apiKey)
    G->>R: pluginFor(modelName, apiKey)
    G->>K: genkit.Init(plugin) + Generate(opts)
    K->>P: upstream generation
    alt success
        P-->>K: text + finishReason
        K-->>G: response
        G-->>H: GenerateResponse
        H-->>C: 200 {model, output, finishReason}
    else error
        P-->>K: provider error
        K-->>G: error
        G-->>H: wrapped error
        H->>H: classify → statusFor → safeMessage
        Note over H: full error logged server-side;<br/>client gets generic message
        H-->>C: 4xx/5xx {error}
    end
    Note over MW: Logger writes access log;<br/>Metrics records count + latency
```

Validation, auth, and routing details live in the [API reference](api.md); error
classification is detailed in [error handling](error-handling.md).

## Provider routing

The provider is derived from the model name and never configured separately. A
fresh, single-provider plugin is built per request so tenant keys stay isolated
(Genkit binds credentials at plugin construction).

```mermaid
flowchart TD
    start(["modelName, e.g. googleai/gemini-2.5-flash"]) --> cut["strings.Cut on '/'"]
    cut --> ok{"prefix present<br/>and known?"}
    ok -->|no| unsup["ErrUnsupportedProvider → 400"]
    ok -->|"model segment empty"| valerr["ValidationError → 400"]
    ok -->|googleai| g["googlegenai.GoogleAI{APIKey}"]
    ok -->|openai| o["openai.OpenAI{APIKey}"]
    ok -->|anthropic| a["anthropic.Anthropic{WithAPIKey}"]
    g --> plug([api.Plugin])
    o --> plug
    a --> plug
```

| Provider | Prefix | Plugin (Genkit) |
|----------|--------|-----------------|
| Google AI | `googleai` | `plugins/googlegenai` |
| OpenAI | `openai` | `plugins/compat_oai/openai` |
| Anthropic | `anthropic` | `plugins/compat_oai/anthropic` |

## Process lifecycle

```mermaid
sequenceDiagram
    autonumber
    participant OS as OS / Cloud Run
    participant M as main()
    participant S as http.Server

    M->>M: loadConfig() (env → timeouts, PORT)
    M->>M: NewMetrics(), NewHandler(NewGenkitGenerator)
    M->>M: build mux + middleware chain
    M->>S: ListenAndServe (goroutine)
    Note over M: block on signal context
    OS->>M: SIGINT / SIGTERM
    M->>S: Shutdown(ctx, 30s deadline)
    S-->>M: in-flight requests drained
    M-->>OS: exit 0
```

The accept loop runs in a goroutine while `main` blocks on a
`signal.NotifyContext`. On `SIGINT`/`SIGTERM` it calls `srv.Shutdown` with a
30-second deadline to drain in-flight requests (`main.go:72-90`). The standard
`net/http` server already serves each request on its own goroutine; the handler
spawns none of its own.
