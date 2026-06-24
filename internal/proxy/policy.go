package proxy

import "strings"

// ModelAllowlist restricts which models callers may invoke. Each entry is either
// a full model name ("provider/model") or a bare provider ("provider"); a
// request is allowed when its exact model name or its provider is listed. A nil
// or empty allowlist allows every model, preserving the proxy's open default.
type ModelAllowlist struct {
	models    map[string]struct{}
	providers map[string]struct{}
}

// NewModelAllowlist builds a ModelAllowlist from entries. An entry containing a
// "/" is treated as an exact model name, otherwise as a provider wildcard.
// Blank entries are ignored. It returns nil when no usable entry remains, so an
// unset allowlist allows all models.
func NewModelAllowlist(entries []string) *ModelAllowlist {
	models := make(map[string]struct{})
	providers := make(map[string]struct{})
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			models[entry] = struct{}{}
		} else {
			providers[entry] = struct{}{}
		}
	}
	if len(models) == 0 && len(providers) == 0 {
		return nil
	}
	return &ModelAllowlist{models: models, providers: providers}
}

// Allows reports whether model may be invoked. A nil allowlist allows every
// model.
func (a *ModelAllowlist) Allows(model string) bool {
	if a == nil {
		return true
	}
	if _, ok := a.models[model]; ok {
		return true
	}
	provider, _, _ := strings.Cut(model, "/")
	_, ok := a.providers[provider]
	return ok
}
