package proxy

import (
	"context"
	"log/slog"

	"github.com/nicolapasqua99/genkit-proxy/internal/auth"
)

// CredentialResolver maps the inbound bearer token to the provider API key used
// upstream for a model. The default behaviour is pass-through (the token is the
// provider key); decoupled gateway auth swaps in a resolver that authenticates
// the tenant and resolves the provider key from a secret store.
type CredentialResolver interface {
	Resolve(ctx context.Context, token, modelName string) (string, error)
}

// passthroughResolver returns the inbound token unchanged, preserving the
// proxy's raw pass-through behaviour.
type passthroughResolver struct{}

// Resolve returns token unchanged.
func (passthroughResolver) Resolve(_ context.Context, token, _ string) (string, error) {
	return token, nil
}

// ResolvingGenerator wraps a Generator, replacing the inbound gateway token with
// the provider API key produced by a CredentialResolver before delegating. It
// composes outside RetryingGenerator so resolution runs once per request and a
// resolution failure short-circuits without any upstream call.
type ResolvingGenerator struct {
	inner    Generator
	resolver CredentialResolver
}

// NewResolvingGenerator returns a Generator that resolves the provider key via
// resolver before delegating to inner.
func NewResolvingGenerator(inner Generator, resolver CredentialResolver) Generator {
	return &ResolvingGenerator{inner: inner, resolver: resolver}
}

// Generate resolves the provider key, then delegates to the inner generator.
func (g *ResolvingGenerator) Generate(ctx context.Context, req GenerateRequest, token string) (GenerateResponse, error) {
	key, err := g.resolver.Resolve(ctx, token, req.ModelName)
	if err != nil {
		return GenerateResponse{}, err
	}
	return g.inner.Generate(ctx, req, key)
}

// GenerateStream resolves the provider key, then delegates to the inner
// generator's stream.
func (g *ResolvingGenerator) GenerateStream(ctx context.Context, req GenerateRequest, token string, onChunk func(delta string) error) (GenerateResponse, error) {
	key, err := g.resolver.Resolve(ctx, token, req.ModelName)
	if err != nil {
		return GenerateResponse{}, err
	}
	return g.inner.GenerateStream(ctx, req, key, onChunk)
}

// secretCredentialResolver bridges an auth.Resolver to CredentialResolver,
// deriving the provider from the model name. For vertexai the tenant is still
// authenticated, but no key is forwarded (Vertex authenticates via ADC).
type secretCredentialResolver struct {
	resolver *auth.Resolver
}

// NewCredentialResolver returns a CredentialResolver backed by an auth.Resolver.
func NewCredentialResolver(resolver *auth.Resolver) CredentialResolver {
	return secretCredentialResolver{resolver: resolver}
}

// Resolve authenticates the tenant and resolves the provider key for the model.
func (s secretCredentialResolver) Resolve(ctx context.Context, token, modelName string) (string, error) {
	provider, err := providerOf(modelName)
	if err != nil {
		return "", err
	}
	if provider == providerVertexAI {
		tenantID, ok := s.resolver.Authenticate(token)
		if !ok {
			return "", auth.ErrUnknownTenant
		}
		slog.DebugContext(ctx, "gateway auth resolved", "tenant", tenantID, "provider", provider)
		return "", nil
	}
	key, tenantID, err := s.resolver.Resolve(ctx, token, provider)
	if err != nil {
		return "", err
	}
	slog.DebugContext(ctx, "gateway auth resolved", "tenant", tenantID, "provider", provider)
	return key, nil
}
