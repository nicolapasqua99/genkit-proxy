package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// maxRequestBytes caps the size of a decoded request body.
const maxRequestBytes = 1 << 20 // 1 MiB

// Handler serves generation requests over HTTP.
type Handler struct {
	generator Generator
}

// NewHandler returns a Handler that routes requests through generator.
func NewHandler(generator Generator) *Handler {
	return &Handler{generator: generator}
}

// ServeHTTP decodes a GenerateRequest, extracts the bearer credential, routes
// the request through the Generator, and writes the JSON response.
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, httpReq *http.Request) {
	if httpReq.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	apiKey, err := bearerToken(httpReq)
	if err != nil {
		writeError(writer, http.StatusUnauthorized, err.Error())
		return
	}

	httpReq.Body = http.MaxBytesReader(writer, httpReq.Body, maxRequestBytes)
	dec := json.NewDecoder(httpReq.Body)
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

	if slot := modelSlotFromContext(httpReq.Context()); slot != nil {
		slot.name = req.ModelName
	}

	resp, err := handler.generator.Generate(httpReq.Context(), req, apiKey)
	if err != nil {
		status := statusFor(err)
		if classify(err) >= categoryUnauthenticated {
			slog.ErrorContext(httpReq.Context(), "generate failed",
				"model", req.ModelName,
				"status", status,
				"err", err,
				"request_id", requestIDFromContext(httpReq.Context()),
			)
		}
		writeError(writer, status, safeMessage(err))
		return
	}

	writeJSON(writer, http.StatusOK, resp)
}

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header per RFC 7235 (scheme comparison is case-insensitive).
// Returns ErrMissingCredentials when the header is absent or malformed.
func bearerToken(httpReq *http.Request) (string, error) {
	header := httpReq.Header.Get("Authorization")
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
