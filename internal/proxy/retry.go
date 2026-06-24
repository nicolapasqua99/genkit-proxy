package proxy

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"
)

// RetryingGenerator wraps a Generator and retries transient upstream failures
// with exponential backoff and full jitter.
type RetryingGenerator struct {
	inner       Generator
	maxAttempts int
	baseBackoff time.Duration
}

// NewRetryingGenerator returns a Generator that retries transient errors up to
// maxAttempts total attempts, with exponential backoff starting at baseBackoff.
// When maxAttempts <= 1, inner is returned unchanged (retry disabled).
func NewRetryingGenerator(inner Generator, maxAttempts int, baseBackoff time.Duration) Generator {
	if maxAttempts <= 1 {
		return inner
	}
	return &RetryingGenerator{inner: inner, maxAttempts: maxAttempts, baseBackoff: baseBackoff}
}

// Generate implements Generator, retrying transient errors with backoff.
func (r *RetryingGenerator) Generate(ctx context.Context, req GenerateRequest, apiKey string) (GenerateResponse, error) {
	var lastErr error
	for attempt := range r.maxAttempts {
		resp, err := r.inner.Generate(ctx, req, apiKey)
		if err == nil {
			return resp, nil
		}
		if !isRetryable(err) {
			return GenerateResponse{}, err
		}
		lastErr = err
		if attempt == r.maxAttempts-1 {
			break
		}
		wait := retryDelay(r.baseBackoff, attempt)
		slog.WarnContext(ctx, "generate failed, retrying",
			"attempt", attempt+1,
			"model", req.ModelName,
			"wait_ms", wait.Milliseconds(),
			"err", err,
		)
		select {
		case <-ctx.Done():
			return GenerateResponse{}, ctx.Err()
		case <-time.After(wait):
		}
	}
	slog.WarnContext(ctx, "generate failed after all attempts",
		"attempts", r.maxAttempts,
		"model", req.ModelName,
		"err", lastErr,
	)
	return GenerateResponse{}, lastErr
}

// GenerateStream implements Generator, retrying before the first chunk is sent.
func (r *RetryingGenerator) GenerateStream(ctx context.Context, req GenerateRequest, apiKey string, onChunk func(string) error) (GenerateResponse, error) {
	var lastErr error
	for attempt := range r.maxAttempts {
		chunkSent := false
		wrapped := func(delta string) error {
			chunkSent = true
			return onChunk(delta)
		}
		resp, err := r.inner.GenerateStream(ctx, req, apiKey, wrapped)
		if err == nil {
			return resp, nil
		}
		if chunkSent || !isRetryable(err) {
			return GenerateResponse{}, err
		}
		lastErr = err
		if attempt == r.maxAttempts-1 {
			break
		}
		wait := retryDelay(r.baseBackoff, attempt)
		slog.WarnContext(ctx, "generate stream failed, retrying",
			"attempt", attempt+1,
			"model", req.ModelName,
			"wait_ms", wait.Milliseconds(),
			"err", err,
		)
		select {
		case <-ctx.Done():
			return GenerateResponse{}, ctx.Err()
		case <-time.After(wait):
		}
	}
	slog.WarnContext(ctx, "generate stream failed after all attempts",
		"attempts", r.maxAttempts,
		"model", req.ModelName,
		"err", lastErr,
	)
	return GenerateResponse{}, lastErr
}

// isRetryable reports whether err represents a transient upstream condition
// that warrants a retry. context.Canceled (client disconnected) is not retried.
func isRetryable(err error) bool {
	switch classify(err) {
	case categoryRateLimit, categoryUpstream:
		return true
	case categoryTimeout:
		return !errors.Is(err, context.Canceled)
	}
	return false
}

// retryDelay returns the wait duration for attempt using exponential backoff
// with full jitter: a random value in [0, base*2^attempt) capped at 10s.
func retryDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	cap := base * (1 << attempt)
	if cap > 10*time.Second {
		cap = 10 * time.Second
	}
	return time.Duration(rand.N(int64(cap)))
}
