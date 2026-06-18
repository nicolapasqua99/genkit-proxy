package proxy

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// scopeName identifies this service as the OpenTelemetry instrumentation scope.
const scopeName = "github.com/nicolapasqua99/genkit-proxy"

// latencyBoundaries are the request-latency histogram buckets, in seconds. They
// span fast health-check responses through slow multi-second LLM generations.
var latencyBoundaries = []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 20, 30, 60}

// Metrics records per-request HTTP metrics and exposes them in Prometheus
// exposition format. Build one with NewMetrics, mount Handler at GET /metrics,
// and wrap the mux with Middleware. Middleware must run inside Logger, which
// installs the model slot Middleware reads to label metrics by provider.
type Metrics struct {
	requests metric.Int64Counter
	latency  metric.Float64Histogram
	handler  http.Handler
}

// NewMetrics builds a Metrics backed by an OpenTelemetry meter whose data is
// exported through a dedicated Prometheus registry, so the exposition contains
// only this service's metrics.
func NewMetrics() (*Metrics, error) {
	registry := prometheus.NewRegistry()
	exporter, err := otelprom.New(
		otelprom.WithRegisterer(registry),
		otelprom.WithoutScopeInfo(),
		otelprom.WithoutTargetInfo(),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: prometheus exporter: %w", err)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithView(sdkmetric.NewView(
			sdkmetric.Instrument{Name: "http_request_duration"},
			sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: latencyBoundaries,
			}},
		)),
	)
	meter := provider.Meter(scopeName)

	requests, err := meter.Int64Counter(
		"http_requests",
		metric.WithDescription("Total HTTP requests handled, by method, status, and provider."),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: requests counter: %w", err)
	}
	latency, err := meter.Float64Histogram(
		"http_request_duration",
		metric.WithDescription("HTTP request latency in seconds, by method, status, and provider."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: latency histogram: %w", err)
	}

	return &Metrics{
		requests: requests,
		latency:  latency,
		handler:  promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	}, nil
}

// Handler serves the collected metrics in Prometheus exposition format. Mount
// it at GET /metrics.
func (m *Metrics) Handler() http.Handler { return m.handler }

// Middleware records the count and latency of each request flowing through
// next, labelled by HTTP method, response status, and — for generation
// requests — the model's provider prefix.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, httpReq *http.Request) {
		start := time.Now()

		// Reuse the model slot installed by Logger, or install one here so the
		// provider label is captured even when Middleware runs without Logger.
		ctx := httpReq.Context()
		slot := modelSlotFromContext(ctx)
		if slot == nil {
			slot = &modelSlot{}
			ctx = context.WithValue(ctx, modelKey, slot)
			httpReq = httpReq.WithContext(ctx)
		}

		tracked := &statusWriter{ResponseWriter: writer}
		next.ServeHTTP(tracked, httpReq)

		code := tracked.code
		if code == 0 {
			code = http.StatusOK
		}
		attrs := metric.WithAttributes(
			attribute.String("method", httpReq.Method),
			attribute.Int("status", code),
			attribute.String("provider", providerLabel(slot.name)),
		)
		m.requests.Add(ctx, 1, attrs)
		m.latency.Record(ctx, time.Since(start).Seconds(), attrs)
	})
}

// providerLabel returns the provider prefix of a provider-namespaced model name
// for use as a metric label, or "" when no model was recorded (for example on
// non-generation routes such as health checks).
func providerLabel(modelName string) string {
	provider, _, _ := strings.Cut(modelName, "/")
	return provider
}
