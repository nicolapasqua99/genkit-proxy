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
		handler := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("partial"))
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
		handler := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

		if recorder.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", recorder.Code)
		}
	})

	t.Run("abort handler re-panics", func(t *testing.T) {
		handler := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			panic(http.ErrAbortHandler)
		}))
		recorder := httptest.NewRecorder()

		var repanicked bool
		func() {
			defer func() {
				if r := recover(); r == http.ErrAbortHandler {
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
