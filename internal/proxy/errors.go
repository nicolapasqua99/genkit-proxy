package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/firebase/genkit/go/core"
	"github.com/openai/openai-go"
	"google.golang.org/genai"
)

// ErrMissingCredentials indicates the request carried no usable bearer token.
var ErrMissingCredentials = errors.New("missing or malformed Authorization bearer token")

// ErrUnsupportedProvider indicates the model name does not map to a known
// provider plugin.
var ErrUnsupportedProvider = errors.New("unsupported model provider")

// ValidationError describes an invalid field in a GenerateRequest.
type ValidationError struct {
	// Field is the name of the offending request field.
	Field string
	// Reason explains why the field is invalid.
	Reason string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

// errCategory is the coarse classification of a Generator error, used to pick
// both the HTTP status and the client-safe message.
type errCategory int

const (
	catValidation       errCategory = iota // caller-caused; safe to echo verbatim
	catUnsupported                         // caller-caused; safe to echo verbatim
	catUnauthenticated                     // upstream rejected credentials (401)
	catPermissionDenied                    // upstream denied access (403)
	catRateLimit                           // upstream rate-limited (429)
	catTimeout                             // upstream deadline / cancel (504)
	catNotFound                            // model or resource not found (404)
	catUpstream                            // any other upstream failure (502)
)

// classify maps a Generator error to an errCategory. It prefers typed
// extraction (core.GenkitError status, provider SDK error codes, context
// sentinels) over string matching.
func classify(err error) errCategory {
	var validationErr *ValidationError
	switch {
	case errors.As(err, &validationErr):
		return catValidation
	case errors.Is(err, ErrUnsupportedProvider):
		return catUnsupported
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return catTimeout
	}

	// googleai: genkit-canonical typed status.
	var ge *core.GenkitError
	if errors.As(err, &ge) {
		switch ge.Status {
		case core.UNAUTHENTICATED:
			return catUnauthenticated
		case core.PERMISSION_DENIED:
			return catPermissionDenied
		case core.RESOURCE_EXHAUSTED:
			return catRateLimit
		case core.DEADLINE_EXCEEDED:
			return catTimeout
		case core.NOT_FOUND:
			return catNotFound
		}
		return categoryForHTTP(core.HTTPStatusCode(ge.Status))
	}

	// googleai fallback: raw genai.APIError (value type, value receiver).
	var ae genai.APIError
	if errors.As(err, &ae) {
		return categoryForHTTP(ae.Code)
	}

	// openai + anthropic: openai-go typed error (pointer receiver).
	var oe *openai.Error
	if errors.As(err, &oe) {
		return categoryForHTTP(oe.StatusCode)
	}

	return catUpstream
}

// categoryForHTTP maps a provider HTTP status code to an errCategory.
func categoryForHTTP(code int) errCategory {
	switch code {
	case http.StatusUnauthorized:
		return catUnauthenticated
	case http.StatusForbidden:
		return catPermissionDenied
	case http.StatusTooManyRequests:
		return catRateLimit
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return catTimeout
	case http.StatusNotFound:
		return catNotFound
	default:
		return catUpstream
	}
}

// safeMessage returns a client-safe message for err. Caller-caused errors are
// returned verbatim; upstream/provider errors are reduced to a generic message
// so internal details are not leaked to the caller.
func safeMessage(err error) string {
	switch classify(err) {
	case catValidation, catUnsupported:
		return err.Error()
	case catUnauthenticated:
		return "upstream provider rejected the supplied credentials"
	case catPermissionDenied:
		return "upstream provider denied access"
	case catRateLimit:
		return "upstream provider rate limit exceeded"
	case catTimeout:
		return "upstream provider request timed out"
	case catNotFound:
		return "requested model was not found"
	default:
		return "upstream provider error"
	}
}
