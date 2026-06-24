package proxy

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/firebase/genkit/go/genkit"
)

// leakThreshold is the goroutine slack tolerated after repeated genkit.Init.
// A genuine per-Init leak would add one goroutine per call (50 below), so a
// small constant cleanly distinguishes "settled" from "leaking".
const leakThreshold = 10

// initLikeGenkitRun mirrors how genkitRun uses genkit.Init: a fresh instance
// per call with a request-scoped context that is cancelled when the call
// returns. genkit.Init discards the stop func from signal.NotifyContext, so
// the signal goroutine it starts is released only when this context is
// cancelled. No plugins are passed, so the call performs no network I/O.
func initLikeGenkitRun() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = genkit.Init(ctx)
}

// settleGoroutines polls until the goroutine count drops to at most want, or
// the deadline elapses, returning the final observed count. The signal
// goroutine genkit.Init starts unwinds asynchronously after its context is
// cancelled, so the count must be allowed to settle before asserting.
func settleGoroutines(want int, timeout time.Duration) int {
	deadline := time.Now().Add(timeout)
	for {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n <= want || time.Now().After(deadline) {
			return n
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// goroutineBaseline runs one Init to warm up process-wide singletons (such as
// the global tracer provider, which is set once via sync.Once and is therefore
// not a per-Init cost), lets the runtime settle, and returns the resulting
// goroutine count.
func goroutineBaseline() int {
	initLikeGenkitRun()
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	return runtime.NumGoroutine()
}

// TestGenkitInitSequentialNoGoroutineLeak verifies that repeated, request-scoped
// genkit.Init calls do not leak goroutines: each call's signal goroutine is
// released once its context is cancelled.
func TestGenkitInitSequentialNoGoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	const n = 50
	for range n {
		initLikeGenkitRun()
	}

	got := settleGoroutines(baseline+leakThreshold, 2*time.Second)
	if got > baseline+leakThreshold {
		t.Errorf("goroutines did not settle after %d sequential inits: baseline=%d got=%d (delta=%d)",
			n, baseline, got, got-baseline)
	}
}

// TestGenkitInitConcurrentNoGoroutineLeak verifies the same invariant under
// concurrent Init calls, since the proxy serves requests in parallel.
func TestGenkitInitConcurrentNoGoroutineLeak(t *testing.T) {
	baseline := goroutineBaseline()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			initLikeGenkitRun()
		}()
	}
	wg.Wait()

	got := settleGoroutines(baseline+leakThreshold, 2*time.Second)
	if got > baseline+leakThreshold {
		t.Errorf("goroutines did not settle after %d concurrent inits: baseline=%d got=%d (delta=%d)",
			n, baseline, got, got-baseline)
	}
}
