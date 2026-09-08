package main

import (
	"reflect"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	t.Run("defaults when no env vars set", func(t *testing.T) {
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.port != "8080" {
			t.Errorf("port: got %q, want %q", cfg.port, "8080")
		}
		if cfg.readHeaderTimeout != 10*time.Second {
			t.Errorf("readHeaderTimeout: got %v, want %v", cfg.readHeaderTimeout, 10*time.Second)
		}
		if cfg.readTimeout != 30*time.Second {
			t.Errorf("readTimeout: got %v, want %v", cfg.readTimeout, 30*time.Second)
		}
		if cfg.writeTimeout != 120*time.Second {
			t.Errorf("writeTimeout: got %v, want %v", cfg.writeTimeout, 120*time.Second)
		}
		if cfg.idleTimeout != 60*time.Second {
			t.Errorf("idleTimeout: got %v, want %v", cfg.idleTimeout, 60*time.Second)
		}
		if cfg.generateTimeout != 30*time.Second {
			t.Errorf("generateTimeout: got %v, want %v", cfg.generateTimeout, 30*time.Second)
		}
	})

	t.Run("PORT override", func(t *testing.T) {
		t.Setenv("PORT", "9090")
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.port != "9090" {
			t.Errorf("port: got %q, want %q", cfg.port, "9090")
		}
	})

	t.Run("valid duration overrides", func(t *testing.T) {
		t.Setenv("GENERATE_TIMEOUT", "45s")
		t.Setenv("WRITE_TIMEOUT", "2m")
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.generateTimeout != 45*time.Second {
			t.Errorf("generateTimeout: got %v, want %v", cfg.generateTimeout, 45*time.Second)
		}
		if cfg.writeTimeout != 2*time.Minute {
			t.Errorf("writeTimeout: got %v, want %v", cfg.writeTimeout, 2*time.Minute)
		}
	})

	t.Run("invalid duration returns error", func(t *testing.T) {
		t.Setenv("READ_TIMEOUT", "notaduration")
		_, err := loadConfig()
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("rate limit defaults", func(t *testing.T) {
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.rateLimitRPS != 60 {
			t.Errorf("rateLimitRPS: got %d, want 60", cfg.rateLimitRPS)
		}
		if cfg.rateLimitWindow != time.Minute {
			t.Errorf("rateLimitWindow: got %v, want 1m", cfg.rateLimitWindow)
		}
		if cfg.corsAllowOrigins != "*" {
			t.Errorf("corsAllowOrigins: got %q, want %q", cfg.corsAllowOrigins, "*")
		}
	})

	t.Run("retry defaults", func(t *testing.T) {
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.retryMaxAttempts != 3 {
			t.Errorf("retryMaxAttempts: got %d, want 3", cfg.retryMaxAttempts)
		}
		if cfg.retryBaseBackoff != 100*time.Millisecond {
			t.Errorf("retryBaseBackoff: got %v, want 100ms", cfg.retryBaseBackoff)
		}
	})

	t.Run("retry env vars parsed", func(t *testing.T) {
		t.Setenv("RETRY_MAX_ATTEMPTS", "5")
		t.Setenv("RETRY_BASE_BACKOFF", "200ms")
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.retryMaxAttempts != 5 {
			t.Errorf("retryMaxAttempts: got %d, want 5", cfg.retryMaxAttempts)
		}
		if cfg.retryBaseBackoff != 200*time.Millisecond {
			t.Errorf("retryBaseBackoff: got %v, want 200ms", cfg.retryBaseBackoff)
		}
	})

	t.Run("cache defaults", func(t *testing.T) {
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.cacheEnabled {
			t.Error("cacheEnabled: got false, want true")
		}
		if cfg.cacheTTL != 10*time.Minute {
			t.Errorf("cacheTTL: got %v, want 10m", cfg.cacheTTL)
		}
		if cfg.cacheMaxSize != 1024 {
			t.Errorf("cacheMaxSize: got %d, want 1024", cfg.cacheMaxSize)
		}
	})

	t.Run("cache env vars parsed", func(t *testing.T) {
		t.Setenv("GENKIT_CACHE_ENABLED", "false")
		t.Setenv("GENKIT_CACHE_TTL", "5m")
		t.Setenv("GENKIT_CACHE_MAX_SIZE", "32")
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.cacheEnabled {
			t.Error("cacheEnabled: got true, want false")
		}
		if cfg.cacheTTL != 5*time.Minute {
			t.Errorf("cacheTTL: got %v, want 5m", cfg.cacheTTL)
		}
		if cfg.cacheMaxSize != 32 {
			t.Errorf("cacheMaxSize: got %d, want 32", cfg.cacheMaxSize)
		}
	})

	t.Run("invalid GENKIT_CACHE_ENABLED returns error", func(t *testing.T) {
		t.Setenv("GENKIT_CACHE_ENABLED", "notabool")
		if _, err := loadConfig(); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("model allowlist default empty", func(t *testing.T) {
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.modelAllowlist != nil {
			t.Errorf("modelAllowlist: got %v, want nil", cfg.modelAllowlist)
		}
	})

	t.Run("MODEL_ALLOWLIST parsed", func(t *testing.T) {
		t.Setenv("MODEL_ALLOWLIST", "googleai/gemini-2.5-flash,openai")
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"googleai/gemini-2.5-flash", "openai"}
		if !reflect.DeepEqual(cfg.modelAllowlist, want) {
			t.Errorf("modelAllowlist = %v, want %v", cfg.modelAllowlist, want)
		}
	})

	t.Run("RATE_LIMIT_MODELS parsed", func(t *testing.T) {
		t.Setenv("RATE_LIMIT_MODELS", "googleai/gemini-2.5-flash:50,openai/gpt-4o:10")
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := map[string]int{
			"googleai/gemini-2.5-flash": 50,
			"openai/gpt-4o":             10,
		}
		if !reflect.DeepEqual(cfg.rateLimitByModel, want) {
			t.Errorf("rateLimitByModel = %v, want %v", cfg.rateLimitByModel, want)
		}
	})

	t.Run("gateway auth defaults to disabled", func(t *testing.T) {
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.gatewayAuthEnabled {
			t.Error("gatewayAuthEnabled: got true, want false")
		}
	})

	t.Run("gateway auth overrides", func(t *testing.T) {
		t.Setenv("GATEWAY_AUTH_ENABLED", "true")
		t.Setenv("GATEWAY_AUTH_TENANTS", `{"abc":{"tenant":"acme","providers":{"openai":"ref"}}}`)
		t.Setenv("GATEWAY_SECRETS", "ref=sk-xx")
		cfg, err := loadConfig()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.gatewayAuthEnabled {
			t.Error("gatewayAuthEnabled: got false, want true")
		}
		if cfg.gatewaySecrets != "ref=sk-xx" {
			t.Errorf("gatewaySecrets = %q, want %q", cfg.gatewaySecrets, "ref=sk-xx")
		}
		if cfg.gatewayAuthTenants == "" {
			t.Error("gatewayAuthTenants: got empty, want JSON")
		}
	})

	t.Run("invalid GATEWAY_AUTH_ENABLED returns error", func(t *testing.T) {
		t.Setenv("GATEWAY_AUTH_ENABLED", "notabool")
		if _, err := loadConfig(); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestParseModelLimits(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    map[string]int
		wantErr bool
	}{
		{
			name:  "empty string returns nil",
			input: "",
			want:  nil,
		},
		{
			name:  "single entry",
			input: "googleai/gemini-2.5-flash:50",
			want:  map[string]int{"googleai/gemini-2.5-flash": 50},
		},
		{
			name:  "multiple entries",
			input: "googleai/gemini-2.5-flash:50,openai/gpt-4o:10",
			want:  map[string]int{"googleai/gemini-2.5-flash": 50, "openai/gpt-4o": 10},
		},
		{
			name:  "whitespace trimmed",
			input: " googleai/gemini-2.5-flash:50 , openai/gpt-4o:10 ",
			want:  map[string]int{"googleai/gemini-2.5-flash": 50, "openai/gpt-4o": 10},
		},
		{
			name:    "missing colon",
			input:   "googleai/gemini-2.5-flash",
			wantErr: true,
		},
		{
			name:    "non-integer limit",
			input:   "googleai/gemini-2.5-flash:abc",
			wantErr: true,
		},
		{
			name:    "zero limit",
			input:   "googleai/gemini-2.5-flash:0",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseModelLimits(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("parseModelLimits(%q): expected error, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseModelLimits(%q): unexpected error: %v", tc.input, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseModelLimits(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
