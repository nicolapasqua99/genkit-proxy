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
	return http.HandlerFunc(func(writer http.ResponseWriter, httpReq *http.Request) {
		tracked := &statusWriter{ResponseWriter: writer}
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				log.Printf("panic recovered: %v", recovered)
				if !tracked.wroteHeader {
					writeJSON(tracked, http.StatusInternalServerError, errorBody{Error: "internal server error"})
				}
			}
		}()
		next.ServeHTTP(tracked, httpReq)
	})
}

// statusWriter tracks whether any response bytes have been sent so the recover
// handler can avoid writing a second status after the body has started.
type statusWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (writer *statusWriter) WriteHeader(code int) {
	writer.wroteHeader = true
	writer.ResponseWriter.WriteHeader(code)
}

func (writer *statusWriter) Write(data []byte) (int, error) {
	writer.wroteHeader = true
	return writer.ResponseWriter.Write(data)
}
