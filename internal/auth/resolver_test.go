package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// testResolver builds a Resolver with a single tenant "acme" whose gateway key
// is "gw-key" and whose openai secret resolves to "sk-openai".
func testResolver(t *testing.T) *Resolver {
	t.Helper()
	tenantsJSON := fmt.Sprintf(
		`{%q: {"tenant": "acme", "providers": {"openai": "acme-openai", "missingref": "absent"}}}`,
		hashKey("gw-key"),
	)
	source := NewStaticSecretSource(map[string]string{"acme-openai": "sk-openai"})
	resolver, err := NewResolver(tenantsJSON, source)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	return resolver
}

func TestNewResolver(t *testing.T) {
	source := NewStaticSecretSource(nil)
	cases := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"valid", `{"abc": {"tenant": "t", "providers": {"openai": "ref"}}}`, false},
		{"empty object", `{}`, false},
		{"malformed json", `{`, true},
		{"unknown field", `{"abc": {"tenant": "t", "extra": 1}}`, true},
		{"missing tenant", `{"abc": {"providers": {"openai": "ref"}}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewResolver(tc.json, source)
			if tc.wantErr != (err != nil) {
				t.Fatalf("NewResolver err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}

	t.Run("nil source", func(t *testing.T) {
		if _, err := NewResolver(`{}`, nil); err == nil {
			t.Fatal("expected error for nil source")
		}
	})
}

func TestResolverAuthenticate(t *testing.T) {
	resolver := testResolver(t)
	t.Run("known key", func(t *testing.T) {
		id, ok := resolver.Authenticate("gw-key")
		if !ok || id != "acme" {
			t.Errorf("Authenticate = %q, %v; want acme, true", id, ok)
		}
	})
	t.Run("unknown key", func(t *testing.T) {
		if id, ok := resolver.Authenticate("nope"); ok {
			t.Errorf("Authenticate = %q, %v; want \"\", false", id, ok)
		}
	})
}

func TestResolverResolve(t *testing.T) {
	resolver := testResolver(t)
	cases := []struct {
		name       string
		key        string
		provider   string
		wantKey    string
		wantTenant string
		wantErr    error
	}{
		{"resolves provider key", "gw-key", "openai", "sk-openai", "acme", nil},
		{"unknown tenant", "wrong", "openai", "", "", ErrUnknownTenant},
		{"no provider secret", "gw-key", "anthropic", "", "acme", ErrNoProviderSecret},
		{"secret value missing", "gw-key", "missingref", "", "acme", ErrSecretUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, tenant, err := resolver.Resolve(context.Background(), tc.key, tc.provider)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tc.wantKey {
				t.Errorf("key = %q, want %q", key, tc.wantKey)
			}
			if tenant != tc.wantTenant {
				t.Errorf("tenant = %q, want %q", tenant, tc.wantTenant)
			}
		})
	}
}
