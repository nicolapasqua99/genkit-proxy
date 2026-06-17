package main

import (
	"fmt"
	"os"
	"time"
)

// config holds the server and generator configuration loaded from the environment.
type config struct {
	port              string
	readHeaderTimeout time.Duration
	readTimeout       time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	generateTimeout   time.Duration
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

	return cfg, nil
}
