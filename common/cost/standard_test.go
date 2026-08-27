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
	"testing"

	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/overloads"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

func TestStandardOverloadModels_CollectionCompleteness(t *testing.T) {
	estimators := StandardOverloadEstimators()
	trackers := StandardOverloadTrackers()

	if len(estimators) != len(StandardOverloadModels) {
		t.Errorf("got %d estimators, wanted %d", len(estimators), len(StandardOverloadModels))
	}
	if len(trackers) != len(StandardOverloadModels) {
		t.Errorf("got %d trackers, wanted %d", len(trackers), len(StandardOverloadModels))
	}
}

func TestStandardOverloadModels_BasicOperationTrackers(t *testing.T) {
	trackers := StandardOverloadTrackers()
	adapter := types.DefaultTypeAdapter

	tests := []struct {
		name       string
		overloadID string
		args       []ref.Val
		wantCost   uint64
	}{
		{
			name:       "in_list_string_elements",
			overloadID: overloads.InList,
			args: []ref.Val{
				types.String("item"),
				adapter.NativeToValue([]string{"a", "b", "c"}),
			},
			wantCost: 3,
		},
		{
			name:       "index_list_lookup",
			overloadID: overloads.IndexList,
			args: []ref.Val{
				adapter.NativeToValue([]string{"a", "b", "c"}),
				types.Int(1),
			},
			wantCost: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracker := trackers[tc.overloadID]
			if tracker == nil {
				t.Fatalf("missing tracker for %s", tc.overloadID)
			}
			cost := tracker(tc.args, nil)
			if cost == nil {
				t.Fatalf("tracker returned nil cost")
			}
			if *cost != tc.wantCost {
				t.Errorf("cost = %d, want %d", *cost, tc.wantCost)
			}
		})
	}
}

func TestEqualsNotEqualsOverloadModels_Trackers(t *testing.T) {
	trackers := StandardOverloadTrackers()
	eqTracker := trackers[overloads.Equals]
	if eqTracker == nil {
		t.Fatalf("missing tracker for Equals")
	}
	neTracker := trackers[overloads.NotEquals]
	if neTracker == nil {
		t.Fatalf("missing tracker for NotEquals")
	}

	adapter := types.DefaultTypeAdapter

	largeIntSlice := make([]int64, 40)
	for i := range largeIntSlice {
		largeIntSlice[i] = int64(i)
	}

	largeByteSlice := make([]byte, 50)
	for i := range largeByteSlice {
		largeByteSlice[i] = byte(i)
	}

	tests := []struct {
		name     string
		tracker  FunctionTracker
		lhs, rhs any
		wantCost uint64
	}{
		{
			name:     "int_list_equal_small",
			tracker:  eqTracker,
			lhs:      []int64{1, 2, 3},
			rhs:      []int64{1, 2, 3},
			wantCost: 1, // ceil(3 * 0.1) = 1
		},
		{
			name:     "int_list_equal_large",
			tracker:  eqTracker,
			lhs:      largeIntSlice,
			rhs:      largeIntSlice,
			wantCost: 4, // ceil(40 * 0.1) = 4
		},
		{
			name:     "int_list_unequal_sizes",
			tracker:  neTracker,
			lhs:      []int64{1, 2, 3, 4, 5},
			rhs:      largeIntSlice,
			wantCost: 1, // ceil(min(5, 40) * 0.1) = 1
		},
		{
			name:     "map_equal",
			tracker:  eqTracker,
			lhs:      map[string]int64{"a": 1, "b": 2},
			rhs:      map[string]int64{"a": 1, "b": 2},
			wantCost: 1, // ceil(2 * 0.1) = 1
		},
		{
			name:     "bytes_equal_small",
			tracker:  eqTracker,
			lhs:      []byte("hello"),
			rhs:      []byte("hello"),
			wantCost: 1, // ceil(5 * 0.1) = 1
		},
		{
			name:     "bytes_not_equal_large",
			tracker:  neTracker,
			lhs:      largeByteSlice,
			rhs:      largeByteSlice,
			wantCost: 5, // ceil(50 * 0.1) = 5
		},
		{
			name:     "string_equal",
			tracker:  eqTracker,
			lhs:      "hello world",
			rhs:      "hello world",
			wantCost: 2, // ceil(11 * 0.1) = 2
		},
		{
			name:     "scalar_int_equal",
			tracker:  eqTracker,
			lhs:      int64(42),
			rhs:      int64(42),
			wantCost: 1, // ceil(1 * 0.1) = 1
		},
		{
			name:     "scalar_bool_not_equal",
			tracker:  neTracker,
			lhs:      true,
			rhs:      false,
			wantCost: 1, // ceil(1 * 0.1) = 1
		},
		{
			name:     "scalar_double_equal",
			tracker:  eqTracker,
			lhs:      3.14,
			rhs:      3.14,
			wantCost: 1, // ceil(1 * 0.1) = 1
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := []ref.Val{
				adapter.NativeToValue(tc.lhs),
				adapter.NativeToValue(tc.rhs),
			}
			cost := tc.tracker(args, nil)
			if cost == nil {
				t.Errorf("cost got nil, wanted %d", tc.wantCost)
			} else if *cost != tc.wantCost {
				t.Errorf("cost got %d, wanted %d", *cost, tc.wantCost)
			}
		})
	}
}

func TestEqualsNotEqualsOverloadModels_Estimators(t *testing.T) {
	estimators := StandardOverloadEstimators()
	eqEstimator := estimators[overloads.Equals]
	if eqEstimator == nil {
		t.Fatalf("missing estimator for Equals")
	}
	neEstimator := estimators[overloads.NotEquals]
	if neEstimator == nil {
		t.Fatalf("missing estimator for NotEquals")
	}

	tests := []struct {
		name      string
		estimator FunctionEstimator
		nodeA     AstNode
		nodeB     AstNode
		wantMin   uint64
		wantMax   uint64
	}{
		{
			name:      "int_list_nodes_equal",
			estimator: eqEstimator,
			nodeA:     &testAstNode{t: types.NewListType(types.IntType), size: &SizeEstimate{Min: 10, Max: 40}},
			nodeB:     &testAstNode{t: types.NewListType(types.IntType), size: &SizeEstimate{Min: 20, Max: 30}},
			wantMin:   1, // ceil(min(10, 20) * 0.1) = 1
			wantMax:   3, // ceil(min(40, 30) * 0.1) = 3
		},
		{
			name:      "map_nodes_not_equal",
			estimator: neEstimator,
			nodeA:     &testAstNode{t: types.NewMapType(types.StringType, types.IntType), size: &SizeEstimate{Min: 15, Max: 50}},
			nodeB:     &testAstNode{t: types.NewMapType(types.StringType, types.IntType), size: &SizeEstimate{Min: 5, Max: 60}},
			wantMin:   1, // ceil(min(15, 5) * 0.1) = 1
			wantMax:   5, // ceil(min(50, 60) * 0.1) = 5
		},
		{
			name:      "scalar_int_nodes_equal",
			estimator: eqEstimator,
			nodeA:     &testAstNode{t: types.IntType, size: &SizeEstimate{Min: 1, Max: 1}},
			nodeB:     &testAstNode{t: types.IntType, size: &SizeEstimate{Min: 1, Max: 1}},
			wantMin:   1,
			wantMax:   1,
		},
		{
			name:      "string_nodes_equal",
			estimator: eqEstimator,
			nodeA:     &testAstNode{t: types.StringType, size: &SizeEstimate{Min: 10, Max: 40}},
			nodeB:     &testAstNode{t: types.StringType, size: &SizeEstimate{Min: 20, Max: 30}},
			wantMin:   1, // ceil(min(10, 20) * 0.1) = 1
			wantMax:   3, // ceil(min(40, 30) * 0.1) = 3
		},
		{
			name:      "bytes_nodes_equal",
			estimator: eqEstimator,
			nodeA:     &testAstNode{t: types.BytesType, size: &SizeEstimate{Min: 20, Max: 80}},
			nodeB:     &testAstNode{t: types.BytesType, size: &SizeEstimate{Min: 10, Max: 100}},
			wantMin:   1, // ceil(min(20, 10) * 0.1) = 1
			wantMax:   8, // ceil(min(80, 100) * 0.1) = 8
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.estimator(nil, nil, []AstNode{tc.nodeA, tc.nodeB})
			if res == nil {
				t.Fatalf("estimator returned nil")
			}
			if res.CostEstimate.Min != tc.wantMin || res.CostEstimate.Max != tc.wantMax {
				t.Errorf("estimator cost got [%d, %d], wanted [%d, %d]",
					res.CostEstimate.Min, res.CostEstimate.Max, tc.wantMin, tc.wantMax)
			}
		})
	}
}

type testAstNode struct {
	path []string
	t    *types.Type
	size *SizeEstimate
}

func (n *testAstNode) Path() []string              { return n.path }
func (n *testAstNode) Type() *types.Type           { return n.t }
func (n *testAstNode) Expr() ast.Expr              { return nil }
func (n *testAstNode) ComputedSize() *SizeEstimate { return n.size }

func TestOverloadConstructors(t *testing.T) {
	tests := []struct {
		name       string
		model      OverloadModel
		wantID     string
		wantMember bool
		wantTarget bool
	}{
		{
			name: "custom_global_overload",
			model: Overload("custom_global",
				EvalCost(Scale(Arg(0), 1.5)),
				ResultSize(Sum(Arg(0), Const(1))),
			),
			wantID:     "custom_global",
			wantMember: false,
			wantTarget: false,
		},
		{
			name: "custom_member_overload",
			model: MemberOverload("custom_member",
				EvalCost(Scale(Arg(0), 2.0)),
			),
			wantID:     "custom_member",
			wantMember: true,
			wantTarget: true,
		},
		{
			name: "inferred_target_overload",
			model: Overload("inferred_member",
				EvalCost(Scale(Target(), 2.0)),
			),
			wantID:     "inferred_member",
			wantMember: false,
			wantTarget: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.model.ID != tc.wantID {
				t.Errorf("ID = %q, want %q", tc.model.ID, tc.wantID)
			}
			if tc.model.IsMember != tc.wantMember {
				t.Errorf("IsMember = %t, want %t", tc.model.IsMember, tc.wantMember)
			}
			if tc.model.hasTarget() != tc.wantTarget {
				t.Errorf("hasTarget() = %t, want %t", tc.model.hasTarget(), tc.wantTarget)
			}
		})
	}
}
