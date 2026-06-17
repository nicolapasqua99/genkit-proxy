package proxy

import (
	"errors"
	"testing"
)

func TestProviderOf(t *testing.T) {
	cases := []struct {
		name           string
		model          string
		want           string
		wantErr        bool
		wantValidation bool // true when the error should be *ValidationError, not ErrUnsupportedProvider
	}{
		{"googleai", "googleai/gemini-2.5-flash", "googleai", false, false},
		{"openai", "openai/gpt-4o", "openai", false, false},
		{"anthropic", "anthropic/claude-3-5-sonnet", "anthropic", false, false},
		{"nested path", "googleai/models/gemini", "googleai", false, false},
		{"no slash", "gemini-2.5-flash", "", true, false},
		{"empty provider", "/gemini", "", true, false},
		{"unknown provider", "cohere/command", "", true, false},
		{"empty model segment", "googleai/", "", true, true},
		{"whitespace model segment", "googleai/   ", "", true, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := providerOf(testCase.model)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("providerOf(%q) error = %v, wantErr %v", testCase.model, err, testCase.wantErr)
			}
			if got != testCase.want {
				t.Errorf("providerOf(%q) = %q, want %q", testCase.model, got, testCase.want)
			}
			if !testCase.wantErr {
				return
			}
			if testCase.wantValidation {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Errorf("providerOf(%q) error = %v, want *ValidationError", testCase.model, err)
				}
			} else {
				if !errors.Is(err, ErrUnsupportedProvider) {
					t.Errorf("providerOf(%q) error = %v, want ErrUnsupportedProvider", testCase.model, err)
				}
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
		{"empty model segment", "googleai/", true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			p, err := pluginFor(testCase.model, "test-key")
			if (err != nil) != testCase.wantErr {
				t.Fatalf("pluginFor(%q) error = %v, wantErr %v", testCase.model, err, testCase.wantErr)
			}
			if !testCase.wantErr && p == nil {
				t.Errorf("pluginFor(%q) returned nil plugin", testCase.model)
			}
		})
	}
}
