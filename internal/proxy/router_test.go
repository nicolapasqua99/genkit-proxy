package proxy

import (
	"errors"
	"testing"
)

func TestProviderOf(t *testing.T) {
	cases := []struct {
		name    string
		model   string
		want    string
		wantErr bool
	}{
		{"googleai", "googleai/gemini-2.5-flash", "googleai", false},
		{"openai", "openai/gpt-4o", "openai", false},
		{"anthropic", "anthropic/claude-3-5-sonnet", "anthropic", false},
		{"nested path", "googleai/models/gemini", "googleai", false},
		{"no slash", "gemini-2.5-flash", "", true},
		{"empty provider", "/gemini", "", true},
		{"unknown provider", "cohere/command", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := providerOf(tc.model)
			if (err != nil) != tc.wantErr {
				t.Fatalf("providerOf(%q) error = %v, wantErr %v", tc.model, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("providerOf(%q) = %q, want %q", tc.model, got, tc.want)
			}
			if tc.wantErr && !errors.Is(err, ErrUnsupportedProvider) {
				t.Errorf("providerOf(%q) error = %v, want ErrUnsupportedProvider", tc.model, err)
			}
		})
	}
}

func TestPluginFor(t *testing.T) {
	cases := []struct {
		name    string
		model   string
		wantErr bool
	}{
		{"googleai", "googleai/gemini-2.5-flash", false},
		{"openai", "openai/gpt-4o", false},
		{"anthropic", "anthropic/claude-3-5-sonnet", false},
		{"unsupported", "cohere/command", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := pluginFor(tc.model, "test-key")
			if (err != nil) != tc.wantErr {
				t.Fatalf("pluginFor(%q) error = %v, wantErr %v", tc.model, err, tc.wantErr)
			}
			if !tc.wantErr && p == nil {
				t.Errorf("pluginFor(%q) returned nil plugin", tc.model)
			}
		})
	}
}
