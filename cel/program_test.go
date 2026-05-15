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

package cel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/cel-go/cel/async"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
)

func TestConcurrentEval(t *testing.T) {
	tests := []struct {
		name    string
		envOpts []EnvOption
		expr    string
		prgOpts []ProgramOption
		in      any
		out     ref.Val
	}{
		{
			name:    "basic_addition",
			envOpts: []EnvOption{Variable("x", IntType)},
			expr:    `x + 1`,
			in:      map[string]any{"x": 10},
			out:     types.Int(11),
		},
		{
			name: "async_function",
			envOpts: []EnvOption{
				Function("async_func",
					Overload("async_func_int", []*Type{IntType}, IntType,
						AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
							time.Sleep(10 * time.Millisecond)
							return args[0]
						}),
					),
				),
			},
			expr: `async_func(42) + 1`,
			in:   map[string]any{},
			out:  types.Int(43),
		},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			env, err := NewEnv(tc.envOpts...)
			if err != nil {
				t.Fatalf("NewEnv() failed: %v", err)
			}
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Compile() failed: %v", iss.Err())
			}
			prg, err := env.Program(ast, tc.prgOpts...)
			if err != nil {
				t.Fatalf("env.Program() failed: %v", err)
			}
			resCh := prg.ConcurrentEval(context.Background(), tc.in)
			select {
			case res := <-resCh:
				if res.Err != nil {
					t.Fatalf("ConcurrentEval() returned error: %v", res.Err)
				}
				if res.Val.Equal(tc.out) != types.True {
					t.Errorf("ConcurrentEval() = %v, want %v", res.Val, tc.out)
				}
			case <-time.After(time.Second):
				t.Fatal("ConcurrentEval() timed out")
			}
		})
	}
}

func TestConcurrentEval_Unknowns(t *testing.T) {
	tests := []struct {
		name    string
		envOpts []EnvOption
		expr    string
		prgOpts []ProgramOption
		in      any
	}{
		{
			name:    "partial_variable",
			envOpts: []EnvOption{Variable("x", IntType)},
			expr:    `x + 1`,
			prgOpts: []ProgramOption{EvalOptions(OptPartialEval)},
			in: func() any {
				pvars, _ := PartialVars(map[string]any{}, AttributePattern("x"))
				return pvars
			}(),
		},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			env, err := NewEnv(tc.envOpts...)
			if err != nil {
				t.Fatalf("NewEnv() failed: %v", err)
			}
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Compile() failed: %v", iss.Err())
			}
			prg, err := env.Program(ast, tc.prgOpts...)
			if err != nil {
				t.Fatalf("env.Program() failed: %v", err)
			}
			resCh := prg.ConcurrentEval(context.Background(), tc.in)
			select {
			case res := <-resCh:
				if res.Err != nil {
					t.Fatalf("ConcurrentEval() returned error: %v", res.Err)
				}
				if !types.IsUnknown(res.Val) {
					t.Errorf("ConcurrentEval() = %v, want Unknown", res.Val)
				}
			case <-time.After(time.Second):
				t.Fatal("ConcurrentEval() timed out")
			}
		})
	}
}

func TestConcurrentEval_Cancel(t *testing.T) {
	env, err := NewEnv(
		Function("long_func",
			Overload("long_func", []*Type{}, IntType,
				AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
					<-ctx.Done()
					return types.NewErr("cancelled")
				}),
			),
		),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}

	ast, iss := env.Compile(`long_func()`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}

	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	resCh := prg.ConcurrentEval(ctx, NoVars())

	// Cancel the context immediately
	cancel()

	select {
	case res := <-resCh:
		if res.Err == nil || (res.Err.Error() != "context canceled" && res.Err.Error() != "context deadline exceeded") {
			t.Errorf("ConcurrentEval() expected context canceled error, got: %v", res.Err)
		}
	case <-time.After(time.Second):
		t.Fatal("ConcurrentEval() timed out")
	}
}

func TestConcurrentEval_NilContext(t *testing.T) {
	env, _ := NewEnv()
	ast, _ := env.Compile(`1 + 1`)
	prg, _ := env.Program(ast)

	resCh := prg.ConcurrentEval(nil, NoVars())
	res := <-resCh
	if res.Err == nil || res.Err.Error() != "context can not be nil" {
		t.Errorf("ConcurrentEval(nil) expected error, got %v", res.Err)
	}

	// Test PendingAsyncCalls and AsyncCall on a nil/empty frame
	emptyFrame := interpreter.NewExecutionFrame(NoVars())
	if emptyFrame.PendingAsyncCalls() != 0 {
		t.Errorf("PendingAsyncCalls() on empty frame = %d, want 0", emptyFrame.PendingAsyncCalls())
	}
	if emptyFrame.AsyncCall(1) != nil {
		t.Errorf("AsyncCall() on empty frame != nil")
	}
}

func TestConcurrentEval_Observable(t *testing.T) {
	asyncIdentityBinding := AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
		time.Sleep(10 * time.Millisecond)
		return args[0]
	})
	asyncDoubleBinding := AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
		time.Sleep(10 * time.Millisecond)
		v := args[0].(types.Int)
		return v * 2
	})

	tests := []struct {
		name    string
		expr    string
		envOpts []EnvOption
		prgOpts []ProgramOption
		in      any
		out     ref.Val
	}{
		{
			name: "logical_or",
			expr: `a || b == "b"`,
			envOpts: []EnvOption{
				Variable("a", BoolType),
				Variable("b", StringType),
			},
			in: map[string]any{
				"a": true,
				"b": "b",
			},
			out: types.True,
		},
		{
			name: "conditional",
			expr: `a ? b < 1.0 : c == ['hello']`,
			envOpts: []EnvOption{
				Variable("a", BoolType),
				Variable("b", DoubleType),
				Variable("c", ListType(StringType)),
			},
			in: map[string]any{
				"a": true,
				"b": 0.999,
				"c": []string{"hello"},
			},
			out: types.True,
		},
		{
			name: "exhaustive_eval",
			expr: `{k: true}[k] || v != false`,
			envOpts: []EnvOption{
				Variable("k", StringType),
				Variable("v", BoolType),
			},
			in: map[string]any{
				"k": "key",
				"v": true,
			},
			out: types.True,
		},
		{
			name: "exhaustive_async_only",
			expr: `async_id(1) + async_dbl(3) == 7`,
			envOpts: []EnvOption{
				Function("async_id",
					Overload("async_identity_int", []*Type{IntType}, IntType, asyncIdentityBinding),
				),
				Function("async_dbl",
					Overload("async_double_int", []*Type{IntType}, IntType, asyncDoubleBinding),
				),
			},
			in:  map[string]any{},
			out: types.True,
		},
		{
			name: "exhaustive_mixed_sync_async",
			expr: `async_id(5) + negate(3) == 2`,
			envOpts: []EnvOption{
				Function("async_id",
					Overload("async_identity_int", []*Type{IntType}, IntType, asyncIdentityBinding),
				),
				Function("negate",
					Overload("sync_negate_int", []*Type{IntType}, IntType,
						UnaryBinding(func(v ref.Val) ref.Val {
							return -(v.(types.Int))
						}),
					),
				),
			},
			in:  map[string]any{},
			out: types.True,
		},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			env, err := NewEnv(tc.envOpts...)
			if err != nil {
				t.Fatalf("NewEnv() failed: %v", err)
			}
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Compile() failed: %v", iss.Err())
			}

			prgOpts := append([]ProgramOption{EvalOptions(OptExhaustiveEval)}, tc.prgOpts...)
			prg, err := env.Program(ast, prgOpts...)
			if err != nil {
				t.Fatalf("env.Program() failed: %v", err)
			}

			resCh := prg.ConcurrentEval(context.Background(), tc.in)

			select {
			case res := <-resCh:
				if res.Err != nil {
					t.Fatalf("ConcurrentEval() returned error: %v", res.Err)
				}
				if res.Val.Equal(tc.out) != types.True {
					t.Errorf("ConcurrentEval() = %v, want %v", res.Val, tc.out)
				}
				if res.EvalDetails == nil {
					t.Fatal("ConcurrentEval() did not return EvalDetails")
				}
				s := res.EvalDetails.State()
				if s == nil {
					t.Fatal("EvalDetails.State() returned nil")
				}
				if len(s.IDs()) == 0 {
					t.Error("EvalState should contain tracked values, but was empty")
				}
			case <-time.After(time.Second):
				t.Fatal("ConcurrentEval() timed out")
			}
		})
	}
}

func TestConcurrentEval_ObservableUnknowns(t *testing.T) {
	ch := make(chan ref.Val, 1)
	defer close(ch)
	asyncIdentityBinding := AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
		select {
		case v := <-ch:
			return v
		case <-ctx.Done():
			return types.NewErr(ctx.Err().Error())
		}
	})

	env, err := NewEnv(
		Variable("x", IntType),
		Variable("y", IntType),
		Function("async_id",
			Overload("async_identity_int", []*Type{IntType}, IntType, asyncIdentityBinding),
		),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}

	ast, iss := env.Compile(`async_id(x) + y == 11`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}
	prg, err := env.Program(ast, EvalOptions(OptExhaustiveEval|OptPartialEval))
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}

	pvars, err := PartialVars(
		map[string]any{"x": 1},
		AttributePattern("y"),
	)
	if err != nil {
		t.Fatalf("PartialVars() failed: %v", err)
	}
	resCh := prg.ConcurrentEval(context.Background(), pvars)
	ch <- types.Int(10)

	select {
	case res := <-resCh:
		if res.Err != nil {
			t.Fatalf("ConcurrentEval() returned error: %v", res.Err)
		}
		if !types.IsUnknown(res.Val) {
			t.Errorf("ConcurrentEval() = %v, want Unknown", res.Val)
		}
		unk := res.Val.(*types.Unknown)
		for _, id := range unk.IDs() {
			trails, found := unk.GetAttributeTrails(id)
			if !found {
				t.Fatalf("unk.GetAttributeTrails(id) failed for unknown id: %d", id)
			}
			if len(trails) == 1 && trails[0].Variable() == "y" {
				goto found
			}
		}
		t.Errorf("Unknown value does not contain y")
	found:
		if res.EvalDetails == nil {
			t.Fatal("ConcurrentEval() did not return EvalDetails")
		}
		s := res.EvalDetails.State()
		if s == nil {
			t.Fatal("EvalDetails.State() returned nil")
		}
		if len(s.IDs()) == 0 {
			t.Error("EvalState should contain tracked values, but was empty")
		}
	case <-time.After(time.Second):
		t.Fatal("ConcurrentEval() timed out")
	}
}

func TestConcurrentEval_DrainStrategies(t *testing.T) {
	// Create channels to precisely control when the async functions return
	f1Ch := make(chan struct{})
	f2Ch := make(chan struct{})
	f3Ch := make(chan struct{})

	env, err := NewEnv(
		Function("async_f1", Overload("async_f1_int", []*Type{}, IntType, AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
			<-f1Ch
			return types.Int(1)
		}))),
		Function("async_f2", Overload("async_f2_int", []*Type{}, IntType, AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
			<-f2Ch
			return types.Int(2)
		}))),
		Function("async_f3", Overload("async_f3_int", []*Type{}, IntType, AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
			<-f3Ch
			return types.Int(3)
		}))),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}

	ast, iss := env.Compile(`async_f1() + async_f2() + async_f3() == 6`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}

	tests := []struct {
		name     string
		strategy async.DrainStrategy
		trigger  func()
	}{
		{
			// DrainNone re-evaluates after every single completion
			name:     "DrainNone",
			strategy: async.DrainNone(),
			trigger: func() {
				// Trigger them sequentially
				f1Ch <- struct{}{}
				time.Sleep(10 * time.Millisecond)
				f2Ch <- struct{}{}
				time.Sleep(10 * time.Millisecond)
				f3Ch <- struct{}{}
			},
		},
		{
			// DrainAll waits for all 3 to finish before re-evaluating
			name:     "DrainAll",
			strategy: async.DrainAll(),
			trigger: func() {
				// Trigger them at any pace, it shouldn't matter
				f1Ch <- struct{}{}
				time.Sleep(10 * time.Millisecond)
				f2Ch <- struct{}{}
				time.Sleep(10 * time.Millisecond)
				f3Ch <- struct{}{}
			},
		},
		{
			// DrainReady batches functions that complete within the debounce window
			name:     "DrainReady",
			strategy: async.DrainReady(50 * time.Millisecond),
			trigger: func() {
				// Fire f1, wait for it to be processed
				f1Ch <- struct{}{}
				// Fire f2 and f3 immediately after each other
				f2Ch <- struct{}{}
				f3Ch <- struct{}{}
			},
		},
		{
			// Custom strategy that re-evaluates immediately if a specific function completes
			name:     "CustomPriority",
			strategy: customPriorityDrain{priorityFunc: "async_f2"},
			trigger: func() {
				// Fire f1.
				f1Ch <- struct{}{}
				time.Sleep(10 * time.Millisecond)
				// Fire f2. Strategy should trigger re-eval immediately even if f3 is pending.
				f2Ch <- struct{}{}
				time.Sleep(10 * time.Millisecond)
				f3Ch <- struct{}{}
			},
		},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			prg, err := env.Program(ast, ConcurrentDrainStrategy(tc.strategy))
			if err != nil {
				t.Fatalf("env.Program() failed: %v", err)
			}

			// Start evaluation
			resCh := prg.ConcurrentEval(context.Background(), NoVars())

			// Trigger completions
			go tc.trigger()

			// Wait for result
			select {
			case res := <-resCh:
				if res.Err != nil {
					t.Fatalf("ConcurrentEval() returned error: %v", res.Err)
				}
				if res.Val.Equal(types.True) != types.True {
					t.Errorf("ConcurrentEval() = %v, want true", res.Val)
				}
			case <-time.After(1 * time.Second):
				t.Fatal("ConcurrentEval() timed out")
			}
		})
	}
}

type customPriorityDrain struct {
	priorityFunc string
}

func (d customPriorityDrain) NextAction(completed []async.Call, pending int) async.DrainAction {
	for _, c := range completed {
		// Exercise all methods for coverage
		_ = c.CallID()
		_ = c.Overload()
		if c.Function() == d.priorityFunc {
			return async.DrainAction{Reevaluate: true}
		}
	}
	return async.DrainAction{Reevaluate: pending == 0}
}

func TestConcurrentEval_DrainStrategies_Timeout(t *testing.T) {
	ch := make(chan struct{})
	env, _ := NewEnv(
		Function("async", Overload("async_int", []*Type{}, IntType, AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
			<-ch
			return types.Int(1)
		}))),
	)
	ast, _ := env.Compile(`async() == 1`)
	// Strategy that waits 10ms
	prg, _ := env.Program(ast, ConcurrentDrainStrategy(async.DrainReady(10*time.Millisecond)))

	resCh := prg.ConcurrentEval(context.Background(), NoVars())
	// Fire completion
	ch <- struct{}{}

	// The evaluator should wait 10ms and then re-evaluate.
	select {
	case res := <-resCh:
		if res.Err != nil {
			t.Fatalf("ConcurrentEval() failed: %v", res.Err)
		}
		if res.Val != types.True {
			t.Errorf("got %v, want true", res.Val)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out")
	}
}

func TestConcurrentEval_DrainStrategies_Cancel(t *testing.T) {
	ch1 := make(chan struct{})
	ch2 := make(chan struct{})
	env, _ := NewEnv(
		Function("async1", Overload("async1_int", []*Type{}, IntType, AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
			<-ch1
			return types.Int(1)
		}))),
		Function("async2", Overload("async2_int", []*Type{}, IntType, AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
			<-ch2
			return types.Int(2)
		}))),
	)
	ast, _ := env.Compile(`async1() + async2() == 3`)
	prg, _ := env.Program(ast, ConcurrentDrainStrategy(async.DrainAll()))

	ctx, cancel := context.WithCancel(context.Background())
	resCh := prg.ConcurrentEval(ctx, NoVars())

	// Trigger first completion
	ch1 <- struct{}{}
	// Give it a tiny bit of time to reach the second select
	time.Sleep(10 * time.Millisecond)
	// Cancel while loop is waiting for async2
	cancel()

	select {
	case res := <-resCh:
		if res.Err == nil || !errors.Is(res.Err, context.Canceled) {
			t.Errorf("got %v, want context.Canceled", res.Err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out")
	}
}

func TestEvalActivation_Parent(t *testing.T) {
	vars := map[string]any{"x": 1}
	act := activationPool.Setup(vars)
	defer activationPool.Put(act)
	if act.Parent() != nil {
		t.Errorf("evalActivation.Parent() should return nil")
	}
}

func TestAsyncProgramOptions(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	ast, _ := env.Compile(`1 + 1`)
	prg, err := env.Program(ast,
		AsyncCallObserver(nil),
		AsyncCompletionBufferSize(10),
		AsyncMaxConcurrency(5),
	)
	if err != nil {
		t.Fatalf("Program() failed: %v", err)
	}
	p := prg.(*prog)
	if p.asyncCompletionBufferSize != 10 {
		t.Errorf("asyncCompletionBufferSize = %d, want 10", p.asyncCompletionBufferSize)
	}
	if p.asyncMaxConcurrency != 5 {
		t.Errorf("asyncMaxConcurrency = %d, want 5", p.asyncMaxConcurrency)
	}
}

func TestConcurrentEval_Interrupt(t *testing.T) {
	env, err := NewEnv(Variable("items", ListType(IntType)))
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	// Use a comprehension with many iterations so interrupt checking fires
	ast, iss := env.Compile(`items.exists(i, i > 9999)`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}
	prg, err := env.Program(ast, InterruptCheckFrequency(1))
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}
	// Build a large input list so the comprehension runs long enough to be interrupted
	items := make([]int64, 10000)
	for i := range items {
		items[i] = int64(i)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errors.New("test interrupt"))

	_, _, err = prg.ContextEval(ctx, map[string]any{"items": items})
	if err == nil {
		t.Fatal("ContextEval() should return an interrupt error")
	}
	if !errors.Is(err, interpreter.InterruptError{}) {
		t.Errorf("expected InterruptError, got: %v", err)
	}
}

func TestConcurrentEval_Accumulate(t *testing.T) {
	ch := make(chan int, 2)
	env, err := NewEnv(
		Function("async_fn", Overload("async_fn_int", []*Type{}, IntType,
			AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
				v := <-ch
				return types.Int(v)
			}))),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	ast, iss := env.Compile(`async_fn() + async_fn() == 3`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}
	prg, err := env.Program(ast, ConcurrentDrainStrategy(async.DrainAll()))
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}

	resCh := prg.ConcurrentEval(context.Background(), NoVars())
	ch <- 1
	ch <- 2

	select {
	case res := <-resCh:
		if res.Err != nil {
			t.Fatalf("ConcurrentEval() failed: %v", res.Err)
		}
		if res.Val != types.True {
			t.Errorf("got %v, want true", res.Val)
		}
	case <-time.After(time.Second):
		t.Fatal("ConcurrentEval() timed out")
	}
}

func TestConcurrentEval_DrainReady_TimerReset(t *testing.T) {
	ch := make(chan struct{})
	env, err := NewEnv(
		Function("async_fn", Overload("async_fn_int", []*Type{}, IntType,
			AsyncBinding(func(ctx context.Context, args ...ref.Val) ref.Val {
				<-ch
				return types.Int(1)
			}))),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	ast, iss := env.Compile(`async_fn() == 1`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}
	// Use DrainReady with a non-zero duration to exercise the timer paths
	prg, err := env.Program(ast, ConcurrentDrainStrategy(async.DrainReady(50*time.Millisecond)))
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}

	resCh := prg.ConcurrentEval(context.Background(), NoVars())
	ch <- struct{}{}

	select {
	case res := <-resCh:
		if res.Err != nil {
			t.Fatalf("ConcurrentEval() failed: %v", res.Err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out")
	}
}
