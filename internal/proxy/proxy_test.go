package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/core"
	"github.com/openai/openai-go"
	"google.golang.org/genai"
)

// fakeGenerator records what it was called with and returns canned results.
type fakeGenerator struct {
	resp   GenerateResponse
	err    error
	gotKey string
	gotReq GenerateRequest
}

func (fakeGen *fakeGenerator) Generate(_ context.Context, req GenerateRequest, apiKey string) (GenerateResponse, error) {
	fakeGen.gotKey = apiKey
	fakeGen.gotReq = req
	return fakeGen.resp, fakeGen.err
}

func TestHandlerServeHTTP(t *testing.T) {
	cases := []struct {
		name             string
		method           string
		auth             string
		body             string
		genResp          GenerateResponse
		genErr           error
		wantStatus       int
		wantBodyContains string
		wantBodyAbsent   string
	}{
		{
			name:       "ok",
			method:     http.MethodPost,
			auth:       "Bearer secret-key",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genResp:    GenerateResponse{Model: "googleai/gemini-2.5-flash", Output: "hello"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing auth",
			method:     http.MethodPost,
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "non-bearer auth",
			method:     http.MethodPost,
			auth:       "Basic abc",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "bearer lowercase scheme",
			method:     http.MethodPost,
			auth:       "bearer secret-key",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genResp:    GenerateResponse{Model: "googleai/gemini-2.5-flash", Output: "hello"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "bearer uppercase scheme",
			method:     http.MethodPost,
			auth:       "BEARER secret-key",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genResp:    GenerateResponse{Model: "googleai/gemini-2.5-flash", Output: "hello"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "bearer whitespace only token",
			method:     http.MethodPost,
			auth:       "Bearer   ",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed json",
			method:     http.MethodPost,
			auth:       "Bearer k",
			body:       `{"modelName":}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown field",
			method:     http.MethodPost,
			auth:       "Bearer k",
			body:       `{"modelName":"googleai/x","userMessage":"hi","foo":1}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "validation error",
			method:     http.MethodPost,
			auth:       "Bearer k",
			body:       `{"modelName":"nope","userMessage":"hi"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty model segment",
			method:     http.MethodPost,
			auth:       "Bearer k",
			body:       `{"modelName":"googleai/","userMessage":"hi"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:             "upstream error",
			method:           http.MethodPost,
			auth:             "Bearer k",
			body:             `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genErr:           errors.New("boom"),
			wantStatus:       http.StatusBadGateway,
			wantBodyContains: "upstream provider error",
		},
		{
			name:             "upstream unauth",
			method:           http.MethodPost,
			auth:             "Bearer k",
			body:             `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genErr:           core.NewError(core.UNAUTHENTICATED, "bad key from provider"),
			wantStatus:       http.StatusUnauthorized,
			wantBodyContains: "credentials",
			wantBodyAbsent:   "bad key from provider",
		},
		{
			name:             "upstream forbidden",
			method:           http.MethodPost,
			auth:             "Bearer k",
			body:             `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genErr:           core.NewError(core.PERMISSION_DENIED, "perms denied"),
			wantStatus:       http.StatusForbidden,
			wantBodyContains: "denied",
			wantBodyAbsent:   "perms denied",
		},
		{
			name:           "upstream rate limit",
			method:         http.MethodPost,
			auth:           "Bearer k",
			body:           `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genErr:         core.NewError(core.RESOURCE_EXHAUSTED, "quota exceeded"),
			wantStatus:     http.StatusTooManyRequests,
			wantBodyAbsent: "quota exceeded",
		},
		{
			name:       "upstream timeout context",
			method:     http.MethodPost,
			auth:       "Bearer k",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genErr:     fmt.Errorf("upstream: %w", context.DeadlineExceeded),
			wantStatus: http.StatusGatewayTimeout,
		},
		{
			name:           "upstream not found",
			method:         http.MethodPost,
			auth:           "Bearer k",
			body:           `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genErr:         core.NewError(core.NOT_FOUND, "no such model"),
			wantStatus:     http.StatusNotFound,
			wantBodyAbsent: "no such model",
		},
		{
			name:       "upstream genai apierror",
			method:     http.MethodPost,
			auth:       "Bearer k",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genErr:     genai.APIError{Code: http.StatusUnauthorized},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "method not allowed",
			method:     http.MethodGet,
			auth:       "Bearer k",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fake := &fakeGenerator{resp: testCase.genResp, err: testCase.genErr}
			handler := NewHandler(fake)
			req := httptest.NewRequest(testCase.method, "/v1/generate", strings.NewReader(testCase.body))
			if testCase.auth != "" {
				req.Header.Set("Authorization", testCase.auth)
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
			body := recorder.Body.String()
			if testCase.wantBodyContains != "" && !strings.Contains(body, testCase.wantBodyContains) {
				t.Errorf("body %q does not contain %q", body, testCase.wantBodyContains)
			}
			if testCase.wantBodyAbsent != "" && strings.Contains(body, testCase.wantBodyAbsent) {
				t.Errorf("body %q should not contain %q", body, testCase.wantBodyAbsent)
			}
			if testCase.wantStatus != http.StatusOK {
				return
			}
			var got GenerateResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got != testCase.genResp {
				t.Errorf("response = %+v, want %+v", got, testCase.genResp)
			}
			if fake.gotKey != "secret-key" {
				t.Errorf("generator received key %q, want secret-key", fake.gotKey)
			}
		})
	}
}

func TestStatusFor(t *testing.T) {
	oeUnauth := &openai.Error{
		StatusCode: http.StatusUnauthorized,
		Request:    httptest.NewRequest(http.MethodPost, "/", nil),
		Response:   &http.Response{StatusCode: http.StatusUnauthorized},
	}
	oeForbidden := &openai.Error{
		StatusCode: http.StatusForbidden,
		Request:    httptest.NewRequest(http.MethodPost, "/", nil),
		Response:   &http.Response{StatusCode: http.StatusForbidden},
	}
	oe429 := &openai.Error{
		StatusCode: http.StatusTooManyRequests,
		Request:    httptest.NewRequest(http.MethodPost, "/", nil),
		Response:   &http.Response{StatusCode: http.StatusTooManyRequests},
	}

	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"validation error", &ValidationError{Field: "f", Reason: "r"}, http.StatusBadRequest},
		{"unsupported provider", fmt.Errorf("%w: cohere", ErrUnsupportedProvider), http.StatusBadRequest},
		{"context deadline", fmt.Errorf("wrap: %w", context.DeadlineExceeded), http.StatusGatewayTimeout},
		{"context canceled", fmt.Errorf("wrap: %w", context.Canceled), http.StatusGatewayTimeout},
		{"genkit unauthenticated", core.NewError(core.UNAUTHENTICATED, "bad key"), http.StatusUnauthorized},
		{"genkit permission denied", core.NewError(core.PERMISSION_DENIED, "denied"), http.StatusForbidden},
		{"genkit rate limit", core.NewError(core.RESOURCE_EXHAUSTED, "quota"), http.StatusTooManyRequests},
		{"genkit timeout", core.NewError(core.DEADLINE_EXCEEDED, "timeout"), http.StatusGatewayTimeout},
		{"genkit not found", core.NewError(core.NOT_FOUND, "no model"), http.StatusNotFound},
		{"genai apierror 401", genai.APIError{Code: http.StatusUnauthorized}, http.StatusUnauthorized},
		{"genai apierror 403", genai.APIError{Code: http.StatusForbidden}, http.StatusForbidden},
		{"genai apierror 429", genai.APIError{Code: http.StatusTooManyRequests}, http.StatusTooManyRequests},
		{"openai error 401", oeUnauth, http.StatusUnauthorized},
		{"openai error 403", oeForbidden, http.StatusForbidden},
		{"openai error 429", oe429, http.StatusTooManyRequests},
		{"unknown error", errors.New("boom"), http.StatusBadGateway},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := statusFor(testCase.err)
			if got != testCase.wantStatus {
				t.Errorf("statusFor(%v) = %d, want %d", testCase.err, got, testCase.wantStatus)
			}
		})
	}
}

func TestSafeMessage(t *testing.T) {
	const secret = "secret-api-key-value"
	cases := []struct {
		name         string
		err          error
		wantContains string
		wantAbsent   string
	}{
		{
			name:         "validation error echoed verbatim",
			err:          &ValidationError{Field: "modelName", Reason: "must not be empty"},
			wantContains: "modelName",
		},
		{
			name:         "unsupported provider echoed verbatim",
			err:          fmt.Errorf("%w: %q", ErrUnsupportedProvider, "cohere"),
			wantContains: "unsupported",
		},
		{
			name:         "unauthenticated uses safe message",
			err:          fmt.Errorf("key=%s: %w", secret, core.NewError(core.UNAUTHENTICATED, secret)),
			wantContains: "credentials",
			wantAbsent:   secret,
		},
		{
			name:         "permission denied uses safe message",
			err:          core.NewError(core.PERMISSION_DENIED, secret),
			wantContains: "denied",
			wantAbsent:   secret,
		},
		{
			name:         "rate limit uses safe message",
			err:          core.NewError(core.RESOURCE_EXHAUSTED, secret),
			wantContains: "rate limit",
			wantAbsent:   secret,
		},
		{
			name:         "timeout uses safe message",
			err:          fmt.Errorf("detail=%s: %w", secret, context.DeadlineExceeded),
			wantContains: "timed out",
			wantAbsent:   secret,
		},
		{
			name:         "not found uses safe message",
			err:          core.NewError(core.NOT_FOUND, secret),
			wantContains: "not found",
			wantAbsent:   secret,
		},
		{
			name:         "unknown upstream uses safe message",
			err:          fmt.Errorf("internal detail=%s", secret),
			wantContains: "upstream provider error",
			wantAbsent:   secret,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := safeMessage(testCase.err)
			if testCase.wantContains != "" && !strings.Contains(got, testCase.wantContains) {
				t.Errorf("safeMessage() = %q, want it to contain %q", got, testCase.wantContains)
			}
			if testCase.wantAbsent != "" && strings.Contains(got, testCase.wantAbsent) {
				t.Errorf("safeMessage() = %q, should not contain %q", got, testCase.wantAbsent)
			}
		})
	}
}
