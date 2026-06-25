// Command app runs the Genkit-backed, model-agnostic AI proxy HTTP server.
//
// It exposes POST /v1/generate, which accepts a generic generation payload and
// forwards it to the LLM provider named by the request's model prefix, using
// the API key supplied in the Authorization bearer header. POST
// /v1/generate/stream streams the same generation as Server-Sent Events. GET
// /healthz and
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
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nicolapasqua99/genkit-proxy/internal/auth"
	"github.com/nicolapasqua99/genkit-proxy/internal/proxy"
	"github.com/nicolapasqua99/genkit-proxy/internal/ratelimit"
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

	var redisClient redis.UniversalClient
	switch {
	case cfg.redisClusterAddrs != "":
		redisClient = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs: strings.Split(cfg.redisClusterAddrs, ","),
		})
	case cfg.redisSentinelAddrs != "" && cfg.redisMasterName != "":
		redisClient = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    cfg.redisMasterName,
			SentinelAddrs: strings.Split(cfg.redisSentinelAddrs, ","),
		})
	case cfg.redisURL != "":
		opt, parseErr := redis.ParseURL(cfg.redisURL)
		if parseErr != nil {
			slog.Error("REDIS_URL parse error", "err", parseErr)
			os.Exit(1)
		}
		redisClient = redis.NewClient(opt)
	}

	var lim ratelimit.Limiter
	if redisClient != nil {
		lim = ratelimit.NewRedisLimiter(redisClient, cfg.rateLimitWindow)
	} else {
		lim = ratelimit.NewMemoryLimiter(cfg.rateLimitWindow)
	}
	defer lim.Close() //nolint:errcheck

	var cache *proxy.GenkitCache
	if cfg.cacheEnabled {
		cache = proxy.NewGenkitCache(cfg.cacheTTL, cfg.cacheMaxSize)
		defer cache.Close()
	}

	generator := proxy.NewRetryingGenerator(
		proxy.NewGenkitGenerator(cfg.generateTimeout, cache),
		cfg.retryMaxAttempts,
		cfg.retryBaseBackoff,
	)
	if cfg.gatewayAuthEnabled {
		resolver, err := buildCredentialResolver(cfg)
		if err != nil {
			slog.Error("gateway auth config error", "err", err)
			os.Exit(1)
		}
		generator = proxy.NewResolvingGenerator(generator, resolver)
	}

	handler := proxy.NewHandler(
		generator,
		lim,
		cfg.handlerRateLimitConfig(),
		proxy.NewModelAllowlist(cfg.modelAllowlist),
	)

	mux := http.NewServeMux()
	mux.Handle("POST /v1/generate", handler)
	mux.HandleFunc("POST /v1/generate/stream", handler.ServeStream)
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

	chain := proxy.CORS(cfg.corsAllowOrigins)(
		proxy.Recover(
			proxy.RequestID(
				proxy.RateLimit(lim, cfg.rateLimitRPS)(
					proxy.Logger(
						metrics.Middleware(mux),
					),
				),
			),
		),
	)

	srv := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           chain,
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

// buildCredentialResolver constructs the decoupled gateway-auth resolver from
// the static secret source and tenant table in cfg. It is built only when
// GATEWAY_AUTH_ENABLED is set, and any malformed configuration fails startup.
func buildCredentialResolver(cfg config) (proxy.CredentialResolver, error) {
	source, err := auth.ParseStaticSecrets(cfg.gatewaySecrets)
	if err != nil {
		return nil, err
	}
	resolver, err := auth.NewResolver(cfg.gatewayAuthTenants, source)
	if err != nil {
		return nil, err
	}
	return proxy.NewCredentialResolver(resolver), nil
}
