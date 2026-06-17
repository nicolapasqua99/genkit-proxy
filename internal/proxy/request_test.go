package proxy

import "testing"

func temp(f float64) *float64 { return &f }

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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
