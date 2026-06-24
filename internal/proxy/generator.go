package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// Generator produces a completion for a validated request using the supplied
// per-request API key.
type Generator interface {
	Generate(ctx context.Context, req GenerateRequest, apiKey string) (GenerateResponse, error)
	// GenerateStream streams the completion, invoking onChunk for each text
	// delta, and returns the final response (model, finish reason, usage). The
	// streamed text is delivered only through onChunk, not in the returned
	// response's Output.
	GenerateStream(ctx context.Context, req GenerateRequest, apiKey string, onChunk func(delta string) error) (GenerateResponse, error)
}

// GenkitGenerator is the Genkit-backed Generator. It routes each request to the
// provider named by the model prefix, initialising a Genkit instance with the
// caller's API key so credentials are never shared between tenants.
type GenkitGenerator struct {
	// GenerateTimeout caps each upstream call. Zero means no additional timeout
	// beyond the one already carried by the incoming context.
	GenerateTimeout time.Duration
	// run and runStream are swapped out in tests. Both default to the real
	// genkit implementations set by NewGenkitGenerator.
	run       func(ctx context.Context, req GenerateRequest, apiKey string) (*ai.ModelResponse, error)
	runStream func(ctx context.Context, req GenerateRequest, apiKey string, onChunk func(delta string) error) (*ai.ModelResponse, error)
}

// NewGenkitGenerator returns a GenkitGenerator that applies timeout to each
// upstream Generate call. Pass zero to rely solely on the request context.
func NewGenkitGenerator(timeout time.Duration) GenkitGenerator {
	return GenkitGenerator{
		GenerateTimeout: timeout,
		run:             genkitRun,
		runStream:       genkitRunStream,
	}
}

// Generate implements Generator using Genkit's unified Generate API.
func (g GenkitGenerator) Generate(ctx context.Context, req GenerateRequest, apiKey string) (GenerateResponse, error) {
	if g.GenerateTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.GenerateTimeout)
		defer cancel()
	}

	resp, err := g.run(ctx, req, apiKey)
	if err != nil {
		if errors.Is(err, ErrUnsupportedProvider) {
			return GenerateResponse{}, err
		}
		return GenerateResponse{}, fmt.Errorf("generate %q: %w", req.ModelName, err)
	}
	out := GenerateResponse{
		Model:        req.ModelName,
		FinishReason: string(resp.FinishReason),
		ToolCalls:    toolCallsFrom(resp.ToolRequests()),
		Usage:        usageFrom(resp.Usage),
	}
	out.Output, out.Data = outputAndData(req, resp.Text())
	return out, nil
}

// GenerateStream implements Generator using Genkit's streaming Generate API.
func (g GenkitGenerator) GenerateStream(ctx context.Context, req GenerateRequest, apiKey string, onChunk func(delta string) error) (GenerateResponse, error) {
	if g.GenerateTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.GenerateTimeout)
		defer cancel()
	}

	final, err := g.runStream(ctx, req, apiKey, onChunk)
	if err != nil {
		if errors.Is(err, ErrUnsupportedProvider) {
			return GenerateResponse{}, err
		}
		return GenerateResponse{}, fmt.Errorf("generate %q: %w", req.ModelName, err)
	}

	out := GenerateResponse{Model: req.ModelName}
	if final != nil {
		out.FinishReason = string(final.FinishReason)
		out.ToolCalls = toolCallsFrom(final.ToolRequests())
		out.Usage = usageFrom(final.Usage)
	}
	return out, nil
}

// genkitRun is the real implementation of GenkitGenerator.run. It selects the
// provider plugin, initialises a per-request Genkit instance, assembles options,
// and calls genkit.Generate.
func genkitRun(ctx context.Context, req GenerateRequest, apiKey string) (*ai.ModelResponse, error) {
	plugin, err := pluginFor(req.ModelName, apiKey)
	if err != nil {
		return nil, err
	}
	genkitApp := genkit.Init(ctx, genkit.WithPlugins(plugin))
	opts := append(generateOptions(req), toolOptions(genkitApp, req)...)
	return genkit.Generate(ctx, genkitApp, opts...)
}

// genkitRunStream is the real implementation of GenkitGenerator.runStream. It
// sets up the same per-request Genkit instance as genkitRun, then iterates the
// stream, forwarding text deltas to onChunk and returning the final response.
func genkitRunStream(ctx context.Context, req GenerateRequest, apiKey string, onChunk func(delta string) error) (*ai.ModelResponse, error) {
	plugin, err := pluginFor(req.ModelName, apiKey)
	if err != nil {
		return nil, err
	}
	genkitApp := genkit.Init(ctx, genkit.WithPlugins(plugin))
	opts := append(generateOptions(req), toolOptions(genkitApp, req)...)
	var final *ai.ModelResponse
	for value, streamErr := range genkit.GenerateStream(ctx, genkitApp, opts...) {
		if streamErr != nil {
			return nil, streamErr
		}
		if value.Done {
			final = value.Response
			break
		}
		if delta := value.Chunk.Text(); delta != "" {
			if err := onChunk(delta); err != nil {
				return nil, err
			}
		}
	}
	return final, nil
}

// generateOptions builds the Genkit options shared by Generate and
// GenerateStream from a validated request.
func generateOptions(req GenerateRequest) []ai.GenerateOption {
	opts := []ai.GenerateOption{
		ai.WithModelName(req.ModelName),
	}
	if req.UserMessage != "" {
		opts = append(opts, ai.WithPrompt(req.UserMessage))
	}
	if req.SystemPrompt != "" {
		opts = append(opts, ai.WithSystem(req.SystemPrompt))
	}
	if msgs := messagesFrom(req); msgs != nil {
		opts = append(opts, ai.WithMessages(msgs...))
	}
	if cfg := configFor(req); cfg != nil {
		opts = append(opts, ai.WithConfig(cfg))
	}
	if req.ResponseFormat == responseFormatJSON {
		opts = append(opts, ai.WithOutputFormat(ai.OutputFormatJSON))
		if len(req.OutputSchema) > 0 {
			opts = append(opts, ai.WithOutputSchema(req.OutputSchema))
		}
	}
	return opts
}

// toolOptions registers a stub tool per client-declared tool and returns the
// Genkit options that forward the declarations and return the model's tool calls
// instead of executing them. The stub functions are never invoked, because
// WithReturnToolRequests short-circuits before any tool runs. Registration is
// local to this per-request Genkit instance, so there is no cross-request state.
func toolOptions(genkitApp *genkit.Genkit, req GenerateRequest) []ai.GenerateOption {
	if len(req.Tools) == 0 {
		return nil
	}
	refs := make([]ai.ToolRef, len(req.Tools))
	for i, tool := range req.Tools {
		schema := tool.InputSchema
		if len(schema) == 0 {
			schema = map[string]any{"type": "object"}
		}
		refs[i] = genkit.DefineTool(genkitApp, tool.Name, tool.Description,
			func(_ *ai.ToolContext, _ any) (any, error) {
				return nil, errors.New("tool execution is delegated to the client")
			},
			ai.WithInputSchema(schema))
	}
	opts := []ai.GenerateOption{
		ai.WithTools(refs...),
		ai.WithReturnToolRequests(true),
	}
	if req.ToolChoice != "" {
		opts = append(opts, ai.WithToolChoice(ai.ToolChoice(req.ToolChoice)))
	}
	return opts
}

// toolCallsFrom maps Genkit tool requests to the proxy's ToolCall list,
// returning nil when the model requested none.
func toolCallsFrom(reqs []*ai.ToolRequest) []ToolCall {
	if len(reqs) == 0 {
		return nil
	}
	calls := make([]ToolCall, len(reqs))
	for i, toolReq := range reqs {
		call := ToolCall{Name: toolReq.Name, Ref: toolReq.Ref}
		if toolReq.Input != nil {
			if input, err := json.Marshal(toolReq.Input); err == nil {
				call.Input = input
			}
		}
		calls[i] = call
	}
	return calls
}

// outputAndData places the model's text into the response as structured Data
// when JSON was requested and the text is valid JSON; otherwise as plain Output.
// The fallback guarantees we never emit a malformed data field.
func outputAndData(req GenerateRequest, text string) (string, json.RawMessage) {
	if req.ResponseFormat == responseFormatJSON && json.Valid([]byte(text)) {
		return "", json.RawMessage(text)
	}
	return text, nil
}

// messagesFrom maps the request's conversation history to Genkit messages,
// returning nil when there is none. Validation guarantees each role is "user",
// "model", or "tool", so ai.Role conversion is safe.
func messagesFrom(req GenerateRequest) []*ai.Message {
	if len(req.Messages) == 0 {
		return nil
	}
	msgs := make([]*ai.Message, len(req.Messages))
	for i, message := range req.Messages {
		if len(message.Parts) > 0 {
			parts := make([]*ai.Part, len(message.Parts))
			for j, part := range message.Parts {
				parts[j] = partFrom(part)
			}
			msgs[i] = ai.NewMessage(ai.Role(message.Role), nil, parts...)
			continue
		}
		msgs[i] = ai.NewTextMessage(ai.Role(message.Role), message.Content)
	}
	return msgs
}

// partFrom maps one request part to a Genkit part. Validation guarantees exactly
// one field is set.
func partFrom(part Part) *ai.Part {
	switch {
	case part.Media != nil:
		return ai.NewMediaPart(part.Media.ContentType, part.Media.URL)
	case part.ToolRequest != nil:
		return ai.NewToolRequestPart(&ai.ToolRequest{
			Name:  part.ToolRequest.Name,
			Ref:   part.ToolRequest.Ref,
			Input: rawToAny(part.ToolRequest.Input),
		})
	case part.ToolResponse != nil:
		return ai.NewToolResponsePart(&ai.ToolResponse{
			Name:   part.ToolResponse.Name,
			Ref:    part.ToolResponse.Ref,
			Output: rawToAny(part.ToolResponse.Output),
		})
	default:
		return ai.NewTextPart(part.Text)
	}
}

// rawToAny decodes raw JSON into a generic value for Genkit's any-typed tool
// input/output fields, returning nil for empty or invalid JSON.
func rawToAny(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

// configFor builds the generation config from the request's optional tuning
// fields, returning nil when none are set so the provider defaults apply.
func configFor(req GenerateRequest) *ai.GenerationCommonConfig {
	cfg := &ai.GenerationCommonConfig{}
	set := false
	if req.Temperature != nil {
		cfg.Temperature = *req.Temperature
		set = true
	}
	if req.MaxOutputTokens != nil {
		cfg.MaxOutputTokens = *req.MaxOutputTokens
		set = true
	}
	if req.TopP != nil {
		cfg.TopP = *req.TopP
		set = true
	}
	if req.TopK != nil {
		cfg.TopK = *req.TopK
		set = true
	}
	if len(req.StopSequences) > 0 {
		cfg.StopSequences = req.StopSequences
		set = true
	}
	if !set {
		return nil
	}
	return cfg
}

// usageFrom maps Genkit's generation usage to the proxy's Usage, returning nil
// when the provider reported no usage.
func usageFrom(u *ai.GenerationUsage) *Usage {
	if u == nil {
		return nil
	}
	return &Usage{Input: u.InputTokens, Output: u.OutputTokens, Total: u.TotalTokens}
}
