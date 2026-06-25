package auth

import (
	"context"
	"errors"
	"testing"
)

func TestStaticSecretSourceResolve(t *testing.T) {
	src := NewStaticSecretSource(map[string]string{"ref": "value"})
	cases := []struct {
		name    string
		ref     string
		want    string
		wantErr error
	}{
		{"known ref", "ref", "value", nil},
		{"unknown ref", "missing", "", ErrSecretUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := src.Resolve(context.Background(), tc.ref)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("Resolve(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

func TestNewStaticSecretSourceCopiesInput(t *testing.T) {
	in := map[string]string{"ref": "value"}
	src := NewStaticSecretSource(in)
	in["ref"] = "mutated"
	got, err := src.Resolve(context.Background(), "ref")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "value" {
		t.Errorf("Resolve after mutation = %q, want %q", got, "value")
	}
}

func TestParseStaticSecrets(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr bool
	}{
		{"empty", "", map[string]string{}, false},
		{"single", "a=1", map[string]string{"a": "1"}, false},
		{"multiple with spaces", " a=1 , b=2 ", map[string]string{"a": "1", "b": "2"}, false},
		{"value keeps equals", "a=sk-xx==", map[string]string{"a": "sk-xx=="}, false},
		{"blank entries skipped", "a=1,,", map[string]string{"a": "1"}, false},
		{"missing equals", "a", nil, true},
		{"empty ref", "=1", nil, true},
		{"duplicate ref", "a=1,a=2", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, err := ParseStaticSecrets(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for ref, want := range tc.want {
				got, err := src.Resolve(context.Background(), ref)
				if err != nil || got != want {
					t.Errorf("Resolve(%q) = %q, %v; want %q, nil", ref, got, err, want)
				}
			}
		})
	}
}
