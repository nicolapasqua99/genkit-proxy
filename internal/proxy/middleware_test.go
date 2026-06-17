package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecover(t *testing.T) {
	t.Run("panic before write", func(t *testing.T) {
		handler := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			panic("something went wrong")
		}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		var body errorBody
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Error != "internal server error" {
			t.Errorf("body.Error = %q, want %q", body.Error, "internal server error")
		}
	})

	t.Run("panic after write", func(t *testing.T) {
		handler := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("partial"))
			panic("late panic")
		}))
		rec := httptest.NewRecorder()
		// Must not panic out of ServeHTTP.
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 (headers already sent)", rec.Code)
		}
	})

	t.Run("no panic passthrough", func(t *testing.T) {
		handler := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("abort handler re-panics", func(t *testing.T) {
		handler := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			panic(http.ErrAbortHandler)
		}))
		rec := httptest.NewRecorder()

		var repanicked bool
		func() {
			defer func() {
				if r := recover(); r == http.ErrAbortHandler {
					repanicked = true
				}
			}()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		}()

		if !repanicked {
			t.Error("expected Recover to re-panic with http.ErrAbortHandler")
		}
	})
}
