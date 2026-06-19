// Package proxy implements a model-agnostic HTTP gateway that forwards
// generation requests to LLM providers through Firebase Genkit, using
// per-request credentials supplied by the caller.
//
// The provider is selected dynamically from the provider-prefixed model name
// (for example "googleai/gemini-2.5-flash"), and the caller's API key is taken
// from the request's Authorization header, so credentials are never hardcoded
// or shared between tenants: a fresh, single-provider Genkit plugin is built for
// each request.
//
// # Layout
//
//   - proxy.go      HTTP Handler: method/auth checks, body decode, response and
//     error encoding, HTTP status mapping.
//   - request.go    GenerateRequest/GenerateResponse types and validation.
//   - router.go     Provider detection (providerOf) and plugin construction
//     (pluginFor).
//   - generator.go  The Generator interface and its Genkit-backed implementation.
//   - errors.go     Error classification and client-safe message generation.
//   - middleware.go Recover, RequestID, and Logger HTTP middleware.
//   - metrics.go    OpenTelemetry-backed request metrics in Prometheus format.
//
// Handler depends on the Generator interface rather than on Genkit directly, so
// the HTTP layer can be tested with a fake generator.
//
// Architecture, API, error-handling, observability, and deployment notes live in
// the repository's docs/ directory.
package proxy
