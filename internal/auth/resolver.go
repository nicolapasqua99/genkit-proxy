package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Resolution failures. Callers map these to HTTP statuses (401/403/500); the
// boundaries are kept distinct so an unknown caller, a caller lacking a
// provider credential, and a broken secret store are not conflated.
var (
	// ErrUnknownTenant indicates the gateway key did not match any tenant.
	ErrUnknownTenant = errors.New("unknown gateway tenant")
	// ErrNoProviderSecret indicates the tenant has no secret configured for the
	// requested provider.
	ErrNoProviderSecret = errors.New("no provider secret for tenant")
	// ErrSecretUnavailable indicates the SecretSource could not resolve a
	// configured reference.
	ErrSecretUnavailable = errors.New("secret unavailable")
)

// tenant is one authenticated caller and its per-provider secret references.
type tenant struct {
	id      string
	secrets map[string]string // provider -> secret reference
}

// Resolver authenticates gateway keys against a tenant table and resolves the
// provider API key for an authenticated caller through a SecretSource.
type Resolver struct {
	tenants map[string]tenant // SHA-256 hex of the gateway key -> tenant
	source  SecretSource
}

// tenantConfig is the JSON shape of one GATEWAY_AUTH_TENANTS entry.
type tenantConfig struct {
	Tenant    string            `json:"tenant"`
	Providers map[string]string `json:"providers"`
}

// NewResolver builds a Resolver from the GATEWAY_AUTH_TENANTS JSON and a
// SecretSource. The JSON is an object keyed by the SHA-256 hex digest of each
// gateway key (raw keys are never stored), mapping to {tenant, providers},
// where providers maps a provider name to a SecretSource reference:
//
//	{"<sha256hex(key)>": {"tenant": "acme",
//	                      "providers": {"openai": "acme-openai-ref"}}}
//
// source must be non-nil.
func NewResolver(tenantsJSON string, source SecretSource) (*Resolver, error) {
	if source == nil {
		return nil, errors.New("auth: nil SecretSource")
	}
	var raw map[string]tenantConfig
	dec := json.NewDecoder(strings.NewReader(tenantsJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("GATEWAY_AUTH_TENANTS: %w", err)
	}
	tenants := make(map[string]tenant, len(raw))
	for hash, cfg := range raw {
		if cfg.Tenant == "" {
			return nil, fmt.Errorf("GATEWAY_AUTH_TENANTS: entry %q missing tenant", hash)
		}
		tenants[hash] = tenant{id: cfg.Tenant, secrets: cfg.Providers}
	}
	return &Resolver{tenants: tenants, source: source}, nil
}

// Authenticate reports whether gatewayKey matches a configured tenant, returning
// the tenant id when it does.
func (r *Resolver) Authenticate(gatewayKey string) (tenantID string, ok bool) {
	t, found := r.tenants[hashKey(gatewayKey)]
	if !found {
		return "", false
	}
	return t.id, true
}

// Resolve authenticates gatewayKey and resolves the provider API key for
// provider. It returns ErrUnknownTenant when the key is unrecognised,
// ErrNoProviderSecret when the tenant has no secret for provider, and an error
// wrapping ErrSecretUnavailable when the SecretSource lookup fails.
func (r *Resolver) Resolve(ctx context.Context, gatewayKey, provider string) (providerKey, tenantID string, err error) {
	t, found := r.tenants[hashKey(gatewayKey)]
	if !found {
		return "", "", ErrUnknownTenant
	}
	ref, ok := t.secrets[provider]
	if !ok {
		return "", t.id, fmt.Errorf("%w: tenant %q provider %q", ErrNoProviderSecret, t.id, provider)
	}
	key, err := r.source.Resolve(ctx, ref)
	if err != nil {
		return "", t.id, err
	}
	return key, t.id, nil
}

// hashKey returns the SHA-256 hex digest of a gateway key, matching the digests
// used as keys in the tenant table.
func hashKey(gatewayKey string) string {
	sum := sha256.Sum256([]byte(gatewayKey))
	return hex.EncodeToString(sum[:])
}
