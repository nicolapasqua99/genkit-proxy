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
	roleTool  = "tool"
)

// Tool-choice modes accepted in GenerateRequest.ToolChoice.
const (
	toolChoiceAuto     = "auto"
	toolChoiceRequired = "required"
	toolChoiceNone     = "none"
)

// Message is one turn in a conversation. Provide exactly one of Content (text
// shorthand) or Parts (multimodal: text and/or media).
type Message struct {
	// Role is the speaker: "user" or "model".
	Role string `json:"role"`
	// Content is the message text. Use this or Parts, not both.
	Content string `json:"content,omitempty"`
	// Parts is the multimodal content. Use this or Content, not both.
	Parts []Part `json:"parts,omitempty"`
}

// Part is one piece of a message's content: exactly one of Text, Media,
// ToolRequest, or ToolResponse. ToolRequest echoes a model tool call back in a
// "model" turn; ToolResponse carries a tool result in a "tool" turn.
type Part struct {
	// Text is a plain-text part.
	Text string `json:"text,omitempty"`
	// Media is a non-text part (image, document, ...).
	Media *Media `json:"media,omitempty"`
	// ToolRequest is a tool call previously emitted by the model, replayed so the
	// model has context for the matching ToolResponse.
	ToolRequest *ToolCall `json:"toolRequest,omitempty"`
	// ToolResponse is the result of running a tool the model requested.
	ToolResponse *ToolResult `json:"toolResponse,omitempty"`
}

// ToolDefinition declares a tool the model may call. The proxy forwards the
// declaration to the provider and returns any resulting calls to the client; it
// never executes tools itself.
type ToolDefinition struct {
	// Name is the tool's unique name.
	Name string `json:"name"`
	// Description tells the model what the tool does. Optional but recommended.
	Description string `json:"description,omitempty"`
	// InputSchema is the JSON Schema for the tool's arguments. When omitted, an
	// open object schema is used.
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// ToolCall is a request from the model to run a tool. It is returned in
// GenerateResponse.ToolCalls and replayed by the client in a ToolRequest part.
type ToolCall struct {
	// Name is the tool to call.
	Name string `json:"name"`
	// Ref correlates the call with its later ToolResult. Provider-assigned.
	Ref string `json:"ref,omitempty"`
	// Input is the JSON arguments the model chose for the call.
	Input json.RawMessage `json:"input,omitempty"`
}

// ToolResult is the outcome of a tool the client ran, sent back to the model in
// a "tool" turn.
type ToolResult struct {
	// Name is the tool that produced the result.
	Name string `json:"name"`
	// Ref matches the originating ToolCall.Ref.
	Ref string `json:"ref,omitempty"`
	// Output is the JSON result of the tool.
	Output json.RawMessage `json:"output,omitempty"`
}

// Media is a non-text part: an image or document referenced by an https:// URL
// or a "data:" URL with embedded base64 data.
type Media struct {
	// ContentType is the media MIME type, e.g. "image/png".
	ContentType string `json:"contentType"`
	// URL is an https:// or "data:" URL locating the media.
	URL string `json:"url"`
}

// GenerateRequest is the generic payload accepted by the proxy.
type GenerateRequest struct {
	// ModelName is the provider-prefixed model identifier, for example
	// "googleai/gemini-2.5-flash". The prefix selects the provider plugin.
	ModelName string `json:"modelName"`
	// SystemPrompt is the optional system instruction.
	SystemPrompt string `json:"systemPrompt,omitempty"`
	// UserMessage is the user prompt sent to the model. Required unless Messages
	// is provided.
	UserMessage string `json:"userMessage,omitempty"`
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
	// "user", "model", or "tool"; the current turn is the separate UserMessage.
	Messages []Message `json:"messages,omitempty"`
	// Tools optionally declares tools the model may call. When set, the model's
	// tool calls are returned in GenerateResponse.ToolCalls instead of executed.
	Tools []ToolDefinition `json:"tools,omitempty"`
	// ToolChoice optionally constrains tool use: "auto", "required", or "none".
	ToolChoice string `json:"toolChoice,omitempty"`
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
	// ToolCalls lists the tools the model wants the client to run. Present only
	// when tools were provided and the model chose to call one; Output is then
	// empty. Run the tools and send the results back in a follow-up request.
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
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
	if strings.TrimSpace(request.UserMessage) == "" && len(request.Messages) == 0 {
		return &ValidationError{Field: "userMessage", Reason: "must not be empty when messages is omitted"}
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
	switch request.ToolChoice {
	case "", toolChoiceAuto, toolChoiceRequired, toolChoiceNone:
	default:
		return &ValidationError{Field: "toolChoice", Reason: `must be "auto", "required", or "none" when set`}
	}
	seenTools := make(map[string]bool, len(request.Tools))
	for i, tool := range request.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return &ValidationError{Field: fmt.Sprintf("tools[%d].name", i), Reason: "must not be empty"}
		}
		if seenTools[tool.Name] {
			return &ValidationError{Field: fmt.Sprintf("tools[%d].name", i), Reason: "must be unique"}
		}
		seenTools[tool.Name] = true
	}
	for i, message := range request.Messages {
		if message.Role != roleUser && message.Role != roleModel && message.Role != roleTool {
			return &ValidationError{Field: fmt.Sprintf("messages[%d].role", i), Reason: `must be "user", "model", or "tool"`}
		}
		hasContent := strings.TrimSpace(message.Content) != ""
		hasParts := len(message.Parts) > 0
		if hasContent == hasParts {
			return &ValidationError{Field: fmt.Sprintf("messages[%d]", i), Reason: "must set exactly one of content or parts"}
		}
		for j, part := range message.Parts {
			if err := part.validate(i, j); err != nil {
				return err
			}
		}
	}
	return nil
}

// validate reports the first problem with a part within messages[i].parts[j].
func (part Part) validate(i, j int) error {
	set := 0
	if strings.TrimSpace(part.Text) != "" {
		set++
	}
	if part.Media != nil {
		set++
	}
	if part.ToolRequest != nil {
		set++
	}
	if part.ToolResponse != nil {
		set++
	}
	if set != 1 {
		return &ValidationError{Field: fmt.Sprintf("messages[%d].parts[%d]", i, j), Reason: "must set exactly one of text, media, toolRequest, or toolResponse"}
	}
	switch {
	case part.Media != nil:
		if strings.TrimSpace(part.Media.ContentType) == "" {
			return &ValidationError{Field: fmt.Sprintf("messages[%d].parts[%d].media.contentType", i, j), Reason: "must not be empty"}
		}
		if strings.TrimSpace(part.Media.URL) == "" {
			return &ValidationError{Field: fmt.Sprintf("messages[%d].parts[%d].media.url", i, j), Reason: "must not be empty"}
		}
	case part.ToolRequest != nil:
		if strings.TrimSpace(part.ToolRequest.Name) == "" {
			return &ValidationError{Field: fmt.Sprintf("messages[%d].parts[%d].toolRequest.name", i, j), Reason: "must not be empty"}
		}
	case part.ToolResponse != nil:
		if strings.TrimSpace(part.ToolResponse.Name) == "" {
			return &ValidationError{Field: fmt.Sprintf("messages[%d].parts[%d].toolResponse.name", i, j), Reason: "must not be empty"}
		}
	}
	return nil
}
