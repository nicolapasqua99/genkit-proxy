package proxy

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/firebase/genkit/go/core"
	"github.com/openai/openai-go"
	"google.golang.org/genai"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want errCategory
	}{
		{"validation error", &ValidationError{Field: "f", Reason: "r"}, categoryValidation},
		{"unsupported provider", ErrUnsupportedProvider, categoryUnsupported},
		{"wrapped unsupported provider", fmt.Errorf("wrap: %w", ErrUnsupportedProvider), categoryUnsupported},
		{"deadline exceeded", context.DeadlineExceeded, categoryTimeout},
		{"context canceled", context.Canceled, categoryTimeout},
		{"genkit unauthenticated", core.NewError(core.UNAUTHENTICATED, "x"), categoryUnauthenticated},
		{"genkit permission denied", core.NewError(core.PERMISSION_DENIED, "x"), categoryPermissionDenied},
		{"genkit resource exhausted", core.NewError(core.RESOURCE_EXHAUSTED, "x"), categoryRateLimit},
		{"genkit deadline exceeded", core.NewError(core.DEADLINE_EXCEEDED, "x"), categoryTimeout},
		{"genkit not found", core.NewError(core.NOT_FOUND, "x"), categoryNotFound},
		{"genkit unknown maps to upstream", core.NewError(core.UNKNOWN, "x"), categoryUpstream},
		{"genai 401", genai.APIError{Code: 401}, categoryUnauthenticated},
		{"genai 403", genai.APIError{Code: 403}, categoryPermissionDenied},
		{"genai 429", genai.APIError{Code: 429}, categoryRateLimit},
		{"genai 504", genai.APIError{Code: 504}, categoryTimeout},
		{"genai 500", genai.APIError{Code: 500}, categoryUpstream},
		{"openai 401", &openai.Error{StatusCode: 401}, categoryUnauthenticated},
		{"openai 403", &openai.Error{StatusCode: 403}, categoryPermissionDenied},
		{"openai 429", &openai.Error{StatusCode: 429}, categoryRateLimit},
		{"openai 500", &openai.Error{StatusCode: 500}, categoryUpstream},
		{"unknown error", errors.New("something unexpected"), categoryUpstream},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.err); got != tc.want {
				t.Errorf("classify(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
