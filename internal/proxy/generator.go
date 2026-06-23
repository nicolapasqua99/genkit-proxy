package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// Generator produces a completion for a validated request using the supplied
// per-request API key.
type Generator interface {
	Generate(ctx context.Context, req GenerateRequest, apiKey string) (GenerateResponse, error)
}

// GenkitGenerator is the Genkit-backed Generator. It routes each request to the
// provider named by the model prefix, initialising a Genkit instance with the
// caller's API key so credentials are never shared between tenants.
type GenkitGenerator struct {
	// GenerateTimeout caps each upstream call. Zero means no additional timeout
	// beyond the one already carried by the incoming context.
	GenerateTimeout time.Duration
}

// NewGenkitGenerator returns a GenkitGenerator that applies timeout to each
// upstream Generate call. Pass zero to rely solely on the request context.
func NewGenkitGenerator(timeout time.Duration) GenkitGenerator {
	return GenkitGenerator{GenerateTimeout: timeout}
}

// Generate implements Generator using Genkit's unified Generate API.
func (g GenkitGenerator) Generate(ctx context.Context, req GenerateRequest, apiKey string) (GenerateResponse, error) {
	plugin, err := pluginFor(req.ModelName, apiKey)
	if err != nil {
		return GenerateResponse{}, err
	}

	if g.GenerateTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, g.GenerateTimeout)
		defer cancel()
	}

	genkitApp := genkit.Init(ctx, genkit.WithPlugins(plugin))

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

	resp, err := genkit.Generate(ctx, genkitApp, opts...)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("generate %q: %w", req.ModelName, err)
	}
	out := GenerateResponse{
		Model:        req.ModelName,
		FinishReason: string(resp.FinishReason),
		Usage:        usageFrom(resp.Usage),
	}
	out.Output, out.Data = outputAndData(req, resp.Text())
	return out, nil
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
// returning nil when there is none. Validation guarantees each role is "user"
// or "model", so ai.Role conversion is safe.
func messagesFrom(req GenerateRequest) []*ai.Message {
	if len(req.Messages) == 0 {
		return nil
	}
	msgs := make([]*ai.Message, len(req.Messages))
	for i, message := range req.Messages {
		if len(message.Parts) > 0 {
			parts := make([]*ai.Part, len(message.Parts))
			for j, part := range message.Parts {
				if part.Media != nil {
					parts[j] = ai.NewMediaPart(part.Media.ContentType, part.Media.URL)
				} else {
					parts[j] = ai.NewTextPart(part.Text)
				}
			}
			msgs[i] = ai.NewMessage(ai.Role(message.Role), nil, parts...)
			continue
		}
		msgs[i] = ai.NewTextMessage(ai.Role(message.Role), message.Content)
	}
	return msgs
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
