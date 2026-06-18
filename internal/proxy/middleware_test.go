package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecover(t *testing.T) {
	t.Run("panic before write", func(t *testing.T) {
		handler := Recover(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			panic("something went wrong")
		}))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", recorder.Code)
		}
		var body errorBody
		if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Error != "internal server error" {
			t.Errorf("body.Error = %q, want %q", body.Error, "internal server error")
		}
	})

	t.Run("panic after write", func(t *testing.T) {
		handler := Recover(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("partial"))
			panic("late panic")
		}))
		recorder := httptest.NewRecorder()
		// Must not panic out of ServeHTTP.
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 (headers already sent)", recorder.Code)
		}
	})

	t.Run("no panic passthrough", func(t *testing.T) {
		handler := Recover(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", recorder.Code)
		}
	})

	t.Run("abort handler re-panics", func(t *testing.T) {
		handler := Recover(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			panic(http.ErrAbortHandler)
		}))
		recorder := httptest.NewRecorder()

		var repanicked bool
		func() {
			defer func() {
				if recovered := recover(); recovered == http.ErrAbortHandler {
					repanicked = true
				}
			}()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		}()

		if !repanicked {
			t.Error("expected Recover to re-panic with http.ErrAbortHandler")
		}
	})
}

func TestRequestID(t *testing.T) {
	t.Run("generates UUID when header absent", func(t *testing.T) {
		var gotID string
		handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			gotID = requestIDFromContext(r.Context())
		}))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

		if gotID == "" {
			t.Error("expected a generated request ID in context, got empty string")
		}
		if recorder.Header().Get("X-Request-ID") != gotID {
			t.Errorf("response header X-Request-ID = %q, want %q", recorder.Header().Get("X-Request-ID"), gotID)
		}
	})

	t.Run("echoes inbound X-Request-ID", func(t *testing.T) {
		const want = "my-trace-id"
		var gotID string
		handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			gotID = requestIDFromContext(r.Context())
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", want)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)

		if gotID != want {
			t.Errorf("context ID = %q, want %q", gotID, want)
		}
		if recorder.Header().Get("X-Request-ID") != want {
			t.Errorf("response header X-Request-ID = %q, want %q", recorder.Header().Get("X-Request-ID"), want)
		}
	})
}

func TestLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	orig := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(orig) })

	t.Run("logs method path status latency request_id", func(t *testing.T) {
		buf.Reset()
		const reqID = "test-req-id"
		handler := RequestID(Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		})))
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("X-Request-ID", reqID)
		handler.ServeHTTP(httptest.NewRecorder(), req)

		line := buf.String()
		for _, want := range []string{"method=GET", "path=/healthz", "status=201", "request_id=" + reqID} {
			if !strings.Contains(line, want) {
				t.Errorf("log line missing %q; got: %s", want, line)
			}
		}
	})

	t.Run("logs model when slot populated", func(t *testing.T) {
		buf.Reset()
		handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slot := modelSlotFromContext(r.Context()); slot != nil {
				slot.name = "googleai/gemini-2.5-flash"
			}
			w.WriteHeader(http.StatusOK)
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/generate", nil))

		if !strings.Contains(buf.String(), "model=googleai/gemini-2.5-flash") {
			t.Errorf("expected model in log; got: %s", buf.String())
		}
	})

	t.Run("does not log Authorization header", func(t *testing.T) {
		buf.Reset()
		handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodPost, "/v1/generate", nil)
		req.Header.Set("Authorization", "Bearer super-secret-token")
		handler.ServeHTTP(httptest.NewRecorder(), req)

		if strings.Contains(buf.String(), "super-secret-token") {
			t.Errorf("Authorization value must not appear in log; got: %s", buf.String())
		}
	})

	t.Run("defaults status to 200 when handler never calls WriteHeader", func(t *testing.T) {
		buf.Reset()
		handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

		if !strings.Contains(buf.String(), "status=200") {
			t.Errorf("expected status=200; got: %s", buf.String())
		}
	})
}

func TestRequestIDFromContext(t *testing.T) {
	t.Run("returns empty string when absent", func(t *testing.T) {
		if got := requestIDFromContext(context.Background()); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}
