package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// responseFormatJSON is the only supported structured-output format.
const responseFormatJSON = "json"

// Conversation roles accepted in GenerateRequest.Messages.
const (
	roleUser  = "user"
	roleModel = "model"
)

// Message is one prior turn in a multi-turn conversation.
type Message struct {
	// Role is the speaker: "user" or "model".
	Role string `json:"role"`
	// Content is the message text.
	Content string `json:"content"`
}

// GenerateRequest is the generic payload accepted by the proxy.
type GenerateRequest struct {
	// ModelName is the provider-prefixed model identifier, for example
	// "googleai/gemini-2.5-flash". The prefix selects the provider plugin.
	ModelName string `json:"modelName"`
	// SystemPrompt is the optional system instruction.
	SystemPrompt string `json:"systemPrompt,omitempty"`
	// UserMessage is the user prompt sent to the model.
	UserMessage string `json:"userMessage"`
	// Temperature optionally controls sampling randomness. When nil the
	// provider default is used.
	Temperature *float64 `json:"temperature,omitempty"`
	// MaxOutputTokens optionally caps the number of tokens generated. When nil
	// the provider default is used.
	MaxOutputTokens *int `json:"maxOutputTokens,omitempty"`
	// TopP optionally sets nucleus-sampling probability mass. When nil the
	// provider default is used.
	TopP *float64 `json:"topP,omitempty"`
	// TopK optionally limits sampling to the K most likely tokens. When nil the
	// provider default is used.
	TopK *int `json:"topK,omitempty"`
	// StopSequences optionally lists strings that halt generation when produced.
	StopSequences []string `json:"stopSequences,omitempty"`
	// ResponseFormat optionally requests structured output. The only supported
	// value is "json"; empty means plain text.
	ResponseFormat string `json:"responseFormat,omitempty"`
	// OutputSchema optionally constrains JSON output to a JSON Schema. Valid only
	// when ResponseFormat is "json".
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	// Messages optionally provides prior conversation turns. Each role must be
	// "user" or "model"; the current turn is the separate UserMessage.
	Messages []Message `json:"messages,omitempty"`
}

// GenerateResponse is the proxy's reply to a successful generation.
type GenerateResponse struct {
	// Model echoes the model that served the request.
	Model string `json:"model"`
	// Output is the generated text. Empty when the model returned no text
	// (e.g. a safety block); inspect FinishReason in that case.
	Output string `json:"output"`
	// FinishReason is the reason the model stopped generating. Common values:
	// "stop", "length", "blocked", "interrupted", "other", "unknown".
	// Omitted when the provider did not report a reason.
	FinishReason string `json:"finishReason,omitempty"`
	// Data carries the structured JSON output when JSON mode was requested and the
	// model returned valid JSON. Omitted for plain-text responses.
	Data json.RawMessage `json:"data,omitempty"`
	// Usage reports token consumption. Omitted when the provider reported none.
	Usage *Usage `json:"usage,omitempty"`
}

// Usage reports the token counts for a generation. Fields are omitted when the
// provider did not report them.
type Usage struct {
	// Input is the number of tokens in the prompt.
	Input int `json:"input,omitempty"`
	// Output is the number of tokens generated in the response.
	Output int `json:"output,omitempty"`
	// Total is the sum of input and output tokens.
	Total int `json:"total,omitempty"`
}

// Validate reports the first problem found with the request, or nil when the
// request is well formed.
func (request GenerateRequest) Validate() error {
	if strings.TrimSpace(request.ModelName) == "" {
		return &ValidationError{Field: "modelName", Reason: "must not be empty"}
	}
	if _, err := providerOf(request.ModelName); err != nil {
		return err
	}
	if strings.TrimSpace(request.UserMessage) == "" {
		return &ValidationError{Field: "userMessage", Reason: "must not be empty"}
	}
	if request.Temperature != nil && (*request.Temperature < 0 || *request.Temperature > 2) {
		return &ValidationError{Field: "temperature", Reason: "must be between 0 and 2"}
	}
	if request.MaxOutputTokens != nil && *request.MaxOutputTokens < 1 {
		return &ValidationError{Field: "maxOutputTokens", Reason: "must be at least 1"}
	}
	if request.TopP != nil && (*request.TopP < 0 || *request.TopP > 1) {
		return &ValidationError{Field: "topP", Reason: "must be between 0 and 1"}
	}
	if request.TopK != nil && *request.TopK < 1 {
		return &ValidationError{Field: "topK", Reason: "must be at least 1"}
	}
	if request.ResponseFormat != "" && request.ResponseFormat != responseFormatJSON {
		return &ValidationError{Field: "responseFormat", Reason: `must be "json" when set`}
	}
	if len(request.OutputSchema) > 0 && request.ResponseFormat != responseFormatJSON {
		return &ValidationError{Field: "outputSchema", Reason: `requires responseFormat "json"`}
	}
	for i, message := range request.Messages {
		if message.Role != roleUser && message.Role != roleModel {
			return &ValidationError{Field: fmt.Sprintf("messages[%d].role", i), Reason: `must be "user" or "model"`}
		}
		if strings.TrimSpace(message.Content) == "" {
			return &ValidationError{Field: fmt.Sprintf("messages[%d].content", i), Reason: "must not be empty"}
		}
	}
	return nil
}
