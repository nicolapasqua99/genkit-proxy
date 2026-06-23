package proxy

import (
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/plugins/compat_oai/anthropic"
	"github.com/firebase/genkit/go/plugins/compat_oai/openai"
	"github.com/firebase/genkit/go/plugins/googlegenai"
	"github.com/openai/openai-go/option"
)

// Supported provider prefixes, matching Genkit's model-name namespacing.
const (
	providerGoogleAI  = "googleai"
	providerOpenAI    = "openai"
	providerAnthropic = "anthropic"
	providerVertexAI  = "vertexai"
)

// providerOf extracts and validates the provider prefix of a
// provider-namespaced model name such as "googleai/gemini-2.5-flash".
func providerOf(modelName string) (string, error) {
	provider, model, ok := strings.Cut(modelName, "/")
	if !ok || provider == "" {
		return "", fmt.Errorf("%w: %q is not provider-prefixed", ErrUnsupportedProvider, modelName)
	}
	switch provider {
	case providerGoogleAI, providerOpenAI, providerAnthropic, providerVertexAI:
		if strings.TrimSpace(model) == "" {
			return "", &ValidationError{Field: "modelName", Reason: "missing model after provider prefix"}
		}
		return provider, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedProvider, provider)
	}
}

// pluginFor builds the Genkit plugin for the model's provider, configured with
// the per-request apiKey. Genkit binds credentials at plugin construction, so a
// fresh, single-provider plugin is built for each request to keep tenant keys
// isolated.
func pluginFor(modelName, apiKey string) (api.Plugin, error) {
	provider, err := providerOf(modelName)
	if err != nil {
		return nil, err
	}
	switch provider {
	case providerGoogleAI:
		return &googlegenai.GoogleAI{APIKey: apiKey}, nil
	case providerOpenAI:
		return &openai.OpenAI{APIKey: apiKey}, nil
	case providerAnthropic:
		return &anthropic.Anthropic{Opts: []option.RequestOption{option.WithAPIKey(apiKey)}}, nil
	case providerVertexAI:
		// Vertex authenticates via Application Default Credentials; ProjectID and
		// Location come from GOOGLE_CLOUD_PROJECT / GOOGLE_CLOUD_LOCATION. The
		// per-request bearer is not used here.
		return &googlegenai.VertexAI{}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedProvider, provider)
	}
}
