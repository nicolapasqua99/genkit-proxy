// Package proxy implements a model-agnostic HTTP gateway that forwards
// generation requests to LLM providers through Firebase Genkit, using
// per-request credentials supplied by the caller. The provider is selected
// dynamically from the provider-prefixed model name (for example
// "googleai/gemini-2.5-flash"), and the caller's API key is taken from the
// request's Authorization header so credentials are never hardcoded.
package proxy

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// maxRequestBytes caps the size of a decoded request body.
const maxRequestBytes = 1 << 20 // 1 MiB

// Handler serves generation requests over HTTP.
type Handler struct {
	gen Generator
}

// NewHandler returns a Handler that routes requests through gen.
func NewHandler(gen Generator) *Handler {
	return &Handler{gen: gen}
}

// ServeHTTP decodes a GenerateRequest, extracts the bearer credential, routes
// the request through the Generator, and writes the JSON response.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	apiKey, err := bearerToken(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req GenerateRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := h.gen.Generate(r.Context(), req, apiKey)
	if err != nil {
		status := statusFor(err)
		if classify(err) >= catUnauthenticated {
			log.Printf("generate failed: model=%q status=%d err=%v", req.ModelName, status, err)
		}
		writeError(w, status, safeMessage(err))
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header per RFC 7235 (scheme comparison is case-insensitive).
// Returns ErrMissingCredentials when the header is absent or malformed.
func bearerToken(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	scheme, rest, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", ErrMissingCredentials
	}
	token := strings.TrimSpace(rest)
	if token == "" {
		return "", ErrMissingCredentials
	}
	return token, nil
}

// statusFor maps a Generator error to an HTTP status code.
func statusFor(err error) int {
	switch classify(err) {
	case catValidation, catUnsupported:
		return http.StatusBadRequest
	case catUnauthenticated:
		return http.StatusUnauthorized
	case catPermissionDenied:
		return http.StatusForbidden
	case catRateLimit:
		return http.StatusTooManyRequests
	case catTimeout:
		return http.StatusGatewayTimeout
	case catNotFound:
		return http.StatusNotFound
	default:
		return http.StatusBadGateway
	}
}

// errorBody is the JSON envelope for error responses.
type errorBody struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
