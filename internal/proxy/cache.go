package proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
)

// errCacheClosed is returned by get once the cache has been closed.
var errCacheClosed = errors.New("genkit cache closed")

// GenkitCache memoises *genkit.Genkit instances keyed by (provider, apiKey) so
// the proxy avoids re-running genkit.Init — and rebuilding the provider HTTP
// client and its connection pool — on every request. Entries are bounded by an
// idle TTL and a maximum count (LRU eviction); both also bound how long a
// tenant's provider credential stays resident in memory, which is the tradeoff
// of caching the instance that holds it.
type GenkitCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	closed  bool
	ttl     time.Duration
	maxSize int

	// now and initApp are swapped out in tests. now defaults to time.Now and
	// initApp to defaultInitApp.
	now     func() time.Time
	initApp func(ctx context.Context, plugin api.Plugin) (*genkit.Genkit, error)

	builds sync.WaitGroup // in-flight builds, awaited by Close
	stop   chan struct{}
	wg     sync.WaitGroup // janitor goroutine
}

// cacheEntry holds a memoised instance or an in-flight build. ready is closed
// once app and err are set, so concurrent callers for the same key wait on a
// single build (a "future") instead of each running genkit.Init. app, err, and
// cancel are written under GenkitCache.mu and read either under the lock or
// after ready is closed.
type cacheEntry struct {
	ready    chan struct{}
	app      *genkit.Genkit
	err      error
	cancel   context.CancelFunc
	lastUsed time.Time
}

// NewGenkitCache returns a cache that evicts entries idle longer than ttl and
// caps the number of live entries at maxSize (least-recently-used evicted
// first). A ttl <= 0 disables time-based eviction; a maxSize <= 0 disables the
// size cap. The caller must Close the cache to release cached instances and stop
// the background janitor.
func NewGenkitCache(ttl time.Duration, maxSize int) *GenkitCache {
	c := &GenkitCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
		now:     time.Now,
		initApp: defaultInitApp,
		stop:    make(chan struct{}),
	}
	if ttl > 0 {
		c.wg.Add(1)
		go c.janitor()
	}
	return c
}

// get returns a cached instance for the model's provider and apiKey, building
// and caching one on a miss. The request ctx aborts only the wait for an
// in-flight build; the instance itself is initialised with a cache-lifetime
// context so it outlives the request and is released only on eviction or Close.
// This matches genkit.Init's contract: it ties the goroutine it starts to the
// context's lifetime, so a per-request context would tear the instance down when
// the request ends.
func (c *GenkitCache) get(ctx context.Context, modelName, apiKey string) (*genkit.Genkit, error) {
	provider, err := providerOf(modelName)
	if err != nil {
		return nil, err
	}
	key := cacheKey(provider, apiKey)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errCacheClosed
	}
	if e, ok := c.entries[key]; ok && !c.expiredLocked(e) {
		e.lastUsed = c.now()
		c.mu.Unlock()
		return c.await(ctx, e)
	} else if ok {
		c.removeLocked(key, e)
	}

	e := &cacheEntry{ready: make(chan struct{}), lastUsed: c.now()}
	c.entries[key] = e
	c.builds.Add(1)
	c.evictLocked()
	c.mu.Unlock()
	defer c.builds.Done()

	// Build outside the lock so gets for other keys are not blocked on this one.
	plugin, err := pluginFor(modelName, apiKey)
	if err != nil {
		c.fail(key, e, err)
		return nil, err
	}
	ictx, cancel := context.WithCancel(context.Background())
	app, err := c.initApp(ictx, plugin)
	if err != nil {
		cancel()
		c.fail(key, e, err)
		return nil, err
	}

	c.mu.Lock()
	e.app = app
	e.cancel = cancel
	c.mu.Unlock()
	close(e.ready)
	return app, nil
}

// await blocks until the entry's build finishes or the request context is done.
func (c *GenkitCache) await(ctx context.Context, e *cacheEntry) (*genkit.Genkit, error) {
	select {
	case <-e.ready:
		return e.app, e.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fail records err on the entry, removes it so the next caller retries, and wakes
// any waiters.
func (c *GenkitCache) fail(key string, e *cacheEntry, err error) {
	c.mu.Lock()
	if c.entries[key] == e {
		delete(c.entries, key)
	}
	e.err = err
	c.mu.Unlock()
	close(e.ready)
}

// expiredLocked reports whether e has been idle longer than the TTL. The caller
// holds c.mu.
func (c *GenkitCache) expiredLocked(e *cacheEntry) bool {
	return c.ttl > 0 && c.now().Sub(e.lastUsed) > c.ttl
}

// removeLocked deletes e from the map and cancels its instance context,
// releasing the goroutine genkit.Init started. The caller holds c.mu and must
// only pass a built entry (cancel non-nil); in-flight entries are never removed
// here.
func (c *GenkitCache) removeLocked(key string, e *cacheEntry) {
	delete(c.entries, key)
	if e.cancel != nil {
		e.cancel()
	}
}

// evictLocked enforces the size cap by removing the least-recently-used built
// entry. In-flight entries are never chosen as victims, so a build is never
// orphaned. The caller holds c.mu.
func (c *GenkitCache) evictLocked() {
	if c.maxSize <= 0 || len(c.entries) <= c.maxSize {
		return
	}
	var victimKey string
	var victim *cacheEntry
	for k, e := range c.entries {
		if !isReady(e) {
			continue
		}
		if victim == nil || e.lastUsed.Before(victim.lastUsed) {
			victimKey, victim = k, e
		}
	}
	if victim != nil {
		c.removeLocked(victimKey, victim)
	}
}

// janitor periodically sweeps expired entries until the cache is closed.
func (c *GenkitCache) janitor() {
	defer c.wg.Done()
	t := time.NewTicker(c.ttl)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
			c.sweep()
		}
	}
}

// sweep removes every built entry idle longer than the TTL, releasing its
// instance, so credentials do not linger past the TTL even for keys that are
// never looked up again.
func (c *GenkitCache) sweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		if isReady(e) && c.expiredLocked(e) {
			c.removeLocked(k, e)
		}
	}
}

// Close stops the janitor, waits for in-flight builds to finish, and cancels
// every cached instance, releasing the goroutine each genkit.Init started.
// Subsequent gets return an error. Close must be called at most once.
func (c *GenkitCache) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	close(c.stop)
	c.wg.Wait()
	c.builds.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.entries {
		c.removeLocked(k, e)
	}
}

// isReady reports whether the entry's build has completed.
func isReady(e *cacheEntry) bool {
	select {
	case <-e.ready:
		return true
	default:
		return false
	}
}

// defaultInitApp builds a Genkit instance for plugin. It recovers from the panic
// genkit.Init and plugin.Init raise on bad configuration (e.g. missing Vertex
// credentials) so a failed build surfaces as an error and never strands callers
// waiting on the entry's ready channel.
func defaultInitApp(ctx context.Context, plugin api.Plugin) (app *genkit.Genkit, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("genkit init: %v", r)
		}
	}()
	return genkit.Init(ctx, genkit.WithPlugins(plugin)), nil
}

// cacheKey derives an opaque map key from the provider and apiKey so raw
// credentials are not used as map keys. The credential still lives in memory
// inside the cached plugin; this only avoids keying the map on it directly.
func cacheKey(provider, apiKey string) string {
	sum := sha256.Sum256([]byte(provider + "\x00" + apiKey))
	return hex.EncodeToString(sum[:])
}
