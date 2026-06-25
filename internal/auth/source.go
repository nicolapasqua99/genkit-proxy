package auth

import (
	"context"
	"fmt"
	"strings"
)

// SecretSource resolves an opaque secret reference to its value. Implementations
// back the reference with a concrete store (an in-memory map today, Google
// Secret Manager later). A reference that cannot be resolved must return an error
// that wraps ErrSecretUnavailable.
type SecretSource interface {
	Resolve(ctx context.Context, ref string) (string, error)
}

// StaticSecretSource is an in-memory SecretSource backed by a fixed map of
// reference to value. It is the default source for the static, env-configured
// deployment.
type StaticSecretSource struct {
	values map[string]string
}

// NewStaticSecretSource returns a StaticSecretSource over a copy of values.
func NewStaticSecretSource(values map[string]string) *StaticSecretSource {
	copied := make(map[string]string, len(values))
	for ref, value := range values {
		copied[ref] = value
	}
	return &StaticSecretSource{values: copied}
}

// Resolve returns the value for ref, or an error wrapping ErrSecretUnavailable
// when ref is unknown.
func (s *StaticSecretSource) Resolve(_ context.Context, ref string) (string, error) {
	value, ok := s.values[ref]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrSecretUnavailable, ref)
	}
	return value, nil
}

// ParseStaticSecrets builds a StaticSecretSource from a comma-separated
// "ref=value" list (the GATEWAY_SECRETS format). The first '=' separates the
// reference from the value, so values may contain '=' (e.g. base64 padding).
// Whitespace around an entry is trimmed; blank entries are skipped. Secret
// references must not contain ',' or '='.
func ParseStaticSecrets(raw string) (*StaticSecretSource, error) {
	values := make(map[string]string)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		ref, value, ok := strings.Cut(entry, "=")
		ref = strings.TrimSpace(ref)
		if !ok || ref == "" {
			return nil, fmt.Errorf("GATEWAY_SECRETS entry %q: expected ref=value", entry)
		}
		if _, dup := values[ref]; dup {
			return nil, fmt.Errorf("GATEWAY_SECRETS: duplicate ref %q", ref)
		}
		values[ref] = value
	}
	return NewStaticSecretSource(values), nil
}
