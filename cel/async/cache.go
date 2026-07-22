// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package async

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/cel-go/common/decls"
	"github.com/google/cel-go/common/functions"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// Cache is the interface for a user-provided or framework-provided cache backend.
type Cache interface {
	// Get retrieves a value from the cache by its key.
	// Returns the value and true if found, nil and false otherwise.
	Get(key string) (ref.Val, bool)
	// Set stores a value in the cache with the given key.
	Set(key string, val ref.Val)
	// Delete removes a value from the cache by its key.
	Delete(key string)
}

// StaleCache extends Cache to support retrieving expired entries for fallback or revalidation.
type StaleCache interface {
	Cache
	// GetStale retrieves a value even if it has expired.
	// Returns (value, isStale, found).
	GetStale(key string) (val ref.Val, isStale bool, found bool)
}

// CacheOption configures the behavior of CachedBinding.
type CacheOption func(*asyncCacheConfig)

type asyncCacheConfig struct {
	cache                Cache
	keyFunc              func(...ref.Val) string
	ttl                  time.Duration
	maxSize              int
	staleWhileError      bool
	staleWhileRevalidate bool
}

// CustomCache provides an externally managed cache instance.
// If not provided, a default concurrent map-based cache is created.
func CustomCache(cache Cache) CacheOption {
	return func(c *asyncCacheConfig) {
		c.cache = cache
	}
}

// CacheKeyFunc provides a custom function for computing cache keys.
// The default computes a string representation of the arguments.
func CacheKeyFunc(fn func(...ref.Val) string) CacheOption {
	return func(c *asyncCacheConfig) {
		c.keyFunc = fn
	}
}

// CacheTTL sets the time-to-live for cache entries.
// Only applicable for the default cache implementation.
func CacheTTL(ttl time.Duration) CacheOption {
	return func(c *asyncCacheConfig) {
		c.ttl = ttl
	}
}

// CacheSize sets the maximum number of cache entries.
// Only applicable for the default cache implementation.
func CacheSize(maxSize int) CacheOption {
	return func(c *asyncCacheConfig) {
		c.maxSize = maxSize
	}
}

// CacheStaleWhileError enables serving stale cache entries if the underlying function fails.
func CacheStaleWhileError(enabled bool) CacheOption {
	return func(c *asyncCacheConfig) {
		c.staleWhileError = enabled
	}
}

// CacheStaleWhileRevalidate enables serving stale cache entries while refreshing them in the background.
func CacheStaleWhileRevalidate(enabled bool) CacheOption {
	return func(c *asyncCacheConfig) {
		c.staleWhileRevalidate = enabled
	}
}

// CachedBinding wraps a BlockingAsyncOp with a configurable cache.
// It returns an OverloadOpt that can be used with cel.Overload.
func CachedBinding(fn functions.BlockingAsyncOp, opts ...CacheOption) decls.OverloadOpt {
	config := &asyncCacheConfig{}
	for _, opt := range opts {
		opt(config)
	}

	if config.cache == nil {
		config.cache = newDefaultCache(config.maxSize, config.ttl)
	}

	return decls.AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
		key, cacheable := cacheKey(config.keyFunc, args)
		if !cacheable {
			// The arguments cannot be keyed safely under the default strategy; bypass the
			// cache entirely rather than risk serving a value for a non-equivalent call.
			return fn(ctx, args...)
		}

		var staleVal ref.Val
		var isStale bool
		var found bool

		if sc, ok := config.cache.(StaleCache); ok {
			staleVal, isStale, found = sc.GetStale(key)
		} else {
			staleVal, found = config.cache.Get(key)
			isStale = false // Standard cache Get doesn't indicate staleness
		}

		if found && !isStale {
			return staleVal
		}

		if found && isStale && config.staleWhileRevalidate {
			// Serve stale immediately and refresh in the background. The refresh must outlive the
			// triggering evaluation, so it runs on a context detached from the request's
			// cancellation/deadline (values are preserved). Using the request context here would
			// cancel the refresh as soon as the evaluation completes, dropping every update.
			refreshCtx := context.WithoutCancel(ctx)
			go func() {
				res := fn(refreshCtx, args...)
				if !types.IsError(res) {
					config.cache.Set(key, res)
				}
			}()
			return staleVal
		}

		res := fn(ctx, args...)
		if !types.IsError(res) {
			config.cache.Set(key, res)
			return res
		}

		if found && isStale && config.staleWhileError {
			// Function failed, but we have a stale value to fall back to
			return staleVal
		}

		return res
	})
}

// cacheKey resolves the cache key for a set of arguments. A user-supplied key function (via
// CacheKeyFunc) is always treated as cacheable; otherwise the default strategy is consulted, which
// only keys string and bool arguments.
func cacheKey(userKeyFunc func(...ref.Val) string, args []ref.Val) (string, bool) {
	if userKeyFunc != nil {
		return userKeyFunc(args...), true
	}
	return defaultKeyFunc(args...)
}

// defaultKeyFunc computes a cache key from string and bool arguments only.
//
// Numeric and more complex types use a richer notion of equivalence (e.g. cross-type numeric
// equality, unordered maps, NaN/-0.0 for doubles) that a byte-level key cannot capture safely.
// Rather than risk returning a cached value for a non-equivalent call, the default strategy
// declines to cache any call whose arguments are not all string or bool, signaled by a false
// cacheable result. Callers needing to cache such functions should supply CacheKeyFunc or
// CustomCache with semantics appropriate to their argument types.
func defaultKeyFunc(args ...ref.Val) (key string, cacheable bool) {
	h := fnv.New64a()
	for _, arg := range args {
		switch v := arg.(type) {
		case types.String:
			// We use a separator to avoid collisions between e.g. ("a", "bc") and ("ab", "c").
			fmt.Fprintf(h, "s:%s\x00", string(v))
		case types.Bool:
			fmt.Fprintf(h, "b:%t\x00", bool(v))
		default:
			return "", false
		}
	}
	return strconv.FormatUint(h.Sum64(), 16), true
}

// defaultCacheMaxSize bounds the default cache when CacheSize is not configured, so a long-lived
// program cannot accumulate cache entries without limit.
const defaultCacheMaxSize = 1024

type defaultCacheEntry struct {
	key       string
	val       ref.Val
	expiresAt time.Time
	// referenced is the CLOCK second-chance bit. Readers set it under the read lock (so it must be
	// atomic, as readers run concurrently); eviction clears it under the write lock.
	referenced atomic.Bool
}

// defaultCache is a bounded, TTL-aware cache with CLOCK (second-chance) eviction.
//
// CLOCK is used instead of a strict LRU so that reads do not have to mutate shared ordering state:
// a Get only needs a read lock and an atomic flag set, allowing reads to proceed concurrently.
// Only writes (Set and the eviction it triggers) take the exclusive lock. The configured size is
// still respected exactly; eviction is approximately least-recently-used, and evicting an entry
// only costs a recomputation on its next access, so it is always safe.
type defaultCache struct {
	mu      sync.RWMutex
	items   map[string]*defaultCacheEntry
	ring    []*defaultCacheEntry // CLOCK buffer, grown lazily up to maxSize
	hand    int                  // CLOCK hand into ring
	maxSize int
	ttl     time.Duration
}

func newDefaultCache(maxSize int, ttl time.Duration) Cache {
	if maxSize <= 0 {
		maxSize = defaultCacheMaxSize
	}
	return &defaultCache{
		items:   make(map[string]*defaultCacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (c *defaultCache) expiry() time.Time {
	if c.ttl > 0 {
		return time.Now().Add(c.ttl)
	}
	return time.Time{}
}

func (c *defaultCache) Get(key string) (ref.Val, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		// Leave expired entries unreferenced so they become eviction candidates.
		return nil, false
	}
	e.referenced.Store(true)
	return e.val, true
}

func (c *defaultCache) GetStale(key string) (ref.Val, bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false, false
	}
	e.referenced.Store(true)
	isStale := !e.expiresAt.IsZero() && time.Now().After(e.expiresAt)
	return e.val, isStale, true
}

func (c *defaultCache) Set(key string, val ref.Val) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[key]; ok {
		e.val = val
		e.expiresAt = c.expiry()
		e.referenced.Store(true)
		return
	}
	e := &defaultCacheEntry{key: key, val: val, expiresAt: c.expiry()}
	e.referenced.Store(true)
	if len(c.ring) < c.maxSize {
		c.ring = append(c.ring, e)
		c.items[key] = e
		return
	}
	// Cache is full: advance the CLOCK hand, giving referenced entries a second chance, and reuse
	// the slot of the first unreferenced (or empty) entry.
	for {
		victim := c.ring[c.hand]
		if victim == nil {
			break
		}
		if victim.referenced.Load() {
			victim.referenced.Store(false)
			c.hand = (c.hand + 1) % len(c.ring)
			continue
		}
		delete(c.items, victim.key)
		break
	}
	c.ring[c.hand] = e
	c.items[key] = e
	c.hand = (c.hand + 1) % len(c.ring)
}

func (c *defaultCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return
	}
	delete(c.items, key)
	// Clear the entry's slot; the empty slot is reused by a later Set.
	for i, re := range c.ring {
		if re == e {
			c.ring[i] = nil
			break
		}
	}
}
