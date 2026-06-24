package ratelimit_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/nicolapasqua99/genkit-proxy/internal/ratelimit"
)

// testLimiter runs the shared behavioural suite against any Limiter backend.
// Each sub-test uses t.Name() as the base key so parallel runs and shared
// backends (Redis) don't cross-pollute counters.
func testLimiter(t *testing.T, newLimiter func(window time.Duration) ratelimit.Limiter) {
	t.Helper()

	t.Run("below limit allowed", func(t *testing.T) {
		lim := newLimiter(time.Minute)
		defer lim.Close() //nolint:errcheck
		allowed, after, err := lim.Allow(context.Background(), t.Name(), 5)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Error("expected allowed below limit")
		}
		if after != 0 {
			t.Errorf("retryAfter = %v, want 0", after)
		}
	})

	t.Run("at limit still allowed", func(t *testing.T) {
		lim := newLimiter(time.Minute)
		defer lim.Close() //nolint:errcheck
		for i := range 3 {
			allowed, _, err := lim.Allow(context.Background(), t.Name(), 3)
			if err != nil {
				t.Fatal(err)
			}
			if !allowed {
				t.Fatalf("request %d of 3 should be allowed (at-limit)", i+1)
			}
		}
	})

	t.Run("over limit denied with retry-after", func(t *testing.T) {
		lim := newLimiter(time.Minute)
		defer lim.Close() //nolint:errcheck
		for range 3 {
			lim.Allow(context.Background(), t.Name(), 3) //nolint:errcheck
		}
		allowed, after, err := lim.Allow(context.Background(), t.Name(), 3)
		if err != nil {
			t.Fatal(err)
		}
		if allowed {
			t.Error("expected denied over limit")
		}
		if after <= 0 {
			t.Errorf("retryAfter = %v, want > 0", after)
		}
	})

	t.Run("window reset allows again", func(t *testing.T) {
		lim := newLimiter(200 * time.Millisecond)
		defer lim.Close()                            //nolint:errcheck
		lim.Allow(context.Background(), t.Name(), 1) //nolint:errcheck
		allowed, _, _ := lim.Allow(context.Background(), t.Name(), 1)
		if allowed {
			t.Error("second request in same window should be denied")
		}
		time.Sleep(250 * time.Millisecond)
		allowed, _, err := lim.Allow(context.Background(), t.Name(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Error("expected allowed after window reset")
		}
	})

	t.Run("key independence", func(t *testing.T) {
		lim := newLimiter(time.Minute)
		defer lim.Close() //nolint:errcheck
		keyA := t.Name() + "-a"
		keyB := t.Name() + "-b"
		for range 5 {
			lim.Allow(context.Background(), keyA, 5) //nolint:errcheck
		}
		allowed, _, err := lim.Allow(context.Background(), keyB, 5)
		if err != nil {
			t.Fatal(err)
		}
		if !allowed {
			t.Error("key-b should be independent of key-a")
		}
	})

	t.Run("concurrent safety", func(t *testing.T) {
		lim := newLimiter(time.Minute)
		defer lim.Close() //nolint:errcheck
		var wg sync.WaitGroup
		for range 100 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				lim.Allow(context.Background(), t.Name(), 200) //nolint:errcheck
			}()
		}
		wg.Wait()
	})
}

func TestMemoryLimiter(t *testing.T) {
	testLimiter(t, func(window time.Duration) ratelimit.Limiter {
		return ratelimit.NewMemoryLimiter(window)
	})
}

func TestRedisLimiter(t *testing.T) {
	srv := miniredis.RunT(t)
	testLimiter(t, func(window time.Duration) ratelimit.Limiter {
		client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
		return ratelimit.NewRedisLimiter(client, window)
	})

	t.Run("redis error fails open", func(t *testing.T) {
		srv2 := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{
			Addr:       srv2.Addr(),
			MaxRetries: 0, // no retries so the test completes immediately
		})
		lim := ratelimit.NewRedisLimiter(client, time.Minute)
		srv2.Close()

		allowed, after, err := lim.Allow(context.Background(), t.Name(), 1)
		if !allowed {
			t.Error("should fail open on Redis error")
		}
		if after != 0 {
			t.Errorf("retryAfter = %v, want 0 on error", after)
		}
		if err == nil {
			t.Error("expected non-nil error from unavailable Redis")
		}
		lim.Close() //nolint:errcheck
	})
}
