package proxy

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// ServeStream handles POST /v1/generate/stream, emitting the generation as
// Server-Sent Events: a "chunk" event per text delta, a terminating "done"
// event carrying the model, finish reason, and usage, or an "error" event when
// generation fails after streaming has begun. Errors before the first byte are
// returned as an ordinary JSON error with the appropriate status.
func (handler *Handler) ServeStream(writer http.ResponseWriter, httpReq *http.Request) {
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

	ctx := httpReq.Context()

	if !handler.checkModelLimit(writer, ctx, apiKey, req.ModelName) {
		return
	}

	if handler.limiter != nil && handler.rlCfg.StreamLimit > 0 {
		spanCtx, span := otel.Tracer("proxy").Start(ctx, "ratelimit.check")
		key := rateLimitKey(apiKey, "stream")
		allowed, retryAfter, rlErr := handler.limiter.Allow(spanCtx, key, handler.rlCfg.StreamLimit)
		span.SetAttributes(
			attribute.String("rl.layer", "stream"),
			attribute.Bool("rl.allowed", allowed),
			attribute.Int("rl.retry_after_sec", int(retryAfter.Seconds())),
		)
		span.End()
		if rlErr != nil {
			slog.WarnContext(ctx, "rate limiter error", "err", rlErr)
			// fail open: continue to generation on backend error
		} else if !allowed {
			writer.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeError(writer, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
	}

	if slot := modelSlotFromContext(ctx); slot != nil {
		slot.name = req.ModelName
	}

	controller := http.NewResponseController(writer)
	// A stream can outlast the configured WriteTimeout; rely on the per-request
	// generation timeout instead. Test recorders return ErrNotSupported — ignore.
	_ = controller.SetWriteDeadline(time.Time{})

	started := false
	start := func() {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Cache-Control", "no-cache")
		writer.WriteHeader(http.StatusOK)
		started = true
	}
	onChunk := func(delta string) error {
		if !started {
			start()
		}
		if err := writeSSEEvent(writer, "chunk", chunkEvent{Delta: delta}); err != nil {
			return err
		}
		return controller.Flush()
	}

	final, err := handler.generator.GenerateStream(ctx, req, apiKey, onChunk)
	if err != nil {
		if !started {
			status := statusFor(err)
			if classify(err) >= categoryUnauthenticated {
				slog.ErrorContext(ctx, "generate stream failed",
					"model", req.ModelName,
					"status", status,
					"err", err,
					"request_id", requestIDFromContext(ctx),
				)
			}
			writeError(writer, status, safeMessage(err))
			return
		}
		// Headers already sent: report the failure as an SSE error event.
		slog.ErrorContext(ctx, "generate stream failed mid-stream",
			"model", req.ModelName,
			"err", err,
			"request_id", requestIDFromContext(ctx),
		)
		_ = writeSSEEvent(writer, "error", errorBody{Error: safeMessage(err)})
		_ = controller.Flush()
		return
	}

	if !started {
		start()
	}
	if slot := modelSlotFromContext(ctx); slot != nil {
		slot.usage = final.Usage
	}
	_ = writeSSEEvent(writer, "done", doneEvent{
		Model:        final.Model,
		FinishReason: final.FinishReason,
		ToolCalls:    final.ToolCalls,
		Usage:        final.Usage,
	})
	_ = controller.Flush()
}

// chunkEvent is the SSE "chunk" payload carrying one text delta.
type chunkEvent struct {
	Delta string `json:"delta"`
}

// doneEvent is the SSE "done" payload sent once generation completes.
type doneEvent struct {
	Model        string     `json:"model"`
	FinishReason string     `json:"finishReason,omitempty"`
	ToolCalls    []ToolCall `json:"toolCalls,omitempty"`
	Usage        *Usage     `json:"usage,omitempty"`
}

// writeSSEEvent writes one named Server-Sent Event with a JSON data payload.
func writeSSEEvent(writer http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, data)
	return err
}
