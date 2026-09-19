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
	"math"
	"testing"

	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/types"
)

func TestSafeSubtract(t *testing.T) {
	tests := []struct {
		name string
		x, y uint64
		want uint64
	}{
		{name: "zero", x: 0, y: 0, want: 0},
		{name: "simple", x: 5, y: 3, want: 2},
		{name: "underflow to zero", x: 3, y: 5, want: 0},
		{name: "max minus zero", x: math.MaxUint64, y: 0, want: math.MaxUint64},
		{name: "max minus max", x: math.MaxUint64, y: math.MaxUint64, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SafeSubtract(tc.x, tc.y); got != tc.want {
				t.Errorf("SafeSubtract(%d, %d) got %d, want %d", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestSafeAdd(t *testing.T) {
	tests := []struct {
		name string
		x, y uint64
		rest []uint64
		want uint64
	}{
		{name: "zero", x: 0, y: 0, want: 0},
		{name: "simple", x: 2, y: 3, want: 5},
		{name: "variadic", x: 1, y: 2, rest: []uint64{3, 4}, want: 10},
		{name: "max plus zero", x: math.MaxUint64, y: 0, want: math.MaxUint64},
		{name: "overflow", x: math.MaxUint64, y: 1, want: math.MaxUint64},
		{name: "overflow near max", x: math.MaxUint64 - 5, y: 10, want: math.MaxUint64},
		{name: "overflow in rest", x: 1, y: 2, rest: []uint64{math.MaxUint64}, want: math.MaxUint64},
		{name: "saturated stays saturated", x: math.MaxUint64, y: math.MaxUint64,
			rest: []uint64{math.MaxUint64}, want: math.MaxUint64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SafeAdd(tc.x, tc.y, tc.rest...); got != tc.want {
				t.Errorf("SafeAdd(%d, %d, %v) got %d, want %d", tc.x, tc.y, tc.rest, got, tc.want)
			}
		})
	}
}

func TestSafeMultiply(t *testing.T) {
	tests := []struct {
		name string
		x, y uint64
		want uint64
	}{
		{name: "zero", x: 0, y: 0, want: 0},
		{name: "max by zero", x: math.MaxUint64, y: 0, want: 0},
		{name: "simple", x: 3, y: 4, want: 12},
		{name: "max by one", x: math.MaxUint64, y: 1, want: math.MaxUint64},
		{name: "overflow", x: math.MaxUint64, y: 2, want: math.MaxUint64},
		{name: "overflow squared", x: math.MaxUint32, y: math.MaxUint32 * 2, want: math.MaxUint64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SafeMultiply(tc.x, tc.y); got != tc.want {
				t.Errorf("SafeMultiply(%d, %d) got %d, want %d", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

func TestSafeMultiplyByFactor(t *testing.T) {
	tests := []struct {
		name   string
		x      uint64
		factor float64
		want   uint64
	}{
		{name: "zero value", x: 0, factor: 0.1, want: 0},
		{name: "zero factor", x: 100, factor: 0, want: 0},
		{name: "rounds up", x: 15, factor: 0.1, want: 2},
		{name: "exact", x: 10, factor: 0.1, want: 1},
		{name: "whole factor", x: 10, factor: 3, want: 30},
		{name: "max saturates", x: math.MaxUint64, factor: 2, want: math.MaxUint64},
		{name: "max scaled down", x: math.MaxUint64, factor: 0.1, want: 1844674407370955264},
		{name: "negative factor", x: 10, factor: -1, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SafeMultiplyByFactor(tc.x, tc.factor); got != tc.want {
				t.Errorf("SafeMultiplyByFactor(%d, %f) got %d, want %d", tc.x, tc.factor, got, tc.want)
			}
		})
	}
}

func TestSafeCeil(t *testing.T) {
	tests := []struct {
		name string
		x    float64
		want uint64
	}{
		{name: "zero", x: 0, want: 0},
		{name: "negative", x: -1.5, want: 0},
		{name: "nan", x: math.NaN(), want: 0},
		{name: "fraction", x: 0.1, want: 1},
		{name: "rounds up", x: 2.5, want: 3},
		{name: "whole", x: 3.0, want: 3},
		{name: "infinity", x: math.Inf(1), want: math.MaxUint64},
		{name: "out of range", x: math.Ldexp(1.0, 64), want: math.MaxUint64},
		{name: "largest in range", x: math.Ldexp(1.0, 63), want: 1 << 63},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SafeCeil(tc.x); got != tc.want {
				t.Errorf("SafeCeil(%f) got %d, want %d", tc.x, got, tc.want)
			}
		})
	}
}

func TestSizeEstimate(t *testing.T) {
	s1 := FixedSizeEstimate(5)
	s2 := FixedSizeEstimate(10)

	tests := []struct {
		name string
		got  SizeEstimate
		want SizeEstimate
	}{
		{
			name: "add",
			got:  s1.Add(s2),
			want: SizeEstimate{Min: 15, Max: 15},
		},
		{
			name: "multiply",
			got:  s1.Multiply(s2),
			want: SizeEstimate{Min: 50, Max: 50},
		},
		{
			name: "union",
			got:  s1.Union(s2),
			want: SizeEstimate{Min: 5, Max: 10},
		},
		{
			name: "unknown_size_estimate",
			got:  UnknownSizeEstimate(),
			want: SizeEstimate{Min: 0, Max: math.MaxUint64},
		},
		{
			name: "ranged_size_estimate",
			got:  RangedSizeEstimate(3, 8),
			want: SizeEstimate{Min: 3, Max: 8},
		},
		{
			name: "at_least_one_size",
			got:  AtLeastOneSize(FixedSizeEstimate(0)),
			want: SizeEstimate{Min: 1, Max: 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}

	costTests := []struct {
		name string
		got  CostEstimate
		want CostEstimate
	}{
		{
			name: "multiply_by_cost_factor",
			got:  s1.MultiplyByCostFactor(0.5),
			want: CostEstimate{Min: 3, Max: 3},
		},
		{
			name: "multiply_by_cost",
			got:  s1.MultiplyByCost(FixedCostEstimate(4)),
			want: CostEstimate{Min: 20, Max: 20},
		},
		{
			name: "as_cost",
			got:  s1.AsCost(),
			want: CostEstimate{Min: 5, Max: 5},
		},
	}
	for _, tc := range costTests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestCostEstimate(t *testing.T) {
	c1 := FixedCostEstimate(5)
	c2 := FixedCostEstimate(10)

	tests := []struct {
		name string
		got  CostEstimate
		want CostEstimate
	}{
		{
			name: "add",
			got:  c1.Add(c2),
			want: CostEstimate{Min: 15, Max: 15},
		},
		{
			name: "multiply",
			got:  c1.Multiply(c2),
			want: CostEstimate{Min: 50, Max: 50},
		},
		{
			name: "union",
			got:  c1.Union(c2),
			want: CostEstimate{Min: 5, Max: 10},
		},
		{
			name: "multiply_by_cost_factor",
			got:  c1.MultiplyByCostFactor(0.5),
			want: CostEstimate{Min: 3, Max: 3},
		},
		{
			name: "unknown_cost_estimate",
			got:  UnknownCostEstimate(),
			want: CostEstimate{Min: 0, Max: math.MaxUint64},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestExtCostHelpers(t *testing.T) {
	sz := FixedSizeEstimate(10)

	tests := []struct {
		name     string
		callEst  *CallEstimate
		wantCost CostEstimate
		wantSize *SizeEstimate
	}{
		{
			name: "estimate_string_scan",
			callEst: func() *CallEstimate {
				costEst, resSz := EstimateStringScan(sz)
				return NewCallEstimate(costEst, resSz)
			}(),
			wantCost: FixedCostEstimate(1),
			wantSize: &sz,
		},
		{
			name: "estimate_list_alloc",
			callEst: func() *CallEstimate {
				allocCost, allocSz := EstimateListAlloc(sz, 0.5)
				return NewCallEstimate(allocCost, allocSz)
			}(),
			wantCost: FixedCostEstimate(15),
			wantSize: &sz,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.callEst.CostEstimate != tc.wantCost {
				t.Errorf("CostEstimate = %v, want %v", tc.callEst.CostEstimate, tc.wantCost)
			}
			if (tc.callEst.ResultSize == nil) != (tc.wantSize == nil) ||
				(tc.callEst.ResultSize != nil && *tc.callEst.ResultSize != *tc.wantSize) {
				t.Errorf("ResultSize = %v, want %v", tc.callEst.ResultSize, tc.wantSize)
			}
		})
	}
}

func TestSizeEstimate_Subtract(t *testing.T) {
	tests := []struct {
		name string
		s1   SizeEstimate
		s2   SizeEstimate
		want SizeEstimate
	}{
		{
			name: "ranged_subtract",
			s1:   RangedSizeEstimate(5, 15),
			s2:   RangedSizeEstimate(2, 4),
			want: SizeEstimate{Min: 1, Max: 13},
		},
		{
			name: "underflow_subtract",
			s1:   RangedSizeEstimate(2, 4),
			s2:   RangedSizeEstimate(5, 10),
			want: SizeEstimate{Min: 0, Max: 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s1.Subtract(tc.s2); got != tc.want {
				t.Errorf("%v.Subtract(%v) = %v, want %v", tc.s1, tc.s2, got, tc.want)
			}
		})
	}
}

func TestEstimateSize(t *testing.T) {
	compSz := FixedSizeEstimate(42)
	nodeWithComp := NewAstNode(nil, nil, types.IntType, &compSz)
	nodeWithoutComp := NewAstNode(nil, []string{"foo"}, types.IntType, nil)
	nodeUnknown := NewAstNode(nil, []string{"bar"}, types.IntType, nil)
	est := testHintsEstimator{hints: map[string]uint64{"foo": 100}}

	tests := []struct {
		name      string
		estimator Estimator
		node      AstNode
		want      SizeEstimate
	}{
		{
			name:      "nil_node",
			estimator: nil,
			node:      nil,
			want:      UnknownSizeEstimate(),
		},
		{
			name:      "computed_size",
			estimator: nil,
			node:      nodeWithComp,
			want:      compSz,
		},
		{
			name:      "with_estimator",
			estimator: est,
			node:      nodeWithoutComp,
			want:      FixedSizeEstimate(100),
		},
		{
			name:      "estimator_returns_nil",
			estimator: est,
			node:      nodeUnknown,
			want:      UnknownSizeEstimate(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if sz := EstimateSize(tc.estimator, tc.node); sz != tc.want {
				t.Errorf("EstimateSize() = %v, want %v", sz, tc.want)
			}
		})
	}
}

func TestNodeAsUintValue(t *testing.T) {
	fac := ast.NewExprFactory()
	identNode := NewAstNode(fac.NewIdent(1, "x"), nil, types.IntType, nil)
	strNode := NewAstNode(fac.NewLiteral(2, types.String("hello")), nil, types.StringType, nil)
	posIntNode := NewAstNode(fac.NewLiteral(3, types.Int(42)), nil, types.IntType, nil)
	negIntNode := NewAstNode(fac.NewLiteral(4, types.Int(-5)), nil, types.IntType, nil)
	uintNode := NewAstNode(fac.NewLiteral(5, types.Uint(100)), nil, types.UintType, nil)

	tests := []struct {
		name       string
		node       AstNode
		defaultVal uint64
		want       uint64
	}{
		{
			name:       "nil_node",
			node:       nil,
			defaultVal: 99,
			want:       99,
		},
		{
			name:       "non_literal_ident",
			node:       identNode,
			defaultVal: 99,
			want:       99,
		},
		{
			name:       "non_int_literal_string",
			node:       strNode,
			defaultVal: 99,
			want:       99,
		},
		{
			name:       "positive_int_literal",
			node:       posIntNode,
			defaultVal: 99,
			want:       42,
		},
		{
			name:       "negative_int_literal_saturates_zero",
			node:       negIntNode,
			defaultVal: 99,
			want:       0,
		},
		{
			name:       "uint_literal",
			node:       uintNode,
			defaultVal: 99,
			want:       100,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if val := NodeAsUintValue(tc.node, tc.defaultVal); val != tc.want {
				t.Errorf("NodeAsUintValue() = %d, want %d", val, tc.want)
			}
		})
	}
}
