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
	"strings"
	"testing"

	"cel.dev/cel-go/common/cost"
	"cel.dev/cel-go/common/overloads"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

func TestCostModelTrackerOptions(t *testing.T) {
	tests := []struct {
		name       string
		expr       string
		envOpts    []EnvOption
		in         any
		wantResult ref.Val
		wantCost   uint64
	}{
		{
			name: "custom_global_function_cost_model",
			expr: `custom_len(str)`,
			envOpts: []EnvOption{
				Variable("str", StringType),
				Function("custom_len",
					Overload("custom_len_string", []*Type{StringType}, IntType,
						UnaryBinding(func(val ref.Val) ref.Val {
							return types.Int(len(val.Value().(string)))
						}),
					),
				),
				CostModel(
					cost.Overload("custom_len_string",
						cost.EvalCost(cost.Const(42)),
					),
				),
			},
			in:         map[string]any{"str": "hello"},
			wantResult: types.Int(5),
			// 1 for variable 'str' + 42 for custom_len
			wantCost: 43,
		},
		{
			name: "custom_member_function_cost_model",
			expr: `str.custom_transform("prefix_")`,
			envOpts: []EnvOption{
				Variable("str", StringType),
				Function("custom_transform",
					MemberOverload("string_custom_transform_string", []*Type{StringType, StringType}, StringType,
						BinaryBinding(func(target, arg ref.Val) ref.Val {
							return types.String(arg.Value().(string) + target.Value().(string))
						}),
					),
				),
				CostModel(
					cost.MemberOverload("string_custom_transform_string",
						cost.EvalCost(cost.Sum(cost.Scale(cost.Target(), 2.0), cost.Scale(cost.Arg(0), 1.0))),
					),
				),
			},
			in:         map[string]any{"str": "abc"},
			wantResult: types.String("prefix_abc"),
			// 1 for variable 'str' + target size (3)*2 + arg0 size (7)*1 = 1 + 6 + 7 = 14
			wantCost: 14,
		},
		{
			name: "override_standard_overload_cost_model",
			expr: `str.startsWith("prefix")`,
			envOpts: []EnvOption{
				Variable("str", StringType),
				CostModel(
					cost.MemberOverload(overloads.StartsWithString,
						cost.EvalCost(cost.Const(100)),
					),
				),
			},
			in:         map[string]any{"str": "prefix_test"},
			wantResult: types.True,
			// 1 for variable 'str' + 100 for startsWith
			wantCost: 101,
		},
		{
			name: "multiple_overload_models",
			expr: `str.startsWith("pre") && str.endsWith("fix")`,
			envOpts: []EnvOption{
				Variable("str", StringType),
				CostModel(
					cost.MemberOverload(overloads.StartsWithString,
						cost.EvalCost(cost.Const(10)),
					),
					cost.MemberOverload(overloads.EndsWithString,
						cost.EvalCost(cost.Const(20)),
					),
				),
			},
			in:         map[string]any{"str": "prefix"},
			wantResult: types.True,
			// 1 (str) + 10 (startsWith) + 1 (str) + 20 (endsWith) = 32
			wantCost: 32,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := testEnv(t, tc.envOpts...)
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Compile(%q) failed: %v", tc.expr, iss.Err())
			}

			prg, err := env.Program(ast, CostTracking(nil))
			if err != nil {
				t.Fatalf("env.Program() failed: %v", err)
			}

			out, details, err := prg.Eval(tc.in)
			if err != nil {
				t.Fatalf("prg.Eval() failed: %v", err)
			}

			if out.Equal(tc.wantResult) != types.True {
				t.Errorf("prg.Eval() result = %v, want %v", out, tc.wantResult)
			}

			if details.ActualCost() == nil {
				t.Fatalf("details.ActualCost() is nil, expected %d", tc.wantCost)
			}
			if *details.ActualCost() != tc.wantCost {
				t.Errorf("details.ActualCost() = %d, want %d", *details.ActualCost(), tc.wantCost)
			}
		})
	}
}

func TestCostSizingStrategyTrackerOptions(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		envOpts  []EnvOption
		in       any
		wantCost uint64
	}{
		{
			name: "aggregate_sizing_strategy_with_cost_model",
			expr: `"b" in list`,
			envOpts: []EnvOption{
				Variable("list", ListType(StringType)),
				CostSizingStrategy(cost.AggregateSizingStrategy()),
				CostModel(
					cost.Overload(overloads.InList,
						cost.EvalCost(cost.Arg(1)),
					),
				),
			},
			in: map[string]any{"list": []string{"hello", "world"}},
			// Aggregate size of ["hello", "world"] = 1 (list container) + 5 ("hello") + 5 ("world") = 11
			// 1 for variable 'list' + 11 for in_list = 12
			wantCost: 12,
		},
		{
			name: "default_sizing_strategy_with_cost_model",
			expr: `"b" in list`,
			envOpts: []EnvOption{
				Variable("list", ListType(StringType)),
				CostSizingStrategy(cost.DefaultSizingStrategy()),
				CostModel(
					cost.Overload(overloads.InList,
						cost.EvalCost(cost.Arg(1)),
					),
				),
			},
			in: map[string]any{"list": []string{"hello", "world"}},
			// Default size of list ["hello", "world"] = 2 (elements only)
			// 1 for variable 'list' + 2 for in_list = 3
			wantCost: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := testEnv(t, tc.envOpts...)
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Compile(%q) failed: %v", tc.expr, iss.Err())
			}

			prg, err := env.Program(ast, CostTracking(nil))
			if err != nil {
				t.Fatalf("env.Program() failed: %v", err)
			}

			_, details, err := prg.Eval(tc.in)
			if err != nil {
				t.Fatalf("prg.Eval() failed: %v", err)
			}

			if details.ActualCost() == nil {
				t.Fatalf("details.ActualCost() is nil, expected %d", tc.wantCost)
			}
			if *details.ActualCost() != tc.wantCost {
				t.Errorf("details.ActualCost() = %d, want %d", *details.ActualCost(), tc.wantCost)
			}
		})
	}
}

func TestCostModelExtendEnv(t *testing.T) {
	parentEnv := testEnv(t,
		Variable("str", StringType),
		CostModel(
			cost.MemberOverload(overloads.StartsWithString,
				cost.EvalCost(cost.Const(50)),
			),
		),
		CostSizingStrategy(cost.AggregateSizingStrategy()),
	)

	childEnv, err := parentEnv.Extend(
		CostModel(
			cost.MemberOverload(overloads.EndsWithString,
				cost.EvalCost(cost.Const(25)),
			),
		),
	)
	if err != nil {
		t.Fatalf("parentEnv.Extend() failed: %v", err)
	}

	ast, iss := childEnv.Compile(`str.startsWith("a") && str.endsWith("z")`)
	if iss.Err() != nil {
		t.Fatalf("childEnv.Compile() failed: %v", iss.Err())
	}

	prg, err := childEnv.Program(ast, CostTracking(nil))
	if err != nil {
		t.Fatalf("childEnv.Program() failed: %v", err)
	}

	_, details, err := prg.Eval(map[string]any{"str": "abc_xyz"})
	if err != nil {
		t.Fatalf("prg.Eval() failed: %v", err)
	}

	if details.ActualCost() == nil {
		t.Fatal("details.ActualCost() is nil")
	}
	// 1 (str) + 50 (startsWith) + 1 (str) + 25 (endsWith) = 77
	wantCost := uint64(77)
	if *details.ActualCost() != wantCost {
		t.Errorf("details.ActualCost() = %d, want %d", *details.ActualCost(), wantCost)
	}
}

func TestCostTrackerOptionsPrecedence(t *testing.T) {
	// Program-level CostTrackerOptions should override/augment environment-level CostModel.
	env := testEnv(t,
		Variable("str", StringType),
		CostModel(
			cost.MemberOverload(overloads.StartsWithString,
				cost.EvalCost(cost.Const(50)),
			),
		),
	)

	ast, iss := env.Compile(`str.startsWith("prefix")`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}

	overrideCost := uint64(999)
	prg, err := env.Program(ast,
		CostTracking(nil),
		CostTrackerOptions(
			cost.OverloadTracker(overloads.StartsWithString, func(args []ref.Val, result ref.Val) *uint64 {
				return &overrideCost
			}),
		),
	)
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}

	_, details, err := prg.Eval(map[string]any{"str": "prefix_value"})
	if err != nil {
		t.Fatalf("prg.Eval() failed: %v", err)
	}

	if details.ActualCost() == nil {
		t.Fatal("details.ActualCost() is nil")
	}
	// 1 (str) + 999 (program-level override) = 1000
	wantCost := uint64(1000)
	if *details.ActualCost() != wantCost {
		t.Errorf("details.ActualCost() = %d, want %d", *details.ActualCost(), wantCost)
	}
}

func TestCostLimitWithCostModel(t *testing.T) {
	env := testEnv(t,
		Variable("str", StringType),
		CostModel(
			cost.MemberOverload(overloads.StartsWithString,
				cost.EvalCost(cost.Const(100)),
			),
		),
	)

	ast, iss := env.Compile(`str.startsWith("prefix")`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}

	// Case 1: Limit higher than cost (101) -> succeeds
	prgPass, err := env.Program(ast, CostLimit(150))
	if err != nil {
		t.Fatalf("env.Program(CostLimit(150)) failed: %v", err)
	}
	_, detailsPass, err := prgPass.Eval(map[string]any{"str": "prefix_val"})
	if err != nil {
		t.Fatalf("prgPass.Eval() failed: %v", err)
	}
	if detailsPass.ActualCost() == nil || *detailsPass.ActualCost() != 101 {
		t.Errorf("detailsPass.ActualCost() = %v, want 101", detailsPass.ActualCost())
	}

	// Case 2: Limit lower than cost (101) -> fails
	prgFail, err := env.Program(ast, CostLimit(50))
	if err != nil {
		t.Fatalf("env.Program(CostLimit(50)) failed: %v", err)
	}
	_, _, err = prgFail.Eval(map[string]any{"str": "prefix_val"})
	if err == nil {
		t.Fatal("prgFail.Eval() expected error due to cost limit, got nil")
	}
	if !strings.Contains(err.Error(), "actual cost limit exceeded") {
		t.Errorf("prgFail.Eval() error = %q, want containing 'actual cost limit exceeded'", err.Error())
	}
}

func TestCostModelEstimateAndTrackingAlignment(t *testing.T) {
	model := cost.MemberOverload(overloads.StartsWithString,
		cost.EvalCost(cost.Scale(cost.Arg(0), 1.0)),
	)
	env := testEnv(t,
		Variable("str", StringType),
		CostModel(model),
	)

	ast, iss := env.Compile(`str.startsWith("prefix")`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}

	// Estimate cost
	est, err := env.EstimateCost(ast, testCostEstimator{hints: map[string]uint64{"str": 10}})
	if err != nil {
		t.Fatalf("env.EstimateCost() failed: %v", err)
	}

	// Track actual cost
	prg, err := env.Program(ast, CostTracking(nil))
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}

	_, details, err := prg.Eval(map[string]any{"str": "prefix_hello"})
	if err != nil {
		t.Fatalf("prg.Eval() failed: %v", err)
	}

	if details.ActualCost() == nil {
		t.Fatal("details.ActualCost() is nil")
	}

	actualCost := *details.ActualCost()
	if actualCost < est.Min || actualCost > est.Max {
		t.Errorf("actualCost %d not in estimated range [%d, %d]", actualCost, est.Min, est.Max)
	}
}

func TestCostModelNilOrEmpty(t *testing.T) {
	// Verify that cost tracking works as expected when no CostModel or CostSizingStrategy is set
	env := testEnv(t,
		Variable("x", IntType),
	)

	ast, iss := env.Compile(`x + 1`)
	if iss.Err() != nil {
		t.Fatalf("env.Compile() failed: %v", iss.Err())
	}

	prg, err := env.Program(ast, CostTracking(nil))
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}

	out, details, err := prg.Eval(map[string]any{"x": 10})
	if err != nil {
		t.Fatalf("prg.Eval() failed: %v", err)
	}

	if out.Equal(types.Int(11)) != types.True {
		t.Errorf("got %v, want 11", out)
	}

	if details.ActualCost() == nil {
		t.Fatal("details.ActualCost() is nil")
	}
	if *details.ActualCost() == 0 {
		t.Errorf("details.ActualCost() = 0, want > 0")
	}
}
