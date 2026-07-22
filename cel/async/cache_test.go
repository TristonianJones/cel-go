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

package async_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/cel-go/cel/async"
	"github.com/google/cel-go/common/decls"
	"github.com/google/cel-go/common/functions"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// buildCachedAsync constructs a cached async overload binding and returns the channel-based
// AsyncOp so the cache behavior can be exercised directly.
func buildCachedAsync(t *testing.T, fn functions.BlockingAsyncOp, opts ...async.CacheOption) functions.AsyncOp {
	t.Helper()
	fnDecl, err := decls.NewFunction("cached",
		decls.Overload("cached_string", []*types.Type{types.StringType}, types.StringType,
			async.CachedBinding(fn, opts...)),
	)
	if err != nil {
		t.Fatalf("decls.NewFunction() failed: %v", err)
	}
	bindings, err := fnDecl.Bindings()
	if err != nil {
		t.Fatalf("Bindings() failed: %v", err)
	}
	for _, b := range bindings {
		if b.Async != nil {
			return b.Async
		}
	}
	t.Fatal("no async binding produced")
	return nil
}

// buildCachedAsyncTyped is like buildCachedAsync but with a configurable overload signature, so
// the cache's per-argument-type keying behavior can be exercised.
func buildCachedAsyncTyped(t *testing.T, argTypes []*types.Type, resultType *types.Type, fn functions.BlockingAsyncOp, opts ...async.CacheOption) functions.AsyncOp {
	t.Helper()
	fnDecl, err := decls.NewFunction("cached",
		decls.Overload("cached_overload", argTypes, resultType,
			async.CachedBinding(fn, opts...)),
	)
	if err != nil {
		t.Fatalf("decls.NewFunction() failed: %v", err)
	}
	bindings, err := fnDecl.Bindings()
	if err != nil {
		t.Fatalf("Bindings() failed: %v", err)
	}
	for _, b := range bindings {
		if b.Async != nil {
			return b.Async
		}
	}
	t.Fatal("no async binding produced")
	return nil
}

func invoke(t *testing.T, op functions.AsyncOp, args ...ref.Val) ref.Val {
	t.Helper()
	select {
	case res := <-op(context.Background(), args...):
		return res
	case <-time.After(2 * time.Second):
		t.Fatal("async op timed out")
		return nil
	}
}

func TestCachedBindingHit(t *testing.T) {
	var calls atomic.Int32
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		calls.Add(1)
		return args[0].(types.String) + " v"
	})

	// First call computes and caches; second call with identical args is served from cache.
	if got := invoke(t, op, types.String("a")); got.Equal(types.String("a v")) != types.True {
		t.Errorf("first result = %v, want 'a v'", got)
	}
	if got := invoke(t, op, types.String("a")); got.Equal(types.String("a v")) != types.True {
		t.Errorf("cached result = %v, want 'a v'", got)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("underlying fn called %d times, want 1 (second served from cache)", got)
	}

	// Distinct args miss the cache.
	invoke(t, op, types.String("b"))
	if got := calls.Load(); got != 2 {
		t.Errorf("underlying fn called %d times after distinct arg, want 2", got)
	}
}

func TestCachedBindingDoesNotCacheErrors(t *testing.T) {
	var calls atomic.Int32
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		calls.Add(1)
		return types.NewErr("boom")
	})
	invoke(t, op, types.String("a"))
	invoke(t, op, types.String("a"))
	if got := calls.Load(); got != 2 {
		t.Errorf("underlying fn called %d times, want 2 (errors must not be cached)", got)
	}
}

func TestCachedBindingTTLExpiry(t *testing.T) {
	var calls atomic.Int32
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		calls.Add(1)
		return args[0]
	}, async.CacheTTL(20*time.Millisecond))

	invoke(t, op, types.String("a"))
	invoke(t, op, types.String("a"))
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls before expiry = %d, want 1", got)
	}
	time.Sleep(40 * time.Millisecond)
	invoke(t, op, types.String("a"))
	if got := calls.Load(); got != 2 {
		t.Errorf("calls after TTL expiry = %d, want 2", got)
	}
}

func TestCachedBindingStaleWhileError(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		calls.Add(1)
		if fail.Load() {
			return types.NewErr("boom")
		}
		return types.String("fresh")
	}, async.CacheTTL(15*time.Millisecond), async.CacheStaleWhileError(true))

	// Prime the cache with a good value.
	if got := invoke(t, op, types.String("a")); got.Equal(types.String("fresh")) != types.True {
		t.Fatalf("primed result = %v, want 'fresh'", got)
	}
	// Let the entry go stale, then make the function fail.
	time.Sleep(30 * time.Millisecond)
	fail.Store(true)
	if got := invoke(t, op, types.String("a")); got.Equal(types.String("fresh")) != types.True {
		t.Errorf("stale-while-error result = %v, want stale 'fresh'", got)
	}
}

func TestCachedBindingStaleWhileRevalidate(t *testing.T) {
	var version atomic.Int32
	refreshed := make(chan struct{}, 1)
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		v := version.Add(1)
		if v > 1 {
			select {
			case refreshed <- struct{}{}:
			default:
			}
		}
		return types.Int(int64(v))
	}, async.CacheTTL(15*time.Millisecond), async.CacheStaleWhileRevalidate(true))

	if got := invoke(t, op, types.String("a")); got.Equal(types.Int(1)) != types.True {
		t.Fatalf("primed result = %v, want 1", got)
	}
	time.Sleep(30 * time.Millisecond)
	// Stale read should return the old value immediately while refreshing in the background.
	if got := invoke(t, op, types.String("a")); got.Equal(types.Int(1)) != types.True {
		t.Errorf("stale-while-revalidate result = %v, want stale 1", got)
	}
	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("background revalidation did not run")
	}
}

func TestCachedBindingCustomKeyFunc(t *testing.T) {
	var calls atomic.Int32
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		calls.Add(1)
		return args[0]
	}, async.CacheKeyFunc(func(args ...ref.Val) string {
		return "constant" // collapse all args to a single key
	}))
	invoke(t, op, types.String("a"))
	invoke(t, op, types.String("b"))
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (custom key collapses all args)", got)
	}
}

// recordingCache is a minimal custom Cache used to verify CustomCache wiring.
type recordingCache struct {
	mu      sync.Mutex
	entries map[string]ref.Val
	sets    atomic.Int32
}

func newRecordingCache() *recordingCache {
	return &recordingCache{entries: make(map[string]ref.Val)}
}

func (c *recordingCache) Get(key string) (ref.Val, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *recordingCache) Set(key string, val ref.Val) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = val
	c.sets.Add(1)
}

func (c *recordingCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func TestCachedBindingCustomCache(t *testing.T) {
	cache := newRecordingCache()
	var calls atomic.Int32
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		calls.Add(1)
		return args[0]
	}, async.CustomCache(cache))

	invoke(t, op, types.String("a"))
	invoke(t, op, types.String("a"))
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (custom cache should serve hit)", got)
	}
	if got := cache.sets.Load(); got != 1 {
		t.Errorf("custom cache Set called %d times, want 1", got)
	}
}

func TestCachedBindingSkipsNumericArgs(t *testing.T) {
	// Regression: numeric arguments use a complex notion of equivalence, so the default cache
	// must not key on them. It must neither cache repeats nor return a value computed for a
	// different numeric argument.
	var calls atomic.Int32
	op := buildCachedAsyncTyped(t, []*types.Type{types.IntType}, types.IntType,
		func(ctx context.Context, args ...ref.Val) ref.Val {
			calls.Add(1)
			return args[0].(types.Int) * 2
		})

	if got := invoke(t, op, types.Int(1)); got.Equal(types.Int(2)) != types.True {
		t.Errorf("first result = %v, want 2", got)
	}
	if got := invoke(t, op, types.Int(1)); got.Equal(types.Int(2)) != types.True {
		t.Errorf("repeat result = %v, want 2", got)
	}
	// No caching: each call recomputes.
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 (numeric args must not be cached)", got)
	}
	// A different numeric argument must return its own freshly computed value, never a stale hit.
	if got := invoke(t, op, types.Int(5)); got.Equal(types.Int(10)) != types.True {
		t.Errorf("distinct-arg result = %v, want 10", got)
	}
}

func TestCachedBindingCachesBoolArgs(t *testing.T) {
	var calls atomic.Int32
	op := buildCachedAsyncTyped(t, []*types.Type{types.BoolType}, types.IntType,
		func(ctx context.Context, args ...ref.Val) ref.Val {
			calls.Add(1)
			return types.Int(1)
		})
	invoke(t, op, types.Bool(true))
	invoke(t, op, types.Bool(true))
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (bool args are cacheable)", got)
	}
	// A distinct bool key is computed independently.
	invoke(t, op, types.Bool(false))
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 after distinct bool arg", got)
	}
}

func TestCachedBindingMixedArgsNotCached(t *testing.T) {
	var calls atomic.Int32
	op := buildCachedAsyncTyped(t, []*types.Type{types.StringType, types.IntType}, types.StringType,
		func(ctx context.Context, args ...ref.Val) ref.Val {
			calls.Add(1)
			return args[0]
		})
	invoke(t, op, types.String("a"), types.Int(1))
	invoke(t, op, types.String("a"), types.Int(1))
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 (a non-string/bool arg makes the call uncacheable)", got)
	}
}

func TestCachedBindingNumericArgsWithCustomKey(t *testing.T) {
	// A user-supplied key function opts numeric arguments back into caching.
	var calls atomic.Int32
	op := buildCachedAsyncTyped(t, []*types.Type{types.IntType}, types.IntType,
		func(ctx context.Context, args ...ref.Val) ref.Val {
			calls.Add(1)
			return args[0]
		}, async.CacheKeyFunc(func(args ...ref.Val) string {
			return fmt.Sprintf("%v", args[0].Value())
		}))
	invoke(t, op, types.Int(3))
	invoke(t, op, types.Int(3))
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (custom key enables numeric caching)", got)
	}
}

func TestCachedBindingLRUEviction(t *testing.T) {
	var calls atomic.Int32
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		calls.Add(1)
		return args[0]
	}, async.CacheSize(1))

	invoke(t, op, types.String("a")) // miss -> compute (calls=1)
	invoke(t, op, types.String("a")) // hit
	if got := calls.Load(); got != 1 {
		t.Fatalf("calls after repeated 'a' = %d, want 1", got)
	}
	invoke(t, op, types.String("b")) // miss -> compute (calls=2), evicts 'a' (size 1)
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls after 'b' = %d, want 2", got)
	}
	invoke(t, op, types.String("a")) // 'a' was evicted -> recompute (calls=3)
	if got := calls.Load(); got != 3 {
		t.Errorf("calls after evicted 'a' = %d, want 3 (LRU eviction)", got)
	}
}

func TestCachedBindingStaleWhileRevalidateDetachedContext(t *testing.T) {
	var version atomic.Int32
	refreshed := make(chan struct{}, 1)
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		// Respect cancellation: if the refresh ran on the (cancelled) request context, this would
		// fail and the cache would never be updated.
		if err := ctx.Err(); err != nil {
			return types.NewErr("cancelled: %v", err)
		}
		v := version.Add(1)
		if v > 1 {
			select {
			case refreshed <- struct{}{}:
			default:
			}
		}
		return types.String(fmt.Sprintf("v%d", v))
	}, async.CacheTTL(15*time.Millisecond), async.CacheStaleWhileRevalidate(true))

	// Prime the cache.
	if got := invoke(t, op, types.String("k")); got.Equal(types.String("v1")) != types.True {
		t.Fatalf("primed result = %v, want v1", got)
	}
	// Let the entry go stale.
	time.Sleep(30 * time.Millisecond)

	// Trigger the stale read with an already-cancelled request context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	select {
	case got := <-op(ctx, types.String("k")):
		if got.Equal(types.String("v1")) != types.True {
			t.Errorf("stale read = %v, want stale v1", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stale read timed out")
	}

	// The background refresh must complete despite the cancelled request context.
	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("background revalidation did not run on a detached context")
	}
}

func TestCachedBindingConcurrentReads(t *testing.T) {
	// Concurrent reads must be data-race free (the CLOCK referenced bit is set under the read lock
	// from many goroutines) and must all hit the cache. Run under -race to validate.
	var calls atomic.Int32
	op := buildCachedAsync(t, func(ctx context.Context, args ...ref.Val) ref.Val {
		calls.Add(1)
		return args[0]
	})
	// Prime the entry.
	<-op(context.Background(), types.String("k"))

	var wg sync.WaitGroup
	var mismatches atomic.Int32
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				res := <-op(context.Background(), types.String("k"))
				if res.Equal(types.String("k")) != types.True {
					mismatches.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	if got := mismatches.Load(); got != 0 {
		t.Errorf("%d concurrent reads returned an unexpected value", got)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("underlying fn called %d times, want 1 (all reads should hit the cache)", got)
	}
}
