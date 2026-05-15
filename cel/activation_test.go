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

package cel

import (
	"testing"

	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/interpreter"
)

func TestEvalActivation_Lazy(t *testing.T) {
	callCountAny := 0
	callCountVal := 0

	vars := map[string]any{
		"lazy_any": func() any {
			callCountAny++
			return 42
		},
		"lazy_val": func() ref.Val {
			callCountVal++
			return types.Int(100)
		},
		"static": "hello",
	}

	a := activationPool.Setup(vars)
	defer activationPool.Put(a)

	// Test lazy func() any
	val1, found1 := a.ResolveName("lazy_any")
	if !found1 || val1 != 42 {
		t.Errorf("ResolveName(lazy_any) got %v, want 42", val1)
	}
	if callCountAny != 1 {
		t.Errorf("lazy_any called %d times, want 1", callCountAny)
	}

	// Call again, should be cached
	val2, found2 := a.ResolveName("lazy_any")
	if !found2 || val2 != 42 {
		t.Errorf("ResolveName(lazy_any) got %v, want 42", val2)
	}
	if callCountAny != 1 {
		t.Errorf("lazy_any called %d times, want 1", callCountAny)
	}

	// Test lazy func() ref.Val
	val3, found3 := a.ResolveName("lazy_val")
	if !found3 || val3.(ref.Val).Equal(types.Int(100)) != types.True {
		t.Errorf("ResolveName(lazy_val) got %v, want 100", val3)
	}
	if callCountVal != 1 {
		t.Errorf("lazy_val called %d times, want 1", callCountVal)
	}

	// Call again, should be cached
	val4, found4 := a.ResolveName("lazy_val")
	if !found4 || val4.(ref.Val).Equal(types.Int(100)) != types.True {
		t.Errorf("ResolveName(lazy_val) got %v, want 100", val4)
	}
	if callCountVal != 1 {
		t.Errorf("lazy_val called %d times, want 1", callCountVal)
	}

	// Test static
	val5, found5 := a.ResolveName("static")
	if !found5 || val5 != "hello" {
		t.Errorf("ResolveName(static) got %v, want hello", val5)
	}
}

func TestEvalActivation_Pool(t *testing.T) {
	vars1 := map[string]any{
		"lazy": func() any { return 1 },
	}
	a1 := activationPool.Setup(vars1)

	// Evaluate to cache
	a1.ResolveName("lazy")
	if len(a1.lazyVars) != 1 {
		t.Errorf("lazyVars length is %d, want 1", len(a1.lazyVars))
	}

	// Return to pool
	activationPool.Put(a1)

	// Get from pool again with different vars
	vars2 := map[string]any{
		"lazy": func() any { return 2 },
	}
	a2 := activationPool.Setup(vars2)
	defer activationPool.Put(a2)

	// Verify it's clean
	if len(a2.lazyVars) != 0 {
		t.Errorf("lazyVars length is %d, want 0 after pool reset", len(a2.lazyVars))
	}
	val, found := a2.ResolveName("lazy")
	if !found || val != 2 {
		t.Errorf("ResolveName(lazy) got %v, want 2", val)
	}
}

func TestBuildFrame_ValidInputs(t *testing.T) {
	env, _ := NewEnv(Variable("x", IntType))
	ast, _ := env.Compile(`x`)
	prg, _ := env.Program(ast)
	p := prg.(*prog)

	act, _ := interpreter.NewActivation(map[string]any{"x": 2})
	frame := interpreter.NewExecutionFrame(act)

	tests := []struct {
		name  string
		input any
		want  any
	}{
		{name: "map_input", input: map[string]any{"x": 1}, want: 1},
		{name: "activation", input: act, want: 2},
		{name: "execution_frame", input: frame, want: 2},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup, err := p.buildFrame(tc.input)
			if err != nil {
				t.Fatalf("buildFrame() failed: %v", err)
			}
			defer cleanup()
			val, found := f.ResolveName("x")
			if !found || val != tc.want {
				t.Errorf("ResolveName(x) = %v, want %v", val, tc.want)
			}
		})
	}
}

func TestBuildFrame_InvalidInput(t *testing.T) {
	env, _ := NewEnv(Variable("x", IntType))
	ast, _ := env.Compile(`x`)
	prg, _ := env.Program(ast)
	p := prg.(*prog)

	_, _, err := p.buildFrame("invalid")
	if err == nil {
		t.Error("buildFrame(invalid) expected error, got nil")
	}
}
