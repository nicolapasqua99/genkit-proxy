package proxy

import "testing"

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
