package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/firebase/genkit/go/core"
	"github.com/openai/openai-go"
	"google.golang.org/genai"

	"github.com/nicolapasqua99/genkit-proxy/internal/auth"
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
func (validationErr *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", validationErr.Field, validationErr.Reason)
}

// errCategory is the coarse classification of a Generator error, used to pick
// both the HTTP status and the client-safe message.
type errCategory int

const (
	categoryValidation       errCategory = iota // caller-caused; safe to echo verbatim
	categoryUnsupported                         // caller-caused; safe to echo verbatim
	categoryUnauthenticated                     // upstream rejected credentials (401)
	categoryPermissionDenied                    // upstream denied access (403)
	categoryRateLimit                           // upstream rate-limited (429)
	categoryTimeout                             // upstream deadline / cancel (504)
	categoryNotFound                            // model or resource not found (404)
	categoryUpstream                            // any other upstream failure (502)
	categoryInternal                            // proxy-side failure, e.g. credential resolution (500)
)

// classify maps a Generator error to an errCategory. It prefers typed
// extraction (core.GenkitError status, provider SDK error codes, context
// sentinels) over string matching.
func classify(err error) errCategory {
	var validationErr *ValidationError
	switch {
	case errors.As(err, &validationErr):
		return categoryValidation
	case errors.Is(err, ErrUnsupportedProvider):
		return categoryUnsupported
	case errors.Is(err, auth.ErrUnknownTenant):
		return categoryUnauthenticated
	case errors.Is(err, auth.ErrNoProviderSecret):
		return categoryPermissionDenied
	case errors.Is(err, auth.ErrSecretUnavailable):
		return categoryInternal
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return categoryTimeout
	}

	// googleai: genkit-canonical typed status.
	var genkitErr *core.GenkitError
	if errors.As(err, &genkitErr) {
		switch genkitErr.Status {
		case core.UNAUTHENTICATED:
			return categoryUnauthenticated
		case core.PERMISSION_DENIED:
			return categoryPermissionDenied
		case core.RESOURCE_EXHAUSTED:
			return categoryRateLimit
		case core.DEADLINE_EXCEEDED:
			return categoryTimeout
		case core.NOT_FOUND:
			return categoryNotFound
		}
		return categoryForHTTP(core.HTTPStatusCode(genkitErr.Status))
	}

	// googleai fallback: raw genai.APIError (value type, value receiver).
	var genaiApiErr genai.APIError
	if errors.As(err, &genaiApiErr) {
		return categoryForHTTP(genaiApiErr.Code)
	}

	// openai + anthropic: openai-go typed error (pointer receiver).
	var openaiApiErr *openai.Error
	if errors.As(err, &openaiApiErr) {
		return categoryForHTTP(openaiApiErr.StatusCode)
	}

	return categoryUpstream
}

// categoryForHTTP maps a provider HTTP status code to an errCategory.
func categoryForHTTP(code int) errCategory {
	switch code {
	case http.StatusUnauthorized:
		return categoryUnauthenticated
	case http.StatusForbidden:
		return categoryPermissionDenied
	case http.StatusTooManyRequests:
		return categoryRateLimit
	case http.StatusGatewayTimeout, http.StatusRequestTimeout:
		return categoryTimeout
	case http.StatusNotFound:
		return categoryNotFound
	default:
		return categoryUpstream
	}
}

// safeMessage returns a client-safe message for err. Caller-caused errors are
// returned verbatim; upstream/provider errors are reduced to a generic message
// so internal details are not leaked to the caller.
func safeMessage(err error) string {
	switch {
	case errors.Is(err, auth.ErrUnknownTenant):
		return "gateway authentication failed"
	case errors.Is(err, auth.ErrNoProviderSecret):
		return "no provider credential configured for tenant"
	case errors.Is(err, auth.ErrSecretUnavailable):
		return "internal credential resolution error"
	}
	switch classify(err) {
	case categoryValidation, categoryUnsupported:
		return err.Error()
	case categoryUnauthenticated:
		return "upstream provider rejected the supplied credentials"
	case categoryPermissionDenied:
		return "upstream provider denied access"
	case categoryRateLimit:
		return "upstream provider rate limit exceeded"
	case categoryTimeout:
		return "upstream provider request timed out"
	case categoryNotFound:
		return "requested model was not found"
	default:
		return "upstream provider error"
	}
}
