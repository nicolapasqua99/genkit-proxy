package proxy

import (
	"context"
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
		ai.WithPrompt(req.UserMessage),
	}
	if req.SystemPrompt != "" {
		opts = append(opts, ai.WithSystem(req.SystemPrompt))
	}
	if cfg := configFor(req); cfg != nil {
		opts = append(opts, ai.WithConfig(cfg))
	}

	resp, err := genkit.Generate(ctx, genkitApp, opts...)
	if err != nil {
		return GenerateResponse{}, fmt.Errorf("generate %q: %w", req.ModelName, err)
	}
	return GenerateResponse{
		Model:        req.ModelName,
		Output:       resp.Text(),
		FinishReason: string(resp.FinishReason),
		Usage:        usageFrom(resp.Usage),
	}, nil
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
