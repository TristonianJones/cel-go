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

package interpreter

import (
	"context"
	"testing"
	"time"

	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/containers"
	"github.com/google/cel-go/common/functions"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

func TestPlanCallAsync(t *testing.T) {
	disp := NewDispatcher()
	err := disp.Add(&functions.Overload{
		Operator: "async_func",
		Async: func(ctx context.Context, args ...ref.Val) <-chan ref.Val {
			ch := make(chan ref.Val, 1)
			ch <- types.Int(1)
			return ch
		},
	})
	if err != nil {
		t.Fatalf("disp.Add() failed: %v", err)
	}

	reg, err := types.NewRegistry()
	if err != nil {
		t.Fatalf("types.NewRegistry() failed: %v", err)
	}
	cont := containers.DefaultContainer
	attrs := NewAttributeFactory(cont, reg, reg)
	p := newPlanner(disp, reg, reg, attrs, cont, nil)

	fac := ast.NewExprFactory()
	// Create a call expression: async_func()
	call := fac.NewCall(1, "async_func")
	
	// Plan the expression
	i, err := p.Plan(call)
	if err != nil {
		t.Fatalf("Plan() failed: %v", err)
	}

	// Verify that it planned as an async function
	if _, ok := i.(*evalAsyncFunc); !ok {
		t.Errorf("Plan() returned %T, wanted *evalAsyncFunc", i)
	}

	// Execute the expression
	frame := NewExecutionFrame(EmptyActivation())
	defer frame.Close()
	frame.SetContext(context.Background(), 1)
	completions := make(chan int64, 1)
	frame.SetCompletions(completions)

	res := i.Eval(frame)
	if !types.IsUnknown(res) {
		t.Errorf("Eval() returned %v, wanted unknown", res)
	}

	// Wait for the async call to complete
	select {
	case <-completions:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for async completion")
	}

	// Second call should return the result
	res2 := i.Eval(frame)
	if res2.Equal(types.Int(1)) != types.True {
		t.Errorf("Eval() returned %v, wanted 1", res2)
	}
}

func TestPlanCallAsync_Error(t *testing.T) {
	disp := NewDispatcher()
	reg, _ := types.NewRegistry()
	cont := containers.DefaultContainer
	attrs := NewAttributeFactory(cont, reg, reg)
	p := newPlanner(disp, reg, reg, attrs, cont, nil)

	fac := ast.NewExprFactory()
	// Create a call expression: missing_async()
	call := fac.NewCall(1, "missing_async")

	// Plan the expression - should fail because the function is missing
	_, err := p.Plan(call)
	if err == nil {
		t.Fatal("Plan() succeeded for missing function, wanted error")
	}

	// Add an overload without Async implementation
	disp.Add(&functions.Overload{
		Operator: "not_async",
		Unary: func(val ref.Val) ref.Val {
			return val
		},
	})
	call2 := fac.NewCall(2, "not_async", fac.NewLiteral(3, types.Int(1)))
	
	// Plan the expression - should NOT fail as it's not async, but let's check planCallAsync specifically
	// Actually, planCall only calls planCallAsync if fnDef.Async != nil.
	
	// Manually call planCallAsync to test the error path
	pb := &planBuilder{planner: p}
	_, err = pb.planCallAsync(call2, "not_async", "not_async", &functions.Overload{Operator: "not_async"}, nil)
	if err == nil {
		t.Fatal("planCallAsync() succeeded for non-async overload, wanted error")
	}
}

