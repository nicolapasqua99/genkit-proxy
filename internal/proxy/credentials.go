package proxy

import (
	"context"
	"log/slog"

	"github.com/nicolapasqua99/genkit-proxy/internal/auth"
)

// CredentialResolver decouples gateway authentication from upstream provider
// credentials. Authenticate verifies the inbound gateway token belongs to a
// known tenant (run early, before rate limiting and body work); Resolve maps the
// token to the provider API key for a model (run just before generation). The
// default is pass-through: every token authenticates and resolves to itself.
type CredentialResolver interface {
	Authenticate(ctx context.Context, token string) error
	Resolve(ctx context.Context, token, modelName string) (string, error)
}

// passthroughResolver preserves the proxy's raw pass-through behaviour: the
// bearer token is the provider key and every token is accepted.
type passthroughResolver struct{}

// Authenticate accepts any token.
func (passthroughResolver) Authenticate(context.Context, string) error { return nil }

// Resolve returns token unchanged.
func (passthroughResolver) Resolve(_ context.Context, token, _ string) (string, error) {
	return token, nil
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

// Authenticate reports whether token belongs to a known tenant, returning
// auth.ErrUnknownTenant otherwise.
func (s secretCredentialResolver) Authenticate(_ context.Context, token string) error {
	if _, ok := s.resolver.Authenticate(token); !ok {
		return auth.ErrUnknownTenant
	}
	return nil
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
