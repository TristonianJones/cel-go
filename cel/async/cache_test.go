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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/cel/async"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func TestCachedBinding(t *testing.T) {
	var callCount atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		callCount.Add(1)
		return args[0]
	}

	env, err := cel.NewEnv(
		cel.Function("cached",
			cel.Overload("cached_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.CachedBinding(fn)),
		),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	ast, _ := env.Compile(`cached(1) == 1`)
	prg, _ := env.Program(ast)

	ctx := context.Background()

	// First evaluation -> cache miss
	res1 := <-prg.ConcurrentEval(ctx, cel.NoVars())
	if res1.Err != nil {
		t.Fatalf("ConcurrentEval(1) failed: %v", res1.Err)
	}
	if callCount.Load() != 1 {
		t.Errorf("callCount = %d, want 1", callCount.Load())
	}

	// Second evaluation -> cache hit
	res2 := <-prg.ConcurrentEval(ctx, cel.NoVars())
	if res2.Err != nil {
		t.Fatalf("ConcurrentEval(2) failed: %v", res2.Err)
	}
	if callCount.Load() != 1 {
		t.Errorf("callCount = %d, want 1 (cache hit)", callCount.Load())
	}

	// Third evaluation with different arg -> cache miss
	ast2, _ := env.Compile(`cached(2) == 2`)
	prg2, _ := env.Program(ast2)
	res3 := <-prg2.ConcurrentEval(ctx, cel.NoVars())
	if res3.Err != nil {
		t.Fatalf("ConcurrentEval(3) failed: %v", res3.Err)
	}
	if callCount.Load() != 2 {
		t.Errorf("callCount = %d, want 2", callCount.Load())
	}
}

func TestCachedBinding_TTL(t *testing.T) {
	var callCount atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		callCount.Add(1)
		return args[0]
	}

	// Cache with 50ms TTL
	env, _ := cel.NewEnv(
		cel.Function("cached",
			cel.Overload("cached_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.CachedBinding(fn, async.CacheTTL(50*time.Millisecond))),
		),
	)
	ast, _ := env.Compile(`cached(1) == 1`)
	prg, _ := env.Program(ast)

	ctx := context.Background()

	// First call
	<-prg.ConcurrentEval(ctx, cel.NoVars())
	if callCount.Load() != 1 {
		t.Errorf("callCount = %d, want 1", callCount.Load())
	}

	// Immediate second call -> hit
	<-prg.ConcurrentEval(ctx, cel.NoVars())
	if callCount.Load() != 1 {
		t.Errorf("callCount = %d, want 1", callCount.Load())
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Third call -> miss (expired)
	<-prg.ConcurrentEval(ctx, cel.NoVars())
	if callCount.Load() != 2 {
		t.Errorf("callCount = %d, want 2", callCount.Load())
	}
}

func TestCachedBinding_CustomKey(t *testing.T) {
	var callCount atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		callCount.Add(1)
		return args[0]
	}

	// Custom key function that ignores arguments
	env, _ := cel.NewEnv(
		cel.Function("cached",
			cel.Overload("cached_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.CachedBinding(fn, async.CacheKeyFunc(func(args ...ref.Val) string {
					return "constant"
				}))),
		),
	)

	ctx := context.Background()

	// Call with arg 1
	ast1, _ := env.Compile(`cached(1) == 1`)
	prg1, _ := env.Program(ast1)
	<-prg1.ConcurrentEval(ctx, cel.NoVars())

	// Call with arg 2 -> should HIT because key is constant
	ast2, _ := env.Compile(`cached(2) == 2`)
	prg2, _ := env.Program(ast2)
	<-prg2.ConcurrentEval(ctx, cel.NoVars())

	if callCount.Load() != 1 {
		t.Errorf("callCount = %d, want 1 (custom key collapsed args)", callCount.Load())
	}
}

func TestCachedBinding_StaleWhileError(t *testing.T) {
	var callCount atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		count := callCount.Add(1)
		if count == 1 {
			return types.Int(1)
		}
		return types.NewErr("transient failure")
	}

	// Cache with 50ms TTL and stale-while-error enabled
	env, _ := cel.NewEnv(
		cel.Function("cached",
			cel.Overload("cached_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.CachedBinding(fn, async.CacheTTL(50*time.Millisecond), async.CacheStaleWhileError(true))),
		),
	)
	ast, _ := env.Compile(`cached(1) == 1`)
	prg, _ := env.Program(ast)

	ctx := context.Background()

	// First call -> Success, stored in cache
	res1 := <-prg.ConcurrentEval(ctx, cel.NoVars())
	if res1.Err != nil {
		t.Fatalf("first call failed: %v", res1.Err)
	}

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Second call -> Underlying fn fails, but should serve stale entry
	res2 := <-prg.ConcurrentEval(ctx, cel.NoVars())
	if res2.Err != nil {
		t.Fatalf("second call should have served stale value, but got error: %v", res2.Err)
	}
	if res2.Val != types.True {
		t.Errorf("got %v, want true", res2.Val)
	}
}

func TestCachedBinding_StaleWhileRevalidate(t *testing.T) {
	var callCount atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		count := callCount.Add(1)
		return types.Int(count)
	}

	// Cache with 50ms TTL and stale-while-revalidate enabled
	env, _ := cel.NewEnv(
		cel.Function("cached",
			cel.Overload("cached_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.CachedBinding(fn, async.CacheTTL(50*time.Millisecond), async.CacheStaleWhileRevalidate(true))),
		),
	)
	ast, _ := env.Compile(`cached(1)`)
	prg, _ := env.Program(ast)

	ctx := context.Background()

	// First call -> Success, val=1
	<-prg.ConcurrentEval(ctx, cel.NoVars())

	// Wait for TTL to expire
	time.Sleep(100 * time.Millisecond)

	// Second call -> Should serve stale val=1 immediately and refresh in background
	res2 := <-prg.ConcurrentEval(ctx, cel.NoVars())
	if res2.Val.(types.Int) != 1 {
		t.Errorf("got %v, want 1 (stale)", res2.Val)
	}

	// Wait for background refresh to finish
	time.Sleep(50 * time.Millisecond)

	// Third call -> Should serve new val=2
	res3 := <-prg.ConcurrentEval(ctx, cel.NoVars())
	if res3.Val.(types.Int) != 2 {
		t.Errorf("got %v, want 2 (refreshed)", res3.Val)
	}
}

func TestCacheSize(t *testing.T) {
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		return args[0]
	}

	// Cache with size 1
	env, _ := cel.NewEnv(
		cel.Function("cached",
			cel.Overload("cached_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.CachedBinding(fn, async.CacheSize(1))),
		),
	)
	ast, _ := env.Compile(`cached(1) + cached(2) + cached(1)`)
	prg, _ := env.Program(ast)

	<-prg.ConcurrentEval(context.Background(), cel.NoVars())
}

type customCache struct {
	data map[string]ref.Val
}

func (c *customCache) Get(key string) (ref.Val, bool) { val, ok := c.data[key]; return val, ok }
func (c *customCache) Set(key string, val ref.Val)    { c.data[key] = val }
func (c *customCache) Delete(key string)              { delete(c.data, key) }

func TestCustomCache(t *testing.T) {
	cache := &customCache{data: make(map[string]ref.Val)}
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		return args[0]
	}

	env, _ := cel.NewEnv(
		cel.Function("cached",
			cel.Overload("cached_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.CachedBinding(fn, async.CustomCache(cache))),
		),
	)
	ast, _ := env.Compile(`cached(1)`)
	prg, _ := env.Program(ast)

	<-prg.ConcurrentEval(context.Background(), cel.NoVars())

	// We can't easily compute the key here as defaultKeyFunc is private in async package.
	// But we can check that the cache is not empty.
	if len(cache.data) == 0 {
		t.Errorf("cache is empty")
	}
}

func TestDefaultCache_Misc(t *testing.T) {
	// defaultCache is used when no CustomCache is provided.
	var callCount atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		callCount.Add(1)
		return args[0]
	}

	env, _ := cel.NewEnv(
		cel.Function("cached",
			cel.Overload("cached_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.CachedBinding(fn)),
		),
	)
	ast, _ := env.Compile(`cached(1)`)
	prg, _ := env.Program(ast)
	ctx := context.Background()

	// Fill cache
	<-prg.ConcurrentEval(ctx, cel.NoVars())
}
