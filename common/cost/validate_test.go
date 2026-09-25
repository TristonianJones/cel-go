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

package cost

import (
	"strings"
	"testing"
)

// unanswerable is a projection over a constant: it has an estimate, but no value to read at
// tracking time, which is what Validate exists to reject.
var unanswerable = ElemOf(Const(4))

func TestOverloadModelValidate(t *testing.T) {
	tests := []struct {
		name  string
		model OverloadModel
		err   string
	}{
		{
			name:  "valid_cost_only",
			model: Overload("test_id", EvalCost(Const(1))),
		},
		{
			name: "valid_projections_over_values",
			model: MemberOverload("test_id",
				EvalCost(Sum(TargetElem(), ArgKey(0), ElemTotal(Result()))),
				ResultSize(List(Target(), TargetElem())),
			),
		},
		{
			name: "valid_nested_projection",
			model: Overload("test_id",
				EvalCost(ElemOf(ElemOf(Arg(0)))),
			),
		},
		{
			name:  "missing_id",
			model: Overload("", EvalCost(Const(1))),
			err:   "requires an ID",
		},
		{
			name:  "missing_cost",
			model: Overload("test_id", ResultSize(Arg(0))),
			err:   `overload "test_id" requires an EvalCost expression`,
		},
		{
			name:  "projection_over_constant",
			model: Overload("test_id", EvalCost(unanswerable)),
			err:   `overload "test_id" eval cost: ElemOf is not defined over Const`,
		},
		{
			name:  "projection_over_sum",
			model: Overload("test_id", EvalCost(KeyOf(Sum(Arg(0), Arg(1))))),
			err:   "KeyOf is not defined over Sum",
		},
		{
			name:  "total_over_scale",
			model: Overload("test_id", EvalCost(ElemTotal(Scale(Arg(0), 2.0)))),
			err:   "ElemTotal is not defined over Scale",
		},
		{
			name:  "projection_without_operand",
			model: Overload("test_id", EvalCost(ElemOf(nil))),
			err:   "ElemOf requires an operand",
		},
		{
			name:  "projection_in_result_size",
			model: Overload("test_id", EvalCost(Const(1)), ResultSize(unanswerable)),
			err:   `overload "test_id" result size: ElemOf is not defined over Const`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.model.Validate()
			if tc.err == "" {
				if err != nil {
					t.Fatalf("Validate() errored: %v, wanted nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() succeeded, wanted error containing %q", tc.err)
			}
			if !strings.Contains(err.Error(), tc.err) {
				t.Errorf("Validate() errored: %v, wanted error containing %q", err, tc.err)
			}
		})
	}
}

func TestValidateOverloadModels(t *testing.T) {
	err := ValidateOverloadModels(
		Overload("first", EvalCost(Const(1))),
		Overload("second", EvalCost(unanswerable)),
	)
	if err == nil || !strings.Contains(err.Error(), `overload "second"`) {
		t.Errorf("ValidateOverloadModels() errored: %v, wanted the second model reported", err)
	}
	if err := ValidateOverloadModels(); err != nil {
		t.Errorf("ValidateOverloadModels() errored: %v, wanted nil", err)
	}
}

// TestStandardOverloadModelsValidate holds the shipped models to the same rules user models are
// held to at registration.
func TestStandardOverloadModelsValidate(t *testing.T) {
	if err := ValidateOverloadModels(StandardOverloadModels...); err != nil {
		t.Errorf("ValidateOverloadModels(StandardOverloadModels) errored: %v, wanted nil", err)
	}
}

// TestDescribeQuantityExprLabels covers every expression type, so that a new one which is not
// listed in describeQuantityExpr is reported rather than silently named by its Go type.
func TestDescribeQuantityExprLabels(t *testing.T) {
	tests := []struct {
		expr QuantityExpr
		want string
	}{
		{expr: Const(1), want: "Const"},
		{expr: Arg(0), want: "Arg"},
		{expr: IntArg(0, 1), want: "IntArg"},
		{expr: Target(), want: "Target"},
		{expr: IntTarget(1), want: "IntTarget"},
		{expr: Result(), want: "Result"},
		{expr: ElemOf(Arg(0)), want: "ElemOf"},
		{expr: KeyOf(Arg(0)), want: "KeyOf"},
		{expr: ElemTotal(Arg(0)), want: "ElemTotal"},
		{expr: Sum(Arg(0)), want: "Sum"},
		{expr: Sub(Arg(0), Arg(1)), want: "Sub"},
		{expr: Mul(Arg(0)), want: "Mul"},
		{expr: Scale(Arg(0), 1.0), want: "Scale"},
		{expr: Min(Arg(0), Arg(1)), want: "Min"},
		{expr: Max(Arg(0), Arg(1)), want: "Max"},
		{expr: Union(Arg(0)), want: "Union"},
		{expr: Ranged(Arg(0), Arg(1)), want: "Ranged"},
		{expr: AtLeastOneQuantity(Arg(0)), want: "AtLeastOneQuantity"},
		{expr: List(Arg(0), ArgElem(0)), want: "List"},
		{expr: Map(Arg(0), ArgKey(0), ArgElem(0)), want: "Map"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got, _ := describeQuantityExpr(tc.expr)
			if got != tc.want {
				t.Errorf("describeQuantityExpr() got %q, wanted %q", got, tc.want)
			}
		})
	}
}

// TestValidateTraversesOperands places an unanswerable projection in each operand slot of each
// composite expression. A type missing from describeQuantityExpr reports no operands, which hides
// everything below it from validation, and fails here.
func TestValidateTraversesOperands(t *testing.T) {
	tests := []struct {
		name string
		expr QuantityExpr
	}{
		{name: "elem_of", expr: ElemOf(ElemOf(unanswerable))},
		{name: "key_of", expr: KeyOf(ElemOf(unanswerable))},
		{name: "elem_total", expr: ElemTotal(ElemOf(unanswerable))},
		{name: "sum_first", expr: Sum(unanswerable, Arg(0))},
		{name: "sum_last", expr: Sum(Arg(0), unanswerable)},
		{name: "sub_lhs", expr: Sub(unanswerable, Arg(0))},
		{name: "sub_rhs", expr: Sub(Arg(0), unanswerable)},
		{name: "mul_first", expr: Mul(unanswerable, Arg(0))},
		{name: "mul_last", expr: Mul(Arg(0), unanswerable)},
		{name: "scale", expr: Scale(unanswerable, 2.0)},
		{name: "min_lhs", expr: Min(unanswerable, Arg(0))},
		{name: "min_rhs", expr: Min(Arg(0), unanswerable)},
		{name: "max_lhs", expr: Max(unanswerable, Arg(0))},
		{name: "max_rhs", expr: Max(Arg(0), unanswerable)},
		{name: "union_first", expr: Union(unanswerable, Arg(0))},
		{name: "union_last", expr: Union(Arg(0), unanswerable)},
		{name: "ranged_min", expr: Ranged(unanswerable, Arg(0))},
		{name: "ranged_max", expr: Ranged(Arg(0), unanswerable)},
		{name: "at_least_one", expr: AtLeastOneQuantity(unanswerable)},
		{name: "list_len", expr: List(unanswerable, Arg(0))},
		{name: "list_elem", expr: List(Arg(0), unanswerable)},
		{name: "map_size", expr: Map(unanswerable, Arg(0), Arg(1))},
		{name: "map_key", expr: Map(Arg(0), unanswerable, Arg(1))},
		{name: "map_value", expr: Map(Arg(0), Arg(1), unanswerable)},
		{name: "square", expr: Square(unanswerable)},
		{name: "string_scan", expr: StringScan(unanswerable)},
		{name: "list_alloc", expr: ListAlloc(unanswerable, 0.5)},
		{name: "traversal", expr: Traversal(unanswerable, 0.5, 10)},
		{name: "at_most", expr: AtMost(unanswerable)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateQuantityExpr(tc.expr); err == nil {
				t.Error("validateQuantityExpr() succeeded, wanted the nested projection reported")
			}
		})
	}
}
