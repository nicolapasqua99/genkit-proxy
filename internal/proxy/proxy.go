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
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	apiKey, err := bearerToken(r)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, err.Error())
		return
	}

	r.Body = http.MaxBytesReader(writer, r.Body, maxRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req GenerateRequest
	if err := dec.Decode(&req); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := handler.gen.Generate(r.Context(), req, apiKey)
	if err != nil {
		status := statusFor(err)
		if classify(err) >= categoryUnauthenticated {
			log.Printf("generate failed: model=%q status=%d err=%v", req.ModelName, status, err)
		}
		writeError(writer, status, safeMessage(err))
		return
	}

	writeJSON(writer, http.StatusOK, resp)
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
	case categoryValidation, categoryUnsupported:
		return http.StatusBadRequest
	case categoryUnauthenticated:
		return http.StatusUnauthorized
	case categoryPermissionDenied:
		return http.StatusForbidden
	case categoryRateLimit:
		return http.StatusTooManyRequests
	case categoryTimeout:
		return http.StatusGatewayTimeout
	case categoryNotFound:
		return http.StatusNotFound
	default:
		return http.StatusBadGateway
	}
}

// errorBody is the JSON envelope for error responses.
type errorBody struct {
	Error string `json:"error"`
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, errorBody{Error: message})
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
