package proxy

import (
	"encoding/json"
	"testing"
)

func temp(value float64) *float64 { return &value }

func intp(value int) *int { return &value }

func TestGenerateRequestValidate(t *testing.T) {
	cases := []struct {
		name    string
		req     GenerateRequest
		wantErr bool
	}{
		{"valid", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi"}, false},
		{"valid with temperature", GenerateRequest{ModelName: "openai/gpt-4o", UserMessage: "hi", Temperature: temp(0.5)}, false},
		{"valid temperature bounds", GenerateRequest{ModelName: "anthropic/claude-3-5-sonnet", UserMessage: "hi", Temperature: temp(2)}, false},
		{"empty model", GenerateRequest{ModelName: "", UserMessage: "hi"}, true},
		{"blank model", GenerateRequest{ModelName: "   ", UserMessage: "hi"}, true},
		{"no provider prefix", GenerateRequest{ModelName: "gemini-2.5-flash", UserMessage: "hi"}, true},
		{"unsupported provider", GenerateRequest{ModelName: "cohere/command", UserMessage: "hi"}, true},
		{"empty user message", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "  "}, true},
		{"temperature too high", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", Temperature: temp(2.5)}, true},
		{"temperature negative", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", Temperature: temp(-0.1)}, true},
		{"empty model segment", GenerateRequest{ModelName: "googleai/", UserMessage: "hi"}, true},
		{"whitespace model segment", GenerateRequest{ModelName: "googleai/   ", UserMessage: "hi"}, true},
		{"valid generation config", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", MaxOutputTokens: intp(256), TopP: temp(0.9), TopK: intp(40), StopSequences: []string{"\n\n"}}, false},
		{"max output tokens zero", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", MaxOutputTokens: intp(0)}, true},
		{"max output tokens negative", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", MaxOutputTokens: intp(-1)}, true},
		{"top_p too high", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", TopP: temp(1.5)}, true},
		{"top_p negative", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", TopP: temp(-0.1)}, true},
		{"top_p bounds", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", TopP: temp(1)}, false},
		{"top_k zero", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", TopK: intp(0)}, true},
		{"json response format", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", ResponseFormat: "json"}, false},
		{"json with output schema", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", ResponseFormat: "json", OutputSchema: map[string]any{"type": "object"}}, false},
		{"unsupported response format", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", ResponseFormat: "xml"}, true},
		{"output schema without json", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", OutputSchema: map[string]any{"type": "object"}}, true},
		{"valid messages history", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "and then?", Messages: []Message{{Role: "user", Content: "hi"}, {Role: "model", Content: "hello"}}}, false},
		{"message bad role", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", Messages: []Message{{Role: "assistant", Content: "hello"}}}, true},
		{"message system role rejected", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", Messages: []Message{{Role: "system", Content: "hello"}}}, true},
		{"message empty content", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", Messages: []Message{{Role: "user", Content: "  "}}}, true},
		{"messages only no userMessage", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "user", Content: "hi"}}}, false},
		{"neither userMessage nor messages", GenerateRequest{ModelName: "googleai/gemini-2.5-flash"}, true},
		{"message both content and parts", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "user", Content: "hi", Parts: []Part{{Text: "hi"}}}}}, true},
		{"message neither content nor parts", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "user"}}}, true},
		{"valid multimodal parts", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "user", Parts: []Part{{Text: "what is this?"}, {Media: &Media{ContentType: "image/png", URL: "data:image/png;base64,AAAA"}}}}}}, false},
		{"part both text and media", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "user", Parts: []Part{{Text: "hi", Media: &Media{ContentType: "image/png", URL: "u"}}}}}}, true},
		{"part neither text nor media", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "user", Parts: []Part{{}}}}}, true},
		{"media missing contentType", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "user", Parts: []Part{{Media: &Media{URL: "u"}}}}}}, true},
		{"media missing url", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "user", Parts: []Part{{Media: &Media{ContentType: "image/png"}}}}}}, true},
		{"valid with tools", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "weather?", Tools: []ToolDefinition{{Name: "get_weather", Description: "gets weather", InputSchema: map[string]any{"type": "object"}}}}, false},
		{"valid tool without schema", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", Tools: []ToolDefinition{{Name: "ping"}}}, false},
		{"tool missing name", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", Tools: []ToolDefinition{{Description: "no name"}}}, true},
		{"duplicate tool names", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", Tools: []ToolDefinition{{Name: "dup"}, {Name: "dup"}}}, true},
		{"valid toolChoice auto", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", ToolChoice: "auto"}, false},
		{"valid toolChoice none", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", ToolChoice: "none"}, false},
		{"invalid toolChoice", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", UserMessage: "hi", ToolChoice: "always"}, true},
		{"valid tool role with toolResponse", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "tool", Parts: []Part{{ToolResponse: &ToolResult{Name: "get_weather", Ref: "a1", Output: json.RawMessage(`{"tempC":18}`)}}}}}}, false},
		{"valid model role with toolRequest", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "model", Parts: []Part{{ToolRequest: &ToolCall{Name: "get_weather", Ref: "a1", Input: json.RawMessage(`{"city":"SF"}`)}}}}}}, false},
		{"toolRequest missing name", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "model", Parts: []Part{{ToolRequest: &ToolCall{Ref: "a1"}}}}}}, true},
		{"toolResponse missing name", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "tool", Parts: []Part{{ToolResponse: &ToolResult{Ref: "a1"}}}}}}, true},
		{"part text and toolRequest", GenerateRequest{ModelName: "googleai/gemini-2.5-flash", Messages: []Message{{Role: "model", Parts: []Part{{Text: "hi", ToolRequest: &ToolCall{Name: "x"}}}}}}, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.req.Validate()
			if (err != nil) != testCase.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}
