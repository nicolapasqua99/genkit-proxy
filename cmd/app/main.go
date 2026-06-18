// Command app runs the Genkit-backed, model-agnostic AI proxy HTTP server.
//
// It exposes POST /v1/generate, which accepts a generic generation payload and
// forwards it to the LLM provider named by the request's model prefix, using
// the API key supplied in the Authorization bearer header. GET /healthz and
// GET /readyz are liveness and readiness probes. GET /version returns the build
// SHA and timestamp. GET /metrics serves Prometheus metrics. The server listens
// on $PORT (default 8080) for Cloud Run.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nicolapasqua99/genkit-proxy/internal/proxy"
)

// version and buildTime are overridden at link time via -ldflags -X.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	metrics, err := proxy.NewMetrics()
	if err != nil {
		slog.Error("metrics error", "err", err)
		os.Exit(1)
	}

	handler := proxy.NewHandler(proxy.NewGenkitGenerator(cfg.generateTimeout))

	mux := http.NewServeMux()
	mux.Handle("POST /v1/generate", handler)
	mux.Handle("GET /metrics", metrics.Handler())
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /version", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]string{
			"version":   version,
			"buildTime": buildTime,
		})
	})

	srv := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           proxy.Recover(proxy.RequestID(proxy.Logger(metrics.Middleware(mux)))),
		ReadHeaderTimeout: cfg.readHeaderTimeout,
		ReadTimeout:       cfg.readTimeout,
		WriteTimeout:      cfg.writeTimeout,
		IdleTimeout:       cfg.idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("genkit-proxy listening", "port", cfg.port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("shutdown error", "err", err)
	}
}
