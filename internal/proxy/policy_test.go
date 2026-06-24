package proxy

import "testing"

func TestModelAllowlistAllows(t *testing.T) {
	cases := []struct {
		name    string
		entries []string
		model   string
		want    bool
	}{
		{"nil allowlist allows all", nil, "googleai/gemini-2.5-flash", true},
		{"empty entries allow all", []string{}, "googleai/gemini-2.5-flash", true},
		{"blank entries allow all", []string{"", "  "}, "openai/gpt-4o", true},
		{"exact model allowed", []string{"googleai/gemini-2.5-flash"}, "googleai/gemini-2.5-flash", true},
		{"exact model not listed", []string{"googleai/gemini-2.5-flash"}, "googleai/gemini-2.5-pro", false},
		{"provider wildcard allows any model", []string{"openai"}, "openai/gpt-4o", true},
		{"provider wildcard excludes other providers", []string{"openai"}, "googleai/gemini-2.5-flash", false},
		{"mixed entries, model match", []string{"openai", "googleai/gemini-2.5-flash"}, "googleai/gemini-2.5-flash", true},
		{"mixed entries, provider match", []string{"openai", "googleai/gemini-2.5-flash"}, "openai/gpt-4o-mini", true},
		{"mixed entries, no match", []string{"openai", "googleai/gemini-2.5-flash"}, "anthropic/claude-3", false},
		{"entries trimmed", []string{" openai "}, "openai/gpt-4o", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewModelAllowlist(tc.entries)
			if got := a.Allows(tc.model); got != tc.want {
				t.Errorf("Allows(%q) = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

func TestNewModelAllowlistNilWhenEmpty(t *testing.T) {
	for _, entries := range [][]string{nil, {}, {""}, {"  ", ""}} {
		if a := NewModelAllowlist(entries); a != nil {
			t.Errorf("NewModelAllowlist(%q) = %+v, want nil", entries, a)
		}
	}
}
