// Copyright 2024 Google LLC
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
	config := &asyncCacheConfig{
		keyFunc: defaultKeyFunc,
	}
	for _, opt := range opts {
		opt(config)
	}

	if config.cache == nil {
		config.cache = newDefaultCache(config.maxSize, config.ttl)
	}

	return decls.AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
		key := config.keyFunc(args...)

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
			// Serve stale immediately and refresh in background
			go func() {
				res := fn(ctx, args...)
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

func defaultKeyFunc(args ...ref.Val) string {
	h := fnv.New64a()
	for _, arg := range args {
		// Use the native Go value for hashing.
		// We use a separator to avoid collisions between e.g. ("a", "bc") and ("ab", "c").
		fmt.Fprintf(h, "%v\x00", arg.Value())
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

type defaultCacheEntry struct {
	val       ref.Val
	expiresAt time.Time
}

type defaultCache struct {
	mu      sync.RWMutex
	entries map[string]defaultCacheEntry
	maxSize int
	ttl     time.Duration
}

func newDefaultCache(maxSize int, ttl time.Duration) Cache {
	return &defaultCache{
		entries: make(map[string]defaultCacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (c *defaultCache) Get(key string) (ref.Val, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || (!entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt)) {
		return nil, false
	}
	return entry.val, true
}

func (c *defaultCache) GetStale(key string) (ref.Val, bool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false, false
	}
	isStale := !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt)
	return entry.val, isStale, true
}

func (c *defaultCache) Set(key string, val ref.Val) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.maxSize > 0 && len(c.entries) >= c.maxSize {
		// Simple eviction: clear the map if it grows too large.
		// A real LRU would be better but this is a default.
		c.entries = make(map[string]defaultCacheEntry)
	}

	entry := defaultCacheEntry{val: val}
	if c.ttl > 0 {
		entry.expiresAt = time.Now().Add(c.ttl)
	}
	c.entries[key] = entry
}

func (c *defaultCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
