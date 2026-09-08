package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/core"
	"github.com/openai/openai-go"
	"google.golang.org/genai"

	"github.com/nicolapasqua99/genkit-proxy/internal/auth"
)

// fakeGenerator records what it was called with and returns canned results.
type fakeGenerator struct {
	resp         GenerateResponse
	err          error
	streamDeltas []string
	gotKey       string
	gotRequest   GenerateRequest
}

func (fakeGen *fakeGenerator) Generate(_ context.Context, req GenerateRequest, apiKey string) (GenerateResponse, error) {
	fakeGen.gotKey = apiKey
	fakeGen.gotRequest = req
	return fakeGen.resp, fakeGen.err
}

func (fakeGen *fakeGenerator) GenerateStream(_ context.Context, req GenerateRequest, apiKey string, onChunk func(string) error) (GenerateResponse, error) {
	fakeGen.gotKey = apiKey
	fakeGen.gotRequest = req
	for _, delta := range fakeGen.streamDeltas {
		if err := onChunk(delta); err != nil {
			return GenerateResponse{}, err
		}
	}
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
			name:             "ok with tool calls",
			method:           http.MethodPost,
			auth:             "Bearer secret-key",
			body:             `{"modelName":"googleai/gemini-2.5-flash","userMessage":"weather?","tools":[{"name":"get_weather","inputSchema":{"type":"object"}}]}`,
			genResp:          GenerateResponse{Model: "googleai/gemini-2.5-flash", ToolCalls: []ToolCall{{Name: "get_weather", Ref: "a1", Input: json.RawMessage(`{"city":"SF"}`)}}},
			wantStatus:       http.StatusOK,
			wantBodyContains: `"toolCalls":[{"name":"get_weather","ref":"a1","input":{"city":"SF"}}]`,
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
			handler := NewHandler(fake, nil, HandlerRLConfig{}, nil)
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
			if !reflect.DeepEqual(got, testCase.genResp) {
				t.Errorf("response = %+v, want %+v", got, testCase.genResp)
			}
			if fake.gotKey != "secret-key" {
				t.Errorf("generator received key %q, want secret-key", fake.gotKey)
			}
		})
	}
}

func TestHandlerModelAllowlist(t *testing.T) {
	allow := NewModelAllowlist([]string{"googleai/gemini-2.5-flash", "openai"})
	cases := []struct {
		name       string
		model      string
		wantStatus int
		wantCalled bool
	}{
		{"exact model allowed", "googleai/gemini-2.5-flash", http.StatusOK, true},
		{"provider wildcard allowed", "openai/gpt-4o", http.StatusOK, true},
		{"model not permitted", "googleai/gemini-2.5-pro", http.StatusForbidden, false},
		{"provider not permitted", "anthropic/claude-3", http.StatusForbidden, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGenerator{resp: GenerateResponse{Model: tc.model, Output: "hi"}}
			handler := NewHandler(fake, nil, HandlerRLConfig{}, allow)
			body := fmt.Sprintf(`{"modelName":%q,"userMessage":"hi"}`, tc.model)
			req := httptest.NewRequest(http.MethodPost, "/v1/generate", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer k")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if called := fake.gotKey != ""; called != tc.wantCalled {
				t.Errorf("generator called = %v, want %v", called, tc.wantCalled)
			}
		})
	}
}

func TestHandlerWritesUsageToSlot(t *testing.T) {
	want := &Usage{Input: 12, Output: 34, Total: 46}
	fake := &fakeGenerator{resp: GenerateResponse{
		Model:  "googleai/gemini-2.5-flash",
		Output: "hello",
		Usage:  want,
	}}
	handler := NewHandler(fake, nil, HandlerRLConfig{}, nil)

	slot := &modelSlot{}
	ctx := context.WithValue(context.Background(), modelKey, slot)
	req := httptest.NewRequest(http.MethodPost, "/v1/generate",
		strings.NewReader(`{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer k")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if slot.usage == nil || *slot.usage != *want {
		t.Errorf("slot.usage = %+v, want %+v", slot.usage, want)
	}
}

func TestStatusFor(t *testing.T) {
	openaiApiErrUnauth := &openai.Error{
		StatusCode: http.StatusUnauthorized,
		Request:    httptest.NewRequest(http.MethodPost, "/", nil),
		Response:   &http.Response{StatusCode: http.StatusUnauthorized},
	}
	openaiApiErrForbidden := &openai.Error{
		StatusCode: http.StatusForbidden,
		Request:    httptest.NewRequest(http.MethodPost, "/", nil),
		Response:   &http.Response{StatusCode: http.StatusForbidden},
	}
	openaiApiErr429 := &openai.Error{
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
		{"openai error 401", openaiApiErrUnauth, http.StatusUnauthorized},
		{"openai error 403", openaiApiErrForbidden, http.StatusForbidden},
		{"openai error 429", openaiApiErr429, http.StatusTooManyRequests},
		{"unknown error", errors.New("boom"), http.StatusBadGateway},
		{"auth unknown tenant", auth.ErrUnknownTenant, http.StatusUnauthorized},
		{"auth no provider secret", auth.ErrNoProviderSecret, http.StatusForbidden},
		{"auth secret unavailable", auth.ErrSecretUnavailable, http.StatusInternalServerError},
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
		{
			name:         "unknown tenant uses gateway message",
			err:          fmt.Errorf("tenant=%s: %w", secret, auth.ErrUnknownTenant),
			wantContains: "gateway authentication failed",
			wantAbsent:   secret,
		},
		{
			name:         "no provider secret uses safe message",
			err:          fmt.Errorf("ref=%s: %w", secret, auth.ErrNoProviderSecret),
			wantContains: "no provider credential",
			wantAbsent:   secret,
		},
		{
			name:         "secret unavailable uses safe message",
			err:          fmt.Errorf("ref=%s: %w", secret, auth.ErrSecretUnavailable),
			wantContains: "internal credential resolution error",
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

func TestHandlerGatewayAuth(t *testing.T) {
	const body = `{"modelName":"openai/gpt-4o","userMessage":"hi"}`
	known := map[string]bool{"good": true}

	serve := func(t *testing.T, gen *fakeGenerator, resolver CredentialResolver, token string) *httptest.ResponseRecorder {
		t.Helper()
		h := NewHandler(gen, nil, HandlerRLConfig{}, nil).WithCredentialResolver(resolver)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/generate", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(rec, req)
		return rec
	}

	t.Run("unknown tenant rejected before generation", func(t *testing.T) {
		gen := &fakeGenerator{resp: GenerateResponse{Output: "ok"}}
		rec := serve(t, gen, fakeCredentialResolver{known: known}, "bad")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
		if gen.gotRequest.ModelName != "" {
			t.Error("generator should not be called for an unknown tenant")
		}
	})

	t.Run("known tenant uses resolved provider key", func(t *testing.T) {
		gen := &fakeGenerator{resp: GenerateResponse{Output: "ok"}}
		rec := serve(t, gen, fakeCredentialResolver{known: known, key: "sk-real"}, "good")
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if gen.gotKey != "sk-real" {
			t.Errorf("generator key = %q, want %q", gen.gotKey, "sk-real")
		}
	})

	t.Run("resolution error mapped to status, generator not called", func(t *testing.T) {
		gen := &fakeGenerator{}
		rec := serve(t, gen, fakeCredentialResolver{known: known, resolveErr: auth.ErrNoProviderSecret}, "good")
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
		if gen.gotRequest.ModelName != "" {
			t.Error("generator should not be called when resolution fails")
		}
	})
}
