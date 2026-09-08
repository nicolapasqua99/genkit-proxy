package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/nicolapasqua99/genkit-proxy/internal/auth"
)

// fakeCredentialResolver authenticates only tokens in known, and resolves any
// token to key (or returns resolveErr).
type fakeCredentialResolver struct {
	known      map[string]bool
	key        string
	resolveErr error
}

func (f fakeCredentialResolver) Authenticate(_ context.Context, token string) error {
	if f.known != nil && !f.known[token] {
		return auth.ErrUnknownTenant
	}
	return nil
}

func (f fakeCredentialResolver) Resolve(_ context.Context, _, _ string) (string, error) {
	return f.key, f.resolveErr
}

func TestPassthroughResolver(t *testing.T) {
	r := passthroughResolver{}
	if err := r.Authenticate(context.Background(), "anything"); err != nil {
		t.Errorf("Authenticate = %v, want nil", err)
	}
	got, err := r.Resolve(context.Background(), "token", "openai/gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "token" {
		t.Errorf("Resolve = %q, want %q", got, "token")
	}
}

func newTestCredentialResolver(t *testing.T) CredentialResolver {
	t.Helper()
	hash := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	tenantsJSON := fmt.Sprintf(
		`{%q: {"tenant": "acme", "providers": {"openai": "acme-openai"}}}`,
		hash("gw-key"),
	)
	source := auth.NewStaticSecretSource(map[string]string{"acme-openai": "sk-openai"})
	authResolver, err := auth.NewResolver(tenantsJSON, source)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return NewCredentialResolver(authResolver)
}

func TestSecretCredentialResolverAuthenticate(t *testing.T) {
	resolver := newTestCredentialResolver(t)
	if err := resolver.Authenticate(context.Background(), "gw-key"); err != nil {
		t.Errorf("Authenticate(known) = %v, want nil", err)
	}
	if err := resolver.Authenticate(context.Background(), "wrong"); !errors.Is(err, auth.ErrUnknownTenant) {
		t.Errorf("Authenticate(unknown) = %v, want ErrUnknownTenant", err)
	}
}

func TestSecretCredentialResolverResolve(t *testing.T) {
	resolver := newTestCredentialResolver(t)
	cases := []struct {
		name    string
		token   string
		model   string
		wantKey string
		wantErr error
	}{
		{"resolves provider key", "gw-key", "openai/gpt-4o", "sk-openai", nil},
		{"vertex authenticates without key", "gw-key", "vertexai/gemini-2.5-flash", "", nil},
		{"unknown tenant", "wrong", "openai/gpt-4o", "", auth.ErrUnknownTenant},
		{"vertex unknown tenant", "wrong", "vertexai/gemini-2.5-flash", "", auth.ErrUnknownTenant},
		{"unconfigured provider", "gw-key", "anthropic/claude", "", auth.ErrNoProviderSecret},
		{"unsupported model", "gw-key", "bogus/model", "", ErrUnsupportedProvider},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, err := resolver.Resolve(context.Background(), tc.token, tc.model)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tc.wantKey {
				t.Errorf("key = %q, want %q", key, tc.wantKey)
			}
		})
	}
}
