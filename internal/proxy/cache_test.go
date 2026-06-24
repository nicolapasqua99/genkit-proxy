package proxy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
)

const testModel = "googleai/gemini-2.5-flash"

// countingInit is a fake initApp: it returns a distinct instance per call,
// records the contexts it was handed (so tests can assert cancellation), and
// counts invocations. A non-nil block channel makes init wait, forcing
// concurrent callers to overlap; a non-nil err is returned instead of an
// instance.
type countingInit struct {
	mu    sync.Mutex
	calls int
	ctxs  []context.Context
	block chan struct{}
	err   error
}

func (f *countingInit) init(ctx context.Context, _ api.Plugin) (*genkit.Genkit, error) {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.ctxs = append(f.ctxs, ctx)
	if f.err != nil {
		return nil, f.err
	}
	return &genkit.Genkit{}, nil
}

func (f *countingInit) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *countingInit) ctx(i int) context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ctxs[i]
}

// newTestCache builds a cache without starting the background janitor, so tests
// drive expiry deterministically via sweep and a controllable clock.
func newTestCache(ttl time.Duration, maxSize int, init *countingInit) *GenkitCache {
	return &GenkitCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
		now:     time.Now,
		initApp: init.init,
		stop:    make(chan struct{}),
	}
}

func cacheSize(c *GenkitCache) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// fakeClock is a controllable time source for TTL tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestGenkitCacheReusesInstance(t *testing.T) {
	init := &countingInit{}
	c := newTestCache(0, 0, init)
	defer c.Close()

	a1, err := c.get(context.Background(), testModel, "key-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	a2, err := c.get(context.Background(), testModel, "key-a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if a1 != a2 {
		t.Error("cache returned different instances for the same key")
	}
	if init.count() != 1 {
		t.Errorf("init calls = %d, want 1", init.count())
	}
}

func TestGenkitCacheKeying(t *testing.T) {
	init := &countingInit{}
	c := newTestCache(0, 0, init)
	defer c.Close()

	a, _ := c.get(context.Background(), testModel, "key-a")
	b, _ := c.get(context.Background(), testModel, "key-b")
	if a == b {
		t.Error("distinct keys should not share an instance")
	}
	// A different model of the same provider with the same key shares the instance.
	same, _ := c.get(context.Background(), "googleai/gemini-2.5-pro", "key-a")
	if same != a {
		t.Error("same provider+key should share an instance across models")
	}
	if init.count() != 2 {
		t.Errorf("init calls = %d, want 2", init.count())
	}
}

func TestGenkitCacheInvalidModel(t *testing.T) {
	init := &countingInit{}
	c := newTestCache(0, 0, init)
	defer c.Close()

	if _, err := c.get(context.Background(), "noprovider", "k"); err == nil {
		t.Fatal("expected error for non-provider-prefixed model")
	}
	if init.count() != 0 {
		t.Errorf("init calls = %d, want 0", init.count())
	}
}

func TestGenkitCacheMaxSizeEviction(t *testing.T) {
	init := &countingInit{}
	c := newTestCache(0, 1, init)
	defer c.Close()

	c.get(context.Background(), testModel, "key-a")
	// Inserting a second entry over the cap evicts the LRU (key-a).
	c.get(context.Background(), testModel, "key-b")

	if got := cacheSize(c); got != 1 {
		t.Errorf("size = %d, want 1", got)
	}
	if init.ctx(0).Err() == nil {
		t.Error("evicted instance context was not cancelled")
	}
	// key-a is gone, so it rebuilds on the next get.
	c.get(context.Background(), testModel, "key-a")
	if init.count() != 3 {
		t.Errorf("init calls = %d, want 3 (a, b, a-rebuilt)", init.count())
	}
}

func TestGenkitCacheTTLExpiryOnGet(t *testing.T) {
	init := &countingInit{}
	clock := &fakeClock{t: time.Now()}
	c := newTestCache(time.Minute, 0, init)
	c.now = clock.now
	defer c.Close()

	a, _ := c.get(context.Background(), testModel, "key-a")
	clock.advance(2 * time.Minute)
	a2, _ := c.get(context.Background(), testModel, "key-a")

	if a == a2 {
		t.Error("expired entry should be rebuilt")
	}
	if init.ctx(0).Err() == nil {
		t.Error("expired instance context was not cancelled")
	}
	if init.count() != 2 {
		t.Errorf("init calls = %d, want 2", init.count())
	}
}

func TestGenkitCacheSweepEvictsIdle(t *testing.T) {
	init := &countingInit{}
	clock := &fakeClock{t: time.Now()}
	c := newTestCache(time.Minute, 0, init)
	c.now = clock.now
	defer c.Close()

	c.get(context.Background(), testModel, "key-a")
	clock.advance(2 * time.Minute)
	c.sweep()

	if got := cacheSize(c); got != 0 {
		t.Errorf("size after sweep = %d, want 0", got)
	}
	if init.ctx(0).Err() == nil {
		t.Error("swept instance context was not cancelled")
	}
}

func TestGenkitCacheConcurrentSingleBuild(t *testing.T) {
	init := &countingInit{block: make(chan struct{})}
	c := newTestCache(0, 0, init)
	defer c.Close()

	const n = 20
	apps := make([]*genkit.Genkit, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			app, err := c.get(context.Background(), testModel, "key-a")
			if err != nil {
				t.Errorf("get: %v", err)
				return
			}
			apps[i] = app
		}(i)
	}
	// Let callers pile up on the single in-flight build, then release it.
	time.Sleep(20 * time.Millisecond)
	close(init.block)
	wg.Wait()

	if init.count() != 1 {
		t.Errorf("init calls = %d, want 1 (concurrent gets deduped)", init.count())
	}
	for i := 1; i < n; i++ {
		if apps[i] != apps[0] {
			t.Fatalf("goroutine %d got a different instance", i)
		}
	}
}

func TestGenkitCacheGetWaitRespectsContext(t *testing.T) {
	init := &countingInit{block: make(chan struct{})}
	c := newTestCache(0, 0, init)
	defer func() {
		close(init.block)
		c.Close()
	}()

	// Start a build that blocks, occupying the in-flight entry.
	go c.get(context.Background(), testModel, "key-a") //nolint:errcheck
	time.Sleep(20 * time.Millisecond)

	// A second caller whose context is already cancelled must not block.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.get(ctx, testModel, "key-a"); !errors.Is(err, context.Canceled) {
		t.Errorf("get err = %v, want context.Canceled", err)
	}
}

func TestGenkitCacheInitError(t *testing.T) {
	wantErr := errors.New("init failed")
	init := &countingInit{err: wantErr}
	c := newTestCache(0, 0, init)
	defer c.Close()

	if _, err := c.get(context.Background(), testModel, "key-a"); !errors.Is(err, wantErr) {
		t.Fatalf("get err = %v, want %v", err, wantErr)
	}
	if got := cacheSize(c); got != 0 {
		t.Errorf("failed build should not be cached, size = %d", got)
	}
	// The next get retries and can succeed.
	init.mu.Lock()
	init.err = nil
	init.mu.Unlock()
	if _, err := c.get(context.Background(), testModel, "key-a"); err != nil {
		t.Fatalf("retry get: %v", err)
	}
	if init.count() != 2 {
		t.Errorf("init calls = %d, want 2", init.count())
	}
}

func TestGenkitCacheCloseCancelsAll(t *testing.T) {
	init := &countingInit{}
	c := NewGenkitCache(time.Hour, 0) // starts the janitor
	c.initApp = init.init

	c.get(context.Background(), testModel, "key-a")
	c.get(context.Background(), testModel, "key-b")
	c.Close()

	for i := range init.count() {
		if init.ctx(i).Err() == nil {
			t.Errorf("instance %d context not cancelled after Close", i)
		}
	}
	if got := cacheSize(c); got != 0 {
		t.Errorf("entries remain after Close: %d", got)
	}
}
