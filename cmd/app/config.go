package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nicolapasqua99/genkit-proxy/internal/proxy"
)

// config holds the server and generator configuration loaded from the environment.
type config struct {
	port              string
	readHeaderTimeout time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	generateTimeout   time.Duration

	// Redis backend for the rate limiter.
	redisURL           string
	redisClusterAddrs  string // comma-separated
	redisSentinelAddrs string // comma-separated
	redisMasterName    string

	// Rate limiting
	rateLimitRPS          int
	rateLimitWindow       time.Duration
	rateLimitStreamRPS    int
	rateLimitGoogleAIRPS  int
	rateLimitOpenAIRPS    int
	rateLimitAnthropicRPS int
	rateLimitVertexAIRPS  int
	rateLimitByModel      map[string]int // from RATE_LIMIT_MODELS=model:rps,...

	// CORS
	corsAllowOrigins string

	// Retry
	retryMaxAttempts int           // RETRY_MAX_ATTEMPTS, default 3
	retryBaseBackoff time.Duration // RETRY_BASE_BACKOFF, default 100ms

	// Genkit instance cache
	cacheEnabled bool          // GENKIT_CACHE_ENABLED, default true
	cacheTTL     time.Duration // GENKIT_CACHE_TTL, default 10m
	cacheMaxSize int           // GENKIT_CACHE_MAX_SIZE, default 1024

	// Model allowlist
	modelAllowlist []string // from MODEL_ALLOWLIST=model-or-provider,...; empty allows all
}

// loadConfig reads server and generator settings from the environment. Missing
// variables fall back to sensible defaults; present-but-invalid values return an
// error.
func loadConfig() (config, error) {
	cfg := config{
		port:              "8080",
		readHeaderTimeout: 10 * time.Second,
		readTimeout:       30 * time.Second,
		writeTimeout:      120 * time.Second,
		idleTimeout:       60 * time.Second,
		generateTimeout:   30 * time.Second,
		redisMasterName:   "mymaster",
		rateLimitRPS:      60,
		rateLimitWindow:   time.Minute,
		corsAllowOrigins:  "*",
		retryMaxAttempts:  3,
		retryBaseBackoff:  100 * time.Millisecond,
		cacheEnabled:      true,
		cacheTTL:          10 * time.Minute,
		cacheMaxSize:      1024,
	}

	if v := os.Getenv("PORT"); v != "" {
		cfg.port = v
	}

	durations := []struct {
		env  string
		dest *time.Duration
	}{
		{"READ_HEADER_TIMEOUT", &cfg.readHeaderTimeout},
		{"READ_TIMEOUT", &cfg.readTimeout},
		{"WRITE_TIMEOUT", &cfg.writeTimeout},
		{"IDLE_TIMEOUT", &cfg.idleTimeout},
		{"GENERATE_TIMEOUT", &cfg.generateTimeout},
		{"RATE_LIMIT_WINDOW", &cfg.rateLimitWindow},
		{"RETRY_BASE_BACKOFF", &cfg.retryBaseBackoff},
		{"GENKIT_CACHE_TTL", &cfg.cacheTTL},
	}
	for _, d := range durations {
		v := os.Getenv(d.env)
		if v == "" {
			continue
		}
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("%s: %w", d.env, err)
		}
		*d.dest = parsed
	}

	integers := []struct {
		env  string
		dest *int
	}{
		{"RATE_LIMIT_RPS", &cfg.rateLimitRPS},
		{"RATE_LIMIT_STREAM_RPS", &cfg.rateLimitStreamRPS},
		{"RATE_LIMIT_GOOGLEAI_RPS", &cfg.rateLimitGoogleAIRPS},
		{"RATE_LIMIT_OPENAI_RPS", &cfg.rateLimitOpenAIRPS},
		{"RATE_LIMIT_ANTHROPIC_RPS", &cfg.rateLimitAnthropicRPS},
		{"RATE_LIMIT_VERTEXAI_RPS", &cfg.rateLimitVertexAIRPS},
		{"RETRY_MAX_ATTEMPTS", &cfg.retryMaxAttempts},
		{"GENKIT_CACHE_MAX_SIZE", &cfg.cacheMaxSize},
	}
	for _, i := range integers {
		v := os.Getenv(i.env)
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return config{}, fmt.Errorf("%s: must be a non-negative integer", i.env)
		}
		*i.dest = n
	}

	strings_ := []struct {
		env  string
		dest *string
	}{
		{"REDIS_URL", &cfg.redisURL},
		{"REDIS_CLUSTER_ADDRS", &cfg.redisClusterAddrs},
		{"REDIS_SENTINEL_ADDRS", &cfg.redisSentinelAddrs},
		{"REDIS_MASTER_NAME", &cfg.redisMasterName},
		{"CORS_ALLOW_ORIGINS", &cfg.corsAllowOrigins},
	}
	for _, s := range strings_ {
		if v := os.Getenv(s.env); v != "" {
			*s.dest = v
		}
	}

	if raw := os.Getenv("RATE_LIMIT_MODELS"); raw != "" {
		byModel, err := parseModelLimits(raw)
		if err != nil {
			return config{}, err
		}
		cfg.rateLimitByModel = byModel
	}

	if v := os.Getenv("GENKIT_CACHE_ENABLED"); v != "" {
		enabled, err := strconv.ParseBool(v)
		if err != nil {
			return config{}, fmt.Errorf("GENKIT_CACHE_ENABLED: %w", err)
		}
		cfg.cacheEnabled = enabled
	}

	if raw := os.Getenv("MODEL_ALLOWLIST"); raw != "" {
		cfg.modelAllowlist = strings.Split(raw, ",")
	}

	return cfg, nil
}

// parseModelLimits parses RATE_LIMIT_MODELS=model:rps,... into a map.
// The format uses the last colon as the separator so model names containing
// slashes (e.g. googleai/gemini-2.5-flash) are handled correctly.
func parseModelLimits(raw string) (map[string]int, error) {
	if raw == "" {
		return nil, nil
	}
	result := make(map[string]int)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		idx := strings.LastIndex(entry, ":")
		if idx < 0 {
			return nil, fmt.Errorf("RATE_LIMIT_MODELS entry %q: missing limit after ':'", entry)
		}
		model := entry[:idx]
		n, err := strconv.Atoi(entry[idx+1:])
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("RATE_LIMIT_MODELS entry %q: limit must be a positive integer", entry)
		}
		result[model] = n
	}
	return result, nil
}

// limitFor returns the per-provider RPS limit, or 0 if none is configured.
func (c config) limitFor(provider string) int {
	switch strings.ToLower(provider) {
	case "googleai":
		return c.rateLimitGoogleAIRPS
	case "openai":
		return c.rateLimitOpenAIRPS
	case "anthropic":
		return c.rateLimitAnthropicRPS
	case "vertexai":
		return c.rateLimitVertexAIRPS
	default:
		return 0
	}
}

// handlerRateLimitConfig builds a proxy.HandlerRLConfig from the loaded config.
// Most-specific limit wins: exact model match beats provider prefix match.
func (c config) handlerRateLimitConfig() proxy.HandlerRLConfig {
	return proxy.HandlerRLConfig{
		StreamLimit: c.rateLimitStreamRPS,
		LimitForModel: func(model string) (int, string) {
			if limit, ok := c.rateLimitByModel[model]; ok {
				return limit, "model:" + model
			}
			provider, _, _ := strings.Cut(model, "/")
			if limit := c.limitFor(provider); limit > 0 {
				return limit, "provider:" + provider
			}
			return 0, ""
		},
	}
}
