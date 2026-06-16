package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGenerator records what it was called with and returns canned results.
type fakeGenerator struct {
	resp   GenerateResponse
	err    error
	gotKey string
	gotReq GenerateRequest
}

func (f *fakeGenerator) Generate(_ context.Context, req GenerateRequest, apiKey string) (GenerateResponse, error) {
	f.gotKey = apiKey
	f.gotReq = req
	return f.resp, f.err
}

func TestHandlerServeHTTP(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		auth       string
		body       string
		genResp    GenerateResponse
		genErr     error
		wantStatus int
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
			name:       "upstream error",
			method:     http.MethodPost,
			auth:       "Bearer k",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genErr:     errors.New("boom"),
			wantStatus: http.StatusBadGateway,
		},
		{
			name:       "method not allowed",
			method:     http.MethodGet,
			auth:       "Bearer k",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGenerator{resp: tc.genResp, err: tc.genErr}
			h := NewHandler(fake)
			req := httptest.NewRequest(tc.method, "/v1/generate", strings.NewReader(tc.body))
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var got GenerateResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got != tc.genResp {
				t.Errorf("response = %+v, want %+v", got, tc.genResp)
			}
			if fake.gotKey != "secret-key" {
				t.Errorf("generator received key %q, want secret-key", fake.gotKey)
			}
		})
	}
}
