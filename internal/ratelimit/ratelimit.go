package ratelimit

import (
	"context"
	"time"
)

// Limiter enforces per-key request budgets within a fixed window.
//
// Allow returns (true, 0, nil) when the request is within the budget,
// (false, retryAfter, nil) when the budget is exceeded, and
// (true, 0, err) when the backend is unavailable — callers must fail open.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int) (allowed bool, retryAfter time.Duration, err error)
	Close() error
}
