package proxy

import (
	"log"
	"net/http"
)

// Recover wraps next so that a panic in a handler is logged and converted into
// a clean 500 JSON response instead of dropping the connection. If the response
// has already started (headers written), only logs — the status cannot be
// changed at that point. Re-panics on http.ErrAbortHandler to preserve stdlib
// semantics.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				log.Printf("panic recovered: %v", rec)
				if !sw.wroteHeader {
					writeJSON(sw, http.StatusInternalServerError, errorBody{Error: "internal server error"})
				}
			}
		}()
		next.ServeHTTP(sw, r)
	})
}

// statusWriter tracks whether any response bytes have been sent so the recover
// handler can avoid writing a second status after the body has started.
type statusWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}
