// Command app runs the Genkit-backed, model-agnostic AI proxy HTTP server.
//
// It exposes POST /v1/generate, which accepts a generic generation payload and
// forwards it to the LLM provider named by the request's model prefix, using
// the API key supplied in the Authorization bearer header. GET /healthz is a
// liveness probe. The server listens on $PORT (default 8080) for Cloud Run.
package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/nicolapasqua99/genkit-proxy/internal/proxy"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	handler := proxy.NewHandler(proxy.GenkitGenerator{})

	mux := http.NewServeMux()
	mux.Handle("POST /v1/generate", handler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           proxy.Recover(mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("genkit-proxy listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}
