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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/cel/async"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

type mockAsyncCall struct {
	id       int64
	function string
	overload string
}

func (m mockAsyncCall) CallID() int64    { return m.id }
func (m mockAsyncCall) Function() string { return m.function }
func (m mockAsyncCall) Overload() string { return m.overload }

func TestAsyncCallMethods(t *testing.T) {
	m := mockAsyncCall{id: 1, function: "f", overload: "o"}
	if m.CallID() != 1 {
		t.Errorf("got %d, want 1", m.CallID())
	}
	if m.Function() != "f" {
		t.Errorf("got %s, want f", m.Function())
	}
	if m.Overload() != "o" {
		t.Errorf("got %s, want o", m.Overload())
	}
}

func TestDrainNone(t *testing.T) {
	s := async.DrainNone()
	// No completions -> no re-evaluation
	if s.NextAction(nil, 1).Reevaluate {
		t.Error("DrainNone re-evaluated with nil batch")
	}
	// One completion -> re-evaluate
	if !s.NextAction([]async.Call{mockAsyncCall{}}, 1).Reevaluate {
		t.Error("DrainNone did not re-evaluate with 1 completion")
	}
}

func TestDrainAll(t *testing.T) {
	s := async.DrainAll()
	// Pending calls remain -> no re-evaluation
	if s.NextAction([]async.Call{mockAsyncCall{}}, 1).Reevaluate {
		t.Error("DrainAll re-evaluated while calls are pending")
	}
	// No pending calls -> re-evaluate
	if !s.NextAction([]async.Call{mockAsyncCall{}}, 0).Reevaluate {
		t.Error("DrainAll did not re-evaluate when no calls pending")
	}
}

func TestDrainReady(t *testing.T) {
	debounce := 10 * time.Millisecond
	s := async.DrainReady(debounce)

	// No pending calls -> re-evaluate immediately
	action := s.NextAction([]async.Call{mockAsyncCall{}}, 0)
	if !action.Reevaluate {
		t.Error("DrainReady did not re-evaluate when no calls pending")
	}

	// No completions -> wait indefinitely
	action = s.NextAction(nil, 1)
	if action.Reevaluate || action.WaitDuration != 0 {
		t.Errorf("DrainReady NextAction(nil, 1) = %v, want {false, 0}", action)
	}

	action = s.NextAction([]async.Call{mockAsyncCall{}}, 1)
	if action.Reevaluate || action.WaitDuration != debounce {
		t.Errorf("DrainReady NextAction(batch, 1) = %v, want {false, %v}", action, debounce)
	}
}

func TestTimeoutBinding(t *testing.T) {
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		select {
		case <-time.After(100 * time.Millisecond):
			return types.Int(1)
		case <-ctx.Done():
			return types.NewErr("timeout")
		}
	}

	// Set a 50ms timeout, which is shorter than the 100ms delay
	env, _ := cel.NewEnv(
		cel.Function("timed",
			cel.Overload("timed_int", []*cel.Type{}, cel.IntType,
				async.TimeoutBinding(fn, 50*time.Millisecond)),
		),
	)
	ast, _ := env.Compile(`timed()`)
	prg, _ := env.Program(ast)

	ctx := context.Background()
	res := <-prg.ConcurrentEval(ctx, cel.NoVars())
	if res.Err == nil {
		t.Errorf("got nil error, want timeout")
	}
}

type mockRetryableError struct {
	error
	retryable bool
}

func (m mockRetryableError) IsRetryable() bool { return m.retryable }

func TestRetryBinding(t *testing.T) {
	var callCount atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		count := callCount.Add(1)
		if count < 3 {
			return types.WrapErr(mockRetryableError{retryable: true})
		}
		return types.Int(count)
	}

	env, _ := cel.NewEnv(
		cel.Function("retryable",
			cel.Overload("retryable_int", []*cel.Type{}, cel.IntType,
				async.RetryBinding(fn, async.RetryAttempts(3), async.RetryBackoff(10*time.Millisecond))),
		),
	)
	ast, _ := env.Compile(`retryable()`)
	prg, _ := env.Program(ast)

	ctx := context.Background()
	res := <-prg.ConcurrentEval(ctx, cel.NoVars())
	if res.Err != nil {
		t.Fatalf("ConcurrentEval failed: %v", res.Err)
	}
	if res.Val.(types.Int) != 3 {
		t.Errorf("got %v, want 3", res.Val)
	}
	if callCount.Load() != 3 {
		t.Errorf("callCount = %d, want 3", callCount.Load())
	}
}

type mockObserver struct {
	started  atomic.Int32
	finished atomic.Int32
}

func (m *mockObserver) OnCallStarted(id int64, function, overload string, args []ref.Val) {
	m.started.Add(1)
}

func (m *mockObserver) OnCallFinished(id int64, function, overload string, res ref.Val) {
	m.finished.Add(1)
}

func TestObserver(t *testing.T) {
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		return args[0]
	}

	observer := &mockObserver{}
	env, _ := cel.NewEnv(
		cel.Function("observed",
			cel.Overload("observed_int", []*cel.Type{cel.IntType}, cel.IntType,
				cel.AsyncBinding(fn)),
		),
	)
	ast, _ := env.Compile(`observed(1) == 1`)
	prg, _ := env.Program(ast, cel.AsyncCallObserver(observer))

	ctx := context.Background()
	<-prg.ConcurrentEval(ctx, cel.NoVars())

	if observer.started.Load() != 1 {
		t.Errorf("started = %d, want 1", observer.started.Load())
	}
	if observer.finished.Load() != 1 {
		t.Errorf("finished = %d, want 1", observer.finished.Load())
	}
}

func TestLimiter(t *testing.T) {
	limiter := async.NewLimiter(2)
	var active atomic.Int32
	var maxActive atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		curr := active.Add(1)
		for {
			old := maxActive.Load()
			if curr <= old || maxActive.CompareAndSwap(old, curr) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		active.Add(-1)
		return types.Int(1)
	}

	env, _ := cel.NewEnv(
		cel.Function("limited",
			cel.Overload("limited_int", []*cel.Type{cel.IntType}, cel.IntType,
				cel.AsyncBinding(limiter.Limit(fn))),
		),
	)
	ast, _ := env.Compile(`limited(1) + limited(2) + limited(3)`)
	prg, _ := env.Program(ast)

	<-prg.ConcurrentEval(context.Background(), cel.NoVars())

	if maxActive.Load() > 2 {
		t.Errorf("maxActive = %d, want <= 2", maxActive.Load())
	}
}

func TestAsyncMaxConcurrency(t *testing.T) {
	var active atomic.Int32
	var maxActive atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		curr := active.Add(1)
		for {
			old := maxActive.Load()
			if curr <= old || maxActive.CompareAndSwap(old, curr) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		active.Add(-1)
		return types.Int(1)
	}

	env, _ := cel.NewEnv(
		cel.Function("limited",
			cel.Overload("limited_int", []*cel.Type{cel.IntType}, cel.IntType,
				cel.AsyncBinding(fn)),
		),
	)
	ast, _ := env.Compile(`limited(1) + limited(2) + limited(3)`)
	// Limit to 1 at the program level
	prg, _ := env.Program(ast, cel.AsyncMaxConcurrency(1))

	<-prg.ConcurrentEval(context.Background(), cel.NoVars())

	if maxActive.Load() > 1 {
		t.Errorf("maxActive = %d, want 1", maxActive.Load())
	}
}

func TestTimeoutBinding_Cancellation(t *testing.T) {
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		time.Sleep(100 * time.Millisecond)
		return types.Int(1)
	}

	env, _ := cel.NewEnv(
		cel.Function("timeout",
			cel.Overload("timeout_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.TimeoutBinding(fn, 200*time.Millisecond)),
		),
	)
	ast, _ := env.Compile(`timeout(1)`)
	prg, _ := env.Program(ast)

	ctx, cancel := context.WithCancel(context.Background())
	resCh := prg.ConcurrentEval(ctx, cel.NoVars())
	
	// Cancel immediately
	cancel()
	
	res := <-resCh
	if res.Err == nil || (!strings.Contains(res.Err.Error(), "canceled") && !strings.Contains(res.Err.Error(), "cancelled")) {
		t.Errorf("expected cancellation error, got %v", res.Err)
	}
}

func TestRetryBinding_NonRetryable(t *testing.T) {
	var callCount atomic.Int32
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		callCount.Add(1)
		return types.NewErr("fatal: %w", nonRetryableError{fmt.Errorf("fatal")})
	}

	env, _ := cel.NewEnv(
		cel.Function("retry",
			cel.Overload("retry_int", []*cel.Type{cel.IntType}, cel.IntType,
				async.RetryBinding(fn, async.RetryAttempts(3))),
		),
	)
	ast, _ := env.Compile(`retry(1)`)
	prg, _ := env.Program(ast)

	res := <-prg.ConcurrentEval(context.Background(), cel.NoVars())
	if res.Err == nil || !strings.Contains(res.Err.Error(), "fatal") {
		t.Errorf("expected error, got %v", res.Err)
	}

	if callCount.Load() != 1 {
		t.Errorf("callCount = %d, want 1 (should not retry non-retryable error)", callCount.Load())
	}
}

type nonRetryableError struct{ error }
func (e nonRetryableError) IsRetryable() bool { return false }

func TestLimiter_Cancel(t *testing.T) {
	l := async.NewLimiter(1)
	ctx, cancel := context.WithCancel(context.Background())
	
	// Occupy the only slot
	fn1 := l.Limit(func(ctx context.Context, args ...ref.Val) ref.Val {
		<-ctx.Done()
		return types.Int(1)
	})
	go fn1(context.Background())
	
	// Wait a bit to ensure fn1 is running
	time.Sleep(10 * time.Millisecond)
	
	// This call should block and then be cancelled
	fn2 := l.Limit(func(ctx context.Context, args ...ref.Val) ref.Val {
		return types.Int(2)
	})
	
	cancel()
	res := fn2(ctx)
	if !types.IsError(res) || (!strings.Contains(res.(*types.Err).Error(), "canceled") && !strings.Contains(res.(*types.Err).Error(), "cancelled")) {
		t.Errorf("expected cancellation error, got %v", res)
	}
}

func TestRetryBinding_Cancel(t *testing.T) {
	fn := func(ctx context.Context, args ...ref.Val) ref.Val {
		return types.WrapErr(mockRetryableError{retryable: true})
	}
	
	binding := async.RetryBinding(fn, async.RetryAttempts(3), async.RetryBackoff(100*time.Millisecond))
	
	env, _ := cel.NewEnv(
		cel.Function("retry",
			cel.Overload("retry_int", []*cel.Type{}, cel.IntType,
				binding),
		),
	)
	ast, _ := env.Compile(`retry()`)
	prg, _ := env.Program(ast)
	
	ctx, cancel := context.WithCancel(context.Background())
	resCh := prg.ConcurrentEval(ctx, cel.NoVars())
	
	// Wait for first attempt to fail and enter backoff
	time.Sleep(50 * time.Millisecond)
	cancel()
	
	res := <-resCh
	if res.Err == nil || (!strings.Contains(res.Err.Error(), "canceled") && !strings.Contains(res.Err.Error(), "cancelled")) {
		t.Errorf("expected cancellation error, got %v", res.Err)
	}
}
