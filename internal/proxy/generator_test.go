package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core"
)

func TestOutputAndData(t *testing.T) {
	cases := []struct {
		name       string
		req        GenerateRequest
		text       string
		wantOutput string
		wantData   json.RawMessage
	}{
		{"text mode", GenerateRequest{}, "hello", "hello", nil},
		{"json mode valid", GenerateRequest{ResponseFormat: "json"}, `{"a":1}`, "", json.RawMessage(`{"a":1}`)},
		{"json mode invalid falls back to output", GenerateRequest{ResponseFormat: "json"}, "not json", "not json", nil},
		{"json mode empty", GenerateRequest{ResponseFormat: "json"}, "", "", nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			gotOutput, gotData := outputAndData(testCase.req, testCase.text)
			if gotOutput != testCase.wantOutput {
				t.Errorf("output = %q, want %q", gotOutput, testCase.wantOutput)
			}
			if !reflect.DeepEqual(gotData, testCase.wantData) {
				t.Errorf("data = %s, want %s", gotData, testCase.wantData)
			}
		})
	}
}

func TestMessagesFrom(t *testing.T) {
	t.Run("no messages", func(t *testing.T) {
		if got := messagesFrom(GenerateRequest{UserMessage: "hi"}); got != nil {
			t.Errorf("messagesFrom() = %v, want nil", got)
		}
	})

	t.Run("maps role and text in order", func(t *testing.T) {
		req := GenerateRequest{Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "model", Content: "hello"},
		}}
		got := messagesFrom(req)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0].Role != ai.RoleUser || got[0].Text() != "hi" {
			t.Errorf("msg[0] = {%s, %q}, want {user, hi}", got[0].Role, got[0].Text())
		}
		if got[1].Role != ai.RoleModel || got[1].Text() != "hello" {
			t.Errorf("msg[1] = {%s, %q}, want {model, hello}", got[1].Role, got[1].Text())
		}
	})

	t.Run("maps multimodal parts", func(t *testing.T) {
		req := GenerateRequest{Messages: []Message{{
			Role: "user",
			Parts: []Part{
				{Text: "what is this?"},
				{Media: &Media{ContentType: "image/png", URL: "data:image/png;base64,AAAA"}},
			},
		}}}
		got := messagesFrom(req)
		if len(got) != 1 || len(got[0].Content) != 2 {
			t.Fatalf("parts = %d, want a single message with 2 parts", len(got))
		}
		text, media := got[0].Content[0], got[0].Content[1]
		if !text.IsText() || text.Text != "what is this?" {
			t.Errorf("part[0] = %+v, want text %q", text, "what is this?")
		}
		if !media.IsMedia() || media.ContentType != "image/png" {
			t.Errorf("part[1] = %+v, want media image/png", media)
		}
	})

	t.Run("maps tool request and response parts", func(t *testing.T) {
		req := GenerateRequest{Messages: []Message{
			{Role: "model", Parts: []Part{{ToolRequest: &ToolCall{Name: "get_weather", Ref: "a1", Input: json.RawMessage(`{"city":"SF"}`)}}}},
			{Role: "tool", Parts: []Part{{ToolResponse: &ToolResult{Name: "get_weather", Ref: "a1", Output: json.RawMessage(`{"tempC":18}`)}}}},
		}}
		got := messagesFrom(req)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		reqPart := got[0].Content[0]
		if got[0].Role != ai.RoleModel || !reqPart.IsToolRequest() {
			t.Fatalf("msg[0] = {%s, %+v}, want a model tool request", got[0].Role, reqPart)
		}
		if reqPart.ToolRequest.Name != "get_weather" || reqPart.ToolRequest.Ref != "a1" {
			t.Errorf("toolRequest = %+v, want get_weather/a1", reqPart.ToolRequest)
		}
		if input, ok := reqPart.ToolRequest.Input.(map[string]any); !ok || input["city"] != "SF" {
			t.Errorf("toolRequest input = %v, want {city: SF}", reqPart.ToolRequest.Input)
		}
		respPart := got[1].Content[0]
		if got[1].Role != ai.RoleTool || !respPart.IsToolResponse() {
			t.Fatalf("msg[1] = {%s, %+v}, want a tool response", got[1].Role, respPart)
		}
		if respPart.ToolResponse.Name != "get_weather" || respPart.ToolResponse.Ref != "a1" {
			t.Errorf("toolResponse = %+v, want get_weather/a1", respPart.ToolResponse)
		}
	})
}

func TestToolCallsFrom(t *testing.T) {
	t.Run("nil when no requests", func(t *testing.T) {
		if got := toolCallsFrom(nil); got != nil {
			t.Errorf("toolCallsFrom(nil) = %v, want nil", got)
		}
	})

	t.Run("maps name, ref, and input", func(t *testing.T) {
		got := toolCallsFrom([]*ai.ToolRequest{
			{Name: "get_weather", Ref: "a1", Input: map[string]any{"city": "SF"}},
			{Name: "noargs"},
		})
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		if got[0].Name != "get_weather" || got[0].Ref != "a1" || string(got[0].Input) != `{"city":"SF"}` {
			t.Errorf("call[0] = %+v, want get_weather/a1/{city:SF}", got[0])
		}
		if got[1].Name != "noargs" || got[1].Input != nil {
			t.Errorf("call[1] = %+v, want noargs with nil input", got[1])
		}
	})
}

func TestConfigFor(t *testing.T) {
	cases := []struct {
		name string
		req  GenerateRequest
		want *ai.GenerationCommonConfig
	}{
		{"no tuning fields", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi"}, nil},
		{
			"temperature only",
			GenerateRequest{Temperature: temp(0.5)},
			&ai.GenerationCommonConfig{Temperature: 0.5},
		},
		{
			"stop sequences only",
			GenerateRequest{StopSequences: []string{"\n\n", "END"}},
			&ai.GenerationCommonConfig{StopSequences: []string{"\n\n", "END"}},
		},
		{
			"all fields",
			GenerateRequest{Temperature: temp(0.7), MaxOutputTokens: intp(256), TopP: temp(0.9), TopK: intp(40), StopSequences: []string{"STOP"}},
			&ai.GenerationCommonConfig{Temperature: 0.7, MaxOutputTokens: 256, TopP: 0.9, TopK: 40, StopSequences: []string{"STOP"}},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := configFor(testCase.req)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("configFor(%+v) = %+v, want %+v", testCase.req, got, testCase.want)
			}
		})
	}
}

func TestUsageFrom(t *testing.T) {
	cases := []struct {
		name string
		in   *ai.GenerationUsage
		want *Usage
	}{
		{"nil usage", nil, nil},
		{
			"populated usage",
			&ai.GenerationUsage{InputTokens: 12, OutputTokens: 34, TotalTokens: 46},
			&Usage{Input: 12, Output: 34, Total: 46},
		},
		{
			"zero usage",
			&ai.GenerationUsage{},
			&Usage{},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := usageFrom(testCase.in)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Errorf("usageFrom(%+v) = %+v, want %+v", testCase.in, got, testCase.want)
			}
		})
	}
}

func TestGenerateResponseUsageMarshalling(t *testing.T) {
	t.Run("omits usage when nil", func(t *testing.T) {
		out, err := json.Marshal(GenerateResponse{Model: "googleai/gemini-2.5-flash", Output: "hi"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(out), "usage") {
			t.Errorf("response %s should not contain usage", out)
		}
	})
	t.Run("includes usage when set", func(t *testing.T) {
		out, err := json.Marshal(GenerateResponse{
			Model:  "googleai/gemini-2.5-flash",
			Output: "hi",
			Usage:  &Usage{Input: 12, Output: 34, Total: 46},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		const want = `"usage":{"input":12,"output":34,"total":46}`
		if !strings.Contains(string(out), want) {
			t.Errorf("response %s should contain %s", out, want)
		}
	})
}

// makeModelResponse builds a minimal *ai.ModelResponse with a single text
// candidate for use in seam tests.
func makeModelResponse(text string, finishReason ai.FinishReason, usage *ai.GenerationUsage) *ai.ModelResponse {
	resp := &ai.ModelResponse{
		FinishReason: finishReason,
		Usage:        usage,
	}
	if text != "" {
		resp.Message = ai.NewTextMessage(ai.RoleModel, text)
	}
	return resp
}

func TestGenkitGeneratorGenerate(t *testing.T) {
	const model = "googleai/gemini-2.5-flash"
	stdUsage := &ai.GenerationUsage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15}

	cases := []struct {
		name      string
		req       GenerateRequest
		timeout   time.Duration
		runResult *ai.ModelResponse
		runErr    error
		wantErr   bool
		// wantErrIs is checked with errors.Is when set.
		wantErrIs error
		// wantErrClassify is checked when set.
		wantErrClassify errCategory
		wantResp        GenerateResponse
	}{
		{
			name:      "unsupported provider not wrapped",
			req:       GenerateRequest{ModelName: model, UserMessage: "hi"},
			runErr:    ErrUnsupportedProvider,
			wantErr:   true,
			wantErrIs: ErrUnsupportedProvider,
		},
		{
			name:            "upstream auth error wrapped and classified",
			req:             GenerateRequest{ModelName: model, UserMessage: "hi"},
			runErr:          core.NewError(core.UNAUTHENTICATED, "bad key"),
			wantErr:         true,
			wantErrClassify: categoryUnauthenticated,
		},
		{
			name:            "timeout applied to context",
			req:             GenerateRequest{ModelName: model, UserMessage: "hi"},
			timeout:         1 * time.Millisecond,
			runErr:          nil, // fake blocks on ctx.Done
			wantErr:         true,
			wantErrClassify: categoryTimeout,
		},
		{
			name:      "success plain text",
			req:       GenerateRequest{ModelName: model, UserMessage: "hi"},
			runResult: makeModelResponse("hello", ai.FinishReasonStop, stdUsage),
			wantResp: GenerateResponse{
				Model:        model,
				Output:       "hello",
				FinishReason: string(ai.FinishReasonStop),
				Usage:        &Usage{Input: 10, Output: 5, Total: 15},
			},
		},
		{
			name:      "success JSON format routes to data field",
			req:       GenerateRequest{ModelName: model, UserMessage: "hi", ResponseFormat: "json"},
			runResult: makeModelResponse(`{"x":1}`, ai.FinishReasonStop, nil),
			wantResp: GenerateResponse{
				Model:        model,
				Data:         json.RawMessage(`{"x":1}`),
				FinishReason: string(ai.FinishReasonStop),
			},
		},
		{
			name:      "nil usage omitted from response",
			req:       GenerateRequest{ModelName: model, UserMessage: "hi"},
			runResult: makeModelResponse("ok", ai.FinishReasonStop, nil),
			wantResp: GenerateResponse{
				Model:        model,
				Output:       "ok",
				FinishReason: string(ai.FinishReasonStop),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blockCh := make(chan struct{})
			g := GenkitGenerator{
				GenerateTimeout: tc.timeout,
				run: func(ctx context.Context, _ GenerateRequest, _ string) (*ai.ModelResponse, error) {
					if tc.timeout > 0 {
						// Block until the context deadline fires.
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						case <-blockCh:
						}
					}
					return tc.runResult, tc.runErr
				},
			}

			resp, err := g.Generate(context.Background(), tc.req, "test-key")

			if tc.wantErr {
				if err == nil {
					t.Fatalf("Generate() error = nil, want error")
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Errorf("errors.Is(%v, %v) = false, want true", err, tc.wantErrIs)
				}
				if tc.wantErrClassify != 0 && classify(err) != tc.wantErrClassify {
					t.Errorf("classify(err) = %v, want %v", classify(err), tc.wantErrClassify)
				}
				return
			}
			if err != nil {
				t.Fatalf("Generate() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.wantResp) {
				t.Errorf("Generate() = %+v, want %+v", resp, tc.wantResp)
			}
		})
	}
}

func TestGenkitGeneratorGenerateStream(t *testing.T) {
	const model = "googleai/gemini-2.5-flash"
	stdUsage := &ai.GenerationUsage{InputTokens: 8, OutputTokens: 4, TotalTokens: 12}

	cases := []struct {
		name string
		req  GenerateRequest
		// runFn drives the fake runStream. Receives the onChunk callback.
		runFn   func(ctx context.Context, onChunk func(string) error) (*ai.ModelResponse, error)
		timeout time.Duration
		wantErr bool
		// wantErrIs is checked with errors.Is when set.
		wantErrIs error
		// wantErrClassify is checked when set.
		wantErrClassify errCategory
		wantDeltas      []string
		wantResp        GenerateResponse
	}{
		{
			name: "unsupported provider not wrapped",
			req:  GenerateRequest{ModelName: model, UserMessage: "hi"},
			runFn: func(_ context.Context, _ func(string) error) (*ai.ModelResponse, error) {
				return nil, ErrUnsupportedProvider
			},
			wantErr:   true,
			wantErrIs: ErrUnsupportedProvider,
		},
		{
			name: "upstream error wrapped and classified",
			req:  GenerateRequest{ModelName: model, UserMessage: "hi"},
			runFn: func(_ context.Context, _ func(string) error) (*ai.ModelResponse, error) {
				return nil, core.NewError(core.PERMISSION_DENIED, "no access")
			},
			wantErr:         true,
			wantErrClassify: categoryPermissionDenied,
		},
		{
			name:    "timeout applied to context",
			req:     GenerateRequest{ModelName: model, UserMessage: "hi"},
			timeout: 1 * time.Millisecond,
			runFn: func(ctx context.Context, _ func(string) error) (*ai.ModelResponse, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(5 * time.Second):
					return nil, nil
				}
			},
			wantErr:         true,
			wantErrClassify: categoryTimeout,
		},
		{
			name: "multiple deltas delivered in order",
			req:  GenerateRequest{ModelName: model, UserMessage: "hi"},
			runFn: func(_ context.Context, onChunk func(string) error) (*ai.ModelResponse, error) {
				for _, d := range []string{"hello", " ", "world"} {
					if err := onChunk(d); err != nil {
						return nil, err
					}
				}
				return makeModelResponse("", ai.FinishReasonStop, stdUsage), nil
			},
			wantDeltas: []string{"hello", " ", "world"},
			wantResp: GenerateResponse{
				Model:        model,
				FinishReason: string(ai.FinishReasonStop),
				Usage:        &Usage{Input: 8, Output: 4, Total: 12},
			},
		},
		{
			name: "onChunk error aborts stream",
			req:  GenerateRequest{ModelName: model, UserMessage: "hi"},
			runFn: func(_ context.Context, onChunk func(string) error) (*ai.ModelResponse, error) {
				if err := onChunk("oops"); err != nil {
					return nil, err
				}
				return nil, nil
			},
			// The test's onChunk always returns an error; the error propagates back.
			wantErr: true,
		},
		{
			name: "nil final response gives zero-value fields",
			req:  GenerateRequest{ModelName: model, UserMessage: "hi"},
			runFn: func(_ context.Context, _ func(string) error) (*ai.ModelResponse, error) {
				return nil, nil
			},
			wantResp: GenerateResponse{Model: model},
		},
		{
			name: "final response fields all mapped",
			req:  GenerateRequest{ModelName: model, UserMessage: "hi"},
			runFn: func(_ context.Context, onChunk func(string) error) (*ai.ModelResponse, error) {
				_ = onChunk("text")
				return makeModelResponse("", ai.FinishReasonLength, stdUsage), nil
			},
			wantDeltas: []string{"text"},
			wantResp: GenerateResponse{
				Model:        model,
				FinishReason: string(ai.FinishReasonLength),
				Usage:        &Usage{Input: 8, Output: 4, Total: 12},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunkErr := errors.New("chunk write error")
			var gotDeltas []string

			onChunk := func(delta string) error {
				if tc.name == "onChunk error aborts stream" {
					return chunkErr
				}
				gotDeltas = append(gotDeltas, delta)
				return nil
			}

			g := GenkitGenerator{
				GenerateTimeout: tc.timeout,
				runStream: func(ctx context.Context, _ GenerateRequest, _ string, cb func(string) error) (*ai.ModelResponse, error) {
					return tc.runFn(ctx, cb)
				},
			}

			resp, err := g.GenerateStream(context.Background(), tc.req, "test-key", onChunk)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("GenerateStream() error = nil, want error")
				}
				if tc.wantErrIs != nil && !errors.Is(err, tc.wantErrIs) {
					t.Errorf("errors.Is(%v, %v) = false, want true", err, tc.wantErrIs)
				}
				if tc.wantErrClassify != 0 && classify(err) != tc.wantErrClassify {
					t.Errorf("classify(err) = %v, want %v", classify(err), tc.wantErrClassify)
				}
				return
			}
			if err != nil {
				t.Fatalf("GenerateStream() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(gotDeltas, tc.wantDeltas) {
				t.Errorf("deltas = %v, want %v", gotDeltas, tc.wantDeltas)
			}
			if !reflect.DeepEqual(resp, tc.wantResp) {
				t.Errorf("GenerateStream() = %+v, want %+v", resp, tc.wantResp)
			}
		})
	}
}
