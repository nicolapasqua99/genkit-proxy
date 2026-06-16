package proxy

import (
	"errors"
	"fmt"
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
