package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// scrape returns the Prometheus exposition text from m's metrics handler.
func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestProviderLabel(t *testing.T) {
	cases := []struct {
		name  string
		model string
		want  string
	}{
		{"googleai", "googleai/gemini-2.5-flash", "googleai"},
		{"openai", "openai/gpt-4o", "openai"},
		{"anthropic", "anthropic/claude-3-5-sonnet", "anthropic"},
		{"empty model", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerLabel(tc.model); got != tc.want {
				t.Errorf("providerLabel(%q) = %q, want %q", tc.model, got, tc.want)
			}
		})
	}
}

func TestMetricsMiddleware(t *testing.T) {
	t.Run("records generate request with provider label", func(t *testing.T) {
		m, err := NewMetrics()
		if err != nil {
			t.Fatalf("NewMetrics: %v", err)
		}
		handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slot := modelSlotFromContext(r.Context()); slot != nil {
				slot.name = "googleai/gemini-2.5-flash"
			}
			w.WriteHeader(http.StatusOK)
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/generate", nil))

		out := scrape(t, m)
		for _, want := range []string{
			"http_requests_total{",
			"http_request_duration_seconds_count{",
			`method="POST"`,
			`provider="googleai"`,
			`status="200"`,
		} {
			if !strings.Contains(out, want) {
				t.Errorf("scrape output missing %q\n%s", want, out)
			}
		}
	})

	t.Run("defaults status to 200 and empty provider on non-generate route", func(t *testing.T) {
		m, err := NewMetrics()
		if err != nil {
			t.Fatalf("NewMetrics: %v", err)
		}
		handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

		out := scrape(t, m)
		for _, want := range []string{`status="200"`, `provider=""`, `method="GET"`} {
			if !strings.Contains(out, want) {
				t.Errorf("scrape output missing %q\n%s", want, out)
			}
		}
	})

	t.Run("records error status", func(t *testing.T) {
		m, err := NewMetrics()
		if err != nil {
			t.Fatalf("NewMetrics: %v", err)
		}
		handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/generate", nil))

		if out := scrape(t, m); !strings.Contains(out, `status="502"`) {
			t.Errorf("expected status=\"502\"\n%s", out)
		}
	})

	t.Run("counts repeated requests", func(t *testing.T) {
		m, err := NewMetrics()
		if err != nil {
			t.Fatalf("NewMetrics: %v", err)
		}
		handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		for range 3 {
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/readyz", nil))
		}

		// Prometheus sorts labels alphabetically: method, provider, status.
		if out := scrape(t, m); !strings.Contains(out, `http_requests_total{method="GET",provider="",status="200"} 3`) {
			t.Errorf("expected counter value 3\n%s", out)
		}
	})

	t.Run("reuses the model slot installed by Logger", func(t *testing.T) {
		m, err := NewMetrics()
		if err != nil {
			t.Fatalf("NewMetrics: %v", err)
		}
		handler := Logger(m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slot := modelSlotFromContext(r.Context()); slot != nil {
				slot.name = "anthropic/claude-3-5-sonnet"
			}
			w.WriteHeader(http.StatusOK)
		})))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/generate", nil))

		if out := scrape(t, m); !strings.Contains(out, `provider="anthropic"`) {
			t.Errorf("expected provider label captured via shared slot\n%s", out)
		}
	})
}
