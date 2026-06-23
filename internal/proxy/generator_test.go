package proxy

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/ai"
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
