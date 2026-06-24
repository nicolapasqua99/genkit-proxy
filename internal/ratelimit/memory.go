package ratelimit

import (
	"context"
	"strconv"
	"sync"
	"time"
)

const maxBuckets = 10_000

type bucket struct {
	count   int
	resetAt time.Time
}

type memLimiter struct {
	mu      sync.Mutex
	window  time.Duration
	buckets map[string]*bucket
}

// NewMemoryLimiter returns a Limiter backed by an in-memory map using a
// fixed-window algorithm. The window is the counting bucket duration.
func NewMemoryLimiter(window time.Duration) Limiter {
	return &memLimiter{
		window:  window,
		buckets: make(map[string]*bucket),
	}
}

func (m *memLimiter) Allow(_ context.Context, key string, limit int) (bool, time.Duration, error) {
	now := time.Now()
	windowStart := now.Truncate(m.window)
	fullKey := key + ":" + strconv.FormatInt(windowStart.UnixNano(), 10)
	resetAt := windowStart.Add(m.window)

	m.mu.Lock()
	defer m.mu.Unlock()

	b, ok := m.buckets[fullKey]
	if !ok {
		if len(m.buckets) >= maxBuckets {
			m.evictExpired(now)
		}
		b = &bucket{resetAt: resetAt}
		m.buckets[fullKey] = b
	}
	b.count++

	if b.count > limit {
		return false, time.Until(b.resetAt), nil
	}
	return true, 0, nil
}

// evictExpired removes buckets whose window has already passed. Caller holds mu.
func (m *memLimiter) evictExpired(now time.Time) {
	for k, b := range m.buckets {
		if now.After(b.resetAt) {
			delete(m.buckets, k)
		}
	}
}

func (m *memLimiter) Close() error { return nil }
