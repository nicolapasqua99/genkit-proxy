package main

import (
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
}
