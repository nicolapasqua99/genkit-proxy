package proxy

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/core"
)

func TestHandlerServeStream(t *testing.T) {
	cases := []struct {
		name            string
		auth            string
		body            string
		deltas          []string
		genResp         GenerateResponse
		genErr          error
		wantStatus      int
		wantContentType string
		wantContains    []string
		wantAbsent      string
	}{
		{
			name:            "ok streams chunk and done events",
			auth:            "Bearer secret-key",
			body:            `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			deltas:          []string{"He", "llo"},
			genResp:         GenerateResponse{Model: "googleai/gemini-2.5-flash", FinishReason: "stop", Usage: &Usage{Input: 1, Output: 2, Total: 3}},
			wantStatus:      http.StatusOK,
			wantContentType: "text/event-stream",
			wantContains: []string{
				"event: chunk\ndata: {\"delta\":\"He\"}\n\n",
				"event: chunk\ndata: {\"delta\":\"llo\"}\n\n",
				"event: done\ndata: ",
				`"finishReason":"stop"`,
				`"usage":{"input":1,"output":2,"total":3}`,
			},
		},
		{
			name:            "tool calls surface in done event",
			auth:            "Bearer secret-key",
			body:            `{"modelName":"googleai/gemini-2.5-flash","userMessage":"weather?","tools":[{"name":"get_weather","inputSchema":{"type":"object"}}]}`,
			genResp:         GenerateResponse{Model: "googleai/gemini-2.5-flash", FinishReason: "stop", ToolCalls: []ToolCall{{Name: "get_weather", Ref: "a1", Input: json.RawMessage(`{"city":"SF"}`)}}},
			wantStatus:      http.StatusOK,
			wantContentType: "text/event-stream",
			wantContains: []string{
				"event: done\ndata: ",
				`"toolCalls":[{"name":"get_weather","ref":"a1","input":{"city":"SF"}}]`,
			},
		},
		{
			name:       "missing auth",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "validation error",
			auth:       "Bearer k",
			body:       `{"modelName":"nope","userMessage":"hi"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:         "pre-stream upstream error returns json status",
			auth:         "Bearer k",
			body:         `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			genErr:       core.NewError(core.UNAUTHENTICATED, "bad key from provider"),
			wantStatus:   http.StatusUnauthorized,
			wantContains: []string{"credentials"},
			wantAbsent:   "bad key from provider",
		},
		{
			name:       "mid-stream error becomes an error event",
			auth:       "Bearer k",
			body:       `{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`,
			deltas:     []string{"Hi"},
			genErr:     core.NewError(core.RESOURCE_EXHAUSTED, "quota exceeded"),
			wantStatus: http.StatusOK,
			wantContains: []string{
				"event: chunk\ndata: {\"delta\":\"Hi\"}\n\n",
				"event: error\ndata: ",
				"rate limit",
			},
			wantAbsent: "quota exceeded",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fake := &fakeGenerator{resp: testCase.genResp, err: testCase.genErr, streamDeltas: testCase.deltas}
			handler := NewHandler(fake, nil, HandlerRLConfig{}, nil)
			req := httptest.NewRequest(http.MethodPost, "/v1/generate/stream", strings.NewReader(testCase.body))
			if testCase.auth != "" {
				req.Header.Set("Authorization", testCase.auth)
			}
			recorder := httptest.NewRecorder()

			handler.ServeStream(recorder, req)

			if recorder.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
			body := recorder.Body.String()
			if testCase.wantContentType != "" && !strings.Contains(recorder.Header().Get("Content-Type"), testCase.wantContentType) {
				t.Errorf("Content-Type = %q, want %q", recorder.Header().Get("Content-Type"), testCase.wantContentType)
			}
			for _, want := range testCase.wantContains {
				if !strings.Contains(body, want) {
					t.Errorf("body %q does not contain %q", body, want)
				}
			}
			if testCase.wantAbsent != "" && strings.Contains(body, testCase.wantAbsent) {
				t.Errorf("body %q should not contain %q", body, testCase.wantAbsent)
			}
		})
	}
}

func TestStatusWriterUnwrapFlushes(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapped := &statusWriter{ResponseWriter: recorder}

	if err := http.NewResponseController(wrapped).Flush(); err != nil {
		t.Fatalf("Flush through statusWriter: %v", err)
	}
	if !recorder.Flushed {
		t.Error("flush did not reach the underlying ResponseWriter via Unwrap")
	}
}

func TestStreamGeneratorErrorIsSanitized(t *testing.T) {
	// A pre-stream error must not leak the raw provider message to the client.
	fake := &fakeGenerator{err: errors.New("internal detail leak")}
	handler := NewHandler(fake, nil, HandlerRLConfig{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/generate/stream",
		strings.NewReader(`{"modelName":"googleai/gemini-2.5-flash","userMessage":"hi"}`))
	req.Header.Set("Authorization", "Bearer k")
	recorder := httptest.NewRecorder()

	handler.ServeStream(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "internal detail leak") {
		t.Errorf("raw error leaked: %q", recorder.Body.String())
	}
}

func TestStreamModelAllowlist(t *testing.T) {
	allow := NewModelAllowlist([]string{"googleai/gemini-2.5-flash"})
	fake := &fakeGenerator{streamDeltas: []string{"hi"}, resp: GenerateResponse{Model: "googleai/gemini-2.5-pro"}}
	handler := NewHandler(fake, nil, HandlerRLConfig{}, allow)
	req := httptest.NewRequest(http.MethodPost, "/v1/generate/stream",
		strings.NewReader(`{"modelName":"googleai/gemini-2.5-pro","userMessage":"hi"}`))
	req.Header.Set("Authorization", "Bearer k")
	recorder := httptest.NewRecorder()

	handler.ServeStream(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %q)", recorder.Code, recorder.Body.String())
	}
	if fake.gotKey != "" {
		t.Error("generator should not be called for a disallowed model")
	}
}
