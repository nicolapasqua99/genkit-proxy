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

// fakeCredentialResolver maps a token to a provider key, or returns err.
type fakeCredentialResolver struct {
	key string
	err error
}

func (f fakeCredentialResolver) Resolve(_ context.Context, _, _ string) (string, error) {
	return f.key, f.err
}

func TestResolvingGeneratorGenerate(t *testing.T) {
	t.Run("delegates with resolved key", func(t *testing.T) {
		inner := &fakeGenerator{resp: GenerateResponse{Output: "ok"}}
		gen := NewResolvingGenerator(inner, fakeCredentialResolver{key: "sk-real"})
		req := GenerateRequest{ModelName: "openai/gpt-4o", UserMessage: "hi"}
		if _, err := gen.Generate(context.Background(), req, "gw-token"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if inner.gotKey != "sk-real" {
			t.Errorf("inner key = %q, want %q", inner.gotKey, "sk-real")
		}
	})

	t.Run("resolver error short-circuits", func(t *testing.T) {
		inner := &fakeGenerator{}
		gen := NewResolvingGenerator(inner, fakeCredentialResolver{err: auth.ErrUnknownTenant})
		req := GenerateRequest{ModelName: "openai/gpt-4o"}
		_, err := gen.Generate(context.Background(), req, "gw-token")
		if !errors.Is(err, auth.ErrUnknownTenant) {
			t.Fatalf("err = %v, want ErrUnknownTenant", err)
		}
		if inner.gotRequest.ModelName != "" {
			t.Error("inner generator should not have been called")
		}
	})
}

func TestResolvingGeneratorGenerateStream(t *testing.T) {
	t.Run("delegates with resolved key", func(t *testing.T) {
		inner := &fakeGenerator{streamDeltas: []string{"a", "b"}}
		gen := NewResolvingGenerator(inner, fakeCredentialResolver{key: "sk-real"})
		req := GenerateRequest{ModelName: "openai/gpt-4o"}
		var got []string
		_, err := gen.GenerateStream(context.Background(), req, "gw-token", func(d string) error {
			got = append(got, d)
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if inner.gotKey != "sk-real" {
			t.Errorf("inner key = %q, want %q", inner.gotKey, "sk-real")
		}
		if len(got) != 2 {
			t.Errorf("deltas = %v, want 2", got)
		}
	})

	t.Run("resolver error short-circuits", func(t *testing.T) {
		inner := &fakeGenerator{streamDeltas: []string{"a"}}
		gen := NewResolvingGenerator(inner, fakeCredentialResolver{err: auth.ErrNoProviderSecret})
		req := GenerateRequest{ModelName: "openai/gpt-4o"}
		called := false
		_, err := gen.GenerateStream(context.Background(), req, "gw-token", func(string) error {
			called = true
			return nil
		})
		if !errors.Is(err, auth.ErrNoProviderSecret) {
			t.Fatalf("err = %v, want ErrNoProviderSecret", err)
		}
		if called {
			t.Error("inner stream should not have been called")
		}
	})
}

func TestPassthroughResolver(t *testing.T) {
	got, err := passthroughResolver{}.Resolve(context.Background(), "token", "openai/gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "token" {
		t.Errorf("Resolve = %q, want %q", got, "token")
	}
}

func TestSecretCredentialResolver(t *testing.T) {
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
	resolver := NewCredentialResolver(authResolver)

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
