package proxy

import "strings"

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
}

// Validate reports the first problem found with the request, or nil when the
// request is well formed.
func (r GenerateRequest) Validate() error {
	if strings.TrimSpace(r.ModelName) == "" {
		return &ValidationError{Field: "modelName", Reason: "must not be empty"}
	}
	if _, err := providerOf(r.ModelName); err != nil {
		return err
	}
	if strings.TrimSpace(r.UserMessage) == "" {
		return &ValidationError{Field: "userMessage", Reason: "must not be empty"}
	}
	if r.Temperature != nil && (*r.Temperature < 0 || *r.Temperature > 2) {
		return &ValidationError{Field: "temperature", Reason: "must be between 0 and 2"}
	}
	return nil
}
