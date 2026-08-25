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

	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/overloads"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

type testEvalContext struct {
	estimator  Estimator
	strategy   SizingStrategy
	args       []SizeEstimate
	receiver   *SizeEstimate
	result     *SizeEstimate
	targetType *types.Type
	argTypes   []*types.Type
}

func (t *testEvalContext) Arg(index int) (SizeEstimate, bool) {
	if index < len(t.args) {
		return t.args[index], true
	}
	return UnknownSizeEstimate(), false
}

func (t *testEvalContext) Target() (SizeEstimate, bool) {
	if t.receiver != nil {
		return *t.receiver, true
	}
	return UnknownSizeEstimate(), false
}

func (t *testEvalContext) Result() (SizeEstimate, bool) {
	if t.result != nil {
		return *t.result, true
	}
	return UnknownSizeEstimate(), false
}

func (t *testEvalContext) Estimator() Estimator {
	return t.estimator
}

func (t *testEvalContext) Size(node AstNode) SizeEstimate {
	if node == nil {
		return UnknownSizeEstimate()
	}
	if node.ComputedSize() != nil {
		return *node.ComputedSize()
	}
	if t.strategy != nil {
		if sz, ok := t.strategy.EstimateSize(t, node); ok {
			return sz
		}
	}
	if t.estimator != nil {
		if sz := t.estimator.EstimateSize(node); sz != nil {
			return *sz
		}
	}
	if sz := computeTypeSize(node.Type()); sz != nil {
		return *sz
	}
	return UnknownSizeEstimate()
}

func (t *testEvalContext) TargetType() (*types.Type, bool) {
	if t.targetType != nil {
		return t.targetType, true
	}
	return nil, false
}

func (t *testEvalContext) ArgType(index int) (*types.Type, bool) {
	if index < len(t.argTypes) {
		return t.argTypes[index], true
	}
	return nil, false
}

func TestQuantityExprs(t *testing.T) {
	elem0 := FixedSizeEstimate(7)
	key1 := FixedSizeEstimate(3)
	elem1 := FixedSizeEstimate(12)
	rcvElem := FixedSizeEstimate(15)
	rcvKey := FixedSizeEstimate(4)

	arg0 := ListSizeEstimate(RangedSizeEstimate(2, 10), elem0)
	arg1 := MapSizeEstimate(RangedSizeEstimate(3, 5), key1, elem1)
	rcv := MapSizeEstimate(RangedSizeEstimate(4, 8), rcvKey, rcvElem)

	ctx := &testEvalContext{
		args: []SizeEstimate{
			arg0,
			arg1,
		},
		receiver:   &rcv,
		targetType: types.NewListType(types.StringType),
		argTypes:   []*types.Type{types.NewListType(types.IntType)},
	}

	tests := []struct {
		name     string
		expr     QuantityExpr
		expected SizeEstimate
	}{
		{
			name:     "Const",
			expr:     Const(42),
			expected: FixedSizeEstimate(42),
		},
		{
			name:     "Arg",
			expr:     Arg(0),
			expected: arg0,
		},
		{
			name:     "ArgElem",
			expr:     ArgElem(0),
			expected: elem0,
		},
		{
			name:     "ArgKey",
			expr:     ArgKey(1),
			expected: key1,
		},
		{
			name:     "Target",
			expr:     Target(),
			expected: rcv,
		},
		{
			name:     "TargetElem",
			expr:     TargetElem(),
			expected: rcvElem,
		},
		{
			name:     "TargetKey",
			expr:     TargetKey(),
			expected: rcvKey,
		},
		{
			name:     "Add",
			expr:     Sum(Arg(0), Arg(1)),
			expected: arg0.Add(arg1),
		},
		{
			name:     "Mul",
			expr:     Mul(Arg(0), Arg(1)),
			expected: RangedSizeEstimate(6, 50),
		},
		{
			name:     "Scale",
			expr:     Scale(Arg(0), 0.5),
			expected: ListSizeEstimate(RangedSizeEstimate(1, 5), elem0),
		},
		{
			name:     "Square",
			expr:     Square(Arg(1)),
			expected: RangedSizeEstimate(9, 25),
		},
		{
			name:     "Square_ScaleBy",
			expr:     Scale(Square(Arg(1)), 2.0),
			expected: RangedSizeEstimate(18, 50),
		},
		{
			name:     "Min",
			expr:     Min(Arg(0), Arg(1)),
			expected: SizeEstimate{Min: 1, Max: 5},
		},
		{
			name:     "Max",
			expr:     Max(Arg(0), Arg(1)),
			expected: RangedSizeEstimate(3, 10),
		},
		{
			name:     "Union",
			expr:     Union(Arg(0), Arg(1)),
			expected: arg0.Union(arg1),
		},
		{
			name:     "Intersect",
			expr:     Intersect(Arg(0), Arg(1)),
			expected: RangedSizeEstimate(3, 5),
		},
		{
			name:     "Ranged_StringToBytes",
			expr:     Ranged(Arg(0), Scale(Arg(0), 4.0)),
			expected: RangedSizeEstimate(2, 40),
		},
		{
			name:     "Ranged_BytesToString",
			expr:     Ranged(Scale(Arg(0), 0.25), Arg(0)),
			expected: RangedSizeEstimate(1, 10),
		},
		{
			name:     "Ranged_ExtQuoteString",
			expr:     Ranged(Sum(Arg(0), Const(2)), Sum(Scale(Arg(0), 2.0), Const(2))),
			expected: RangedSizeEstimate(4, 22),
		},
		{
			name:     "AtMost",
			expr:     AtMost(Arg(0)),
			expected: RangedSizeEstimate(0, 10),
		},
		{
			name:     "List",
			expr:     List(AtMost(Arg(0)), Arg(0)),
			expected: ListSizeEstimate(RangedSizeEstimate(0, 10), arg0),
		},
		{
			name:     "ElemOf",
			expr:     ElemOf(Arg(0)),
			expected: elem0,
		},
		{
			name:     "KeyOf",
			expr:     KeyOf(Arg(1)),
			expected: key1,
		},
		{
			name:     "Map",
			expr:     Map(Arg(1), KeyOf(Arg(1)), ElemOf(Arg(1))),
			expected: arg1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.expr.estimate(ctx)
			if got.Min != tc.expected.Min || got.Max != tc.expected.Max {
				t.Errorf("estimate got [%v, %v], wanted [%v, %v]", got.Min, got.Max, tc.expected.Min, tc.expected.Max)
			}
			if (got.Elem == nil) != (tc.expected.Elem == nil) {
				t.Errorf("estimate Elem nil mismatch: got %v, wanted %v", got.Elem, tc.expected.Elem)
			} else if got.Elem != nil && (got.Elem.Min != tc.expected.Elem.Min || got.Elem.Max != tc.expected.Elem.Max) {
				t.Errorf("estimate Elem got %v, wanted %v", got.Elem, tc.expected.Elem)
			}
			if (got.Key == nil) != (tc.expected.Key == nil) {
				t.Errorf("estimate Key nil mismatch: got %v, wanted %v", got.Key, tc.expected.Key)
			} else if got.Key != nil && (got.Key.Min != tc.expected.Key.Min || got.Key.Max != tc.expected.Key.Max) {
				t.Errorf("estimate Key got %v, wanted %v", got.Key, tc.expected.Key)
			}
		})
	}

	trackCtx := &testTrackContext{
		args:     []uint64{10, 5},
		receiver: 8,
	}

	scalarTests := []struct {
		name     string
		expr     QuantityExpr
		expected uint64
	}{
		{name: "Const", expr: Const(42), expected: 42},
		{name: "Arg", expr: Arg(0), expected: 10},
		{name: "Target", expr: Target(), expected: 8},
		{name: "Add", expr: Sum(Arg(0), Arg(1)), expected: 15},
		{name: "Mul", expr: Mul(Arg(0), Arg(1)), expected: 50},
		{name: "Scale", expr: Scale(Arg(0), 1.5), expected: 15},
		{name: "Square", expr: Square(Arg(1)), expected: 25},
		{name: "Min", expr: Min(Arg(0), Arg(1)), expected: 5},
		{name: "Max", expr: Max(Arg(0), Arg(1)), expected: 10},
		{name: "Ranged", expr: Ranged(Arg(1), Arg(0)), expected: 10},
	}

	for _, tc := range scalarTests {
		t.Run("Track_"+tc.name, func(t *testing.T) {
			got := tc.expr.track(trackCtx)
			if got != tc.expected {
				t.Errorf("track got %v, wanted %v", got, tc.expected)
			}
		})
	}
}

type testTrackContext struct {
	args      []uint64
	receiver  uint64
	result    uint64
	estimator ActualCostEstimator
}

func (t *testTrackContext) Arg(index int) uint64 {
	if index < len(t.args) {
		return t.args[index]
	}
	return 0
}

func (t *testTrackContext) Target() uint64 {
	return t.receiver
}

func (t *testTrackContext) Result() uint64 {
	return t.result
}

func (t *testTrackContext) Estimator() ActualCostEstimator {
	return t.estimator
}

func (t *testTrackContext) Size(value ref.Val) uint64 {
	return ActualSize(value)
}

func (t *testTrackContext) TargetType() (*types.Type, bool) {
	return nil, false
}

func (t *testTrackContext) ArgType(index int) (*types.Type, bool) {
	return nil, false
}

func TestStandardOverloadModels(t *testing.T) {
	estimators := StandardOverloadEstimators()
	trackers := StandardOverloadTrackers()

	if len(estimators) != len(StandardOverloadModels) {
		t.Errorf("got %d estimators, wanted %d", len(estimators), len(StandardOverloadModels))
	}
	if len(trackers) != len(StandardOverloadModels) {
		t.Errorf("got %d trackers, wanted %d", len(trackers), len(StandardOverloadModels))
	}

	// Test a tracker execution: InList
	inListTracker := trackers[overloads.InList]
	if inListTracker == nil {
		t.Fatalf("missing tracker for InList")
	}
	args := []ref.Val{
		types.String("item"),
		types.DefaultTypeAdapter.NativeToValue([]string{"a", "b", "c"}),
	}
	cost := inListTracker(args, nil)
	if cost == nil {
		t.Errorf("InList cost = nil, wanted 3")
	} else if *cost != 3 {
		t.Errorf("InList cost = %d, wanted 3", *cost)
	}
}

func TestEqualsNotEqualsOverloadModels(t *testing.T) {
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
			name:     "int_list_equal",
			tracker:  eqTracker,
			lhs:      []int64{1, 2, 3},
			rhs:      []int64{1, 2, 3},
			wantCost: 1, // ceil(3 * 0.1) = 1
		},
		{
			name:     "large_int_list_equal",
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
			name:     "bytes_equal",
			tracker:  eqTracker,
			lhs:      []byte("hello"),
			rhs:      []byte("hello"),
			wantCost: 1, // ceil(5 * 0.1) = 1
		},
		{
			name:     "large_bytes_not_equal",
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

	// Test estimators for Equals and NotEquals with non-string AST nodes
	estimators := StandardOverloadEstimators()
	eqEstimator := estimators[overloads.Equals]
	if eqEstimator == nil {
		t.Fatalf("missing estimator for Equals")
	}
	neEstimator := estimators[overloads.NotEquals]
	if neEstimator == nil {
		t.Fatalf("missing estimator for NotEquals")
	}

	estTests := []struct {
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

	for _, tc := range estTests {
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
	global := Overload("custom_global",
		EvalCost(Scale(Arg(0), 1.5)),
		ResultSize(Sum(Arg(0), Const(1))),
	)
	if global.ID != "custom_global" {
		t.Errorf("got ID %q, wanted custom_global", global.ID)
	}
	if global.IsMember {
		t.Errorf("expected IsMember to be false")
	}
	if global.hasTarget() {
		t.Errorf("expected hasTarget to be false")
	}

	member := MemberOverload("custom_member",
		EvalCost(Scale(Arg(0), 2.0)),
	)
	if !member.IsMember {
		t.Errorf("expected IsMember to be true")
	}
	if !member.hasTarget() {
		t.Errorf("expected hasTarget to be true")
	}

	inferredMember := Overload("inferred_member",
		EvalCost(Scale(Target(), 2.0)),
	)
	if !inferredMember.hasTarget() {
		t.Errorf("expected hasTarget to be true when Target() is present")
	}
}

type customTrackerSizingStrategy struct {
	defaultSizingStrategy
}

func (customTrackerSizingStrategy) TrackSize(ctx TrackContext, value ref.Val) (uint64, bool) {
	if s, ok := value.(types.String); ok {
		return uint64(len(string(s)) * 2), true
	}
	return ActualSize(value), true
}

type simpleCall struct {
	function   string
	overloadID string
}

func (s simpleCall) Function() string   { return s.function }
func (s simpleCall) OverloadID() string { return s.overloadID }

func TestTrackerWithSizingStrategy(t *testing.T) {
	strategy := customTrackerSizingStrategy{}
	tracker, err := NewTracker(nil, TrackerSizingStrategy(strategy))
	if err != nil {
		t.Fatalf("NewTracker failed: %v", err)
	}
	// startsWithString cost = Scale(Arg(0), 0.1)
	// target = "target", arg = "hello_world" (len 11)
	// with custom sizing, "hello_world" has size 11 * 2 = 22
	// cost = ceil(22 * 0.1) = 3
	c := tracker.CostCall(simpleCall{overloadID: overloads.StartsWithString}, []ref.Val{types.String("target"), types.String("hello_world")}, types.True)
	if c != 3 {
		t.Errorf("got cost %d, wanted 3", c)
	}
}

func TestDefaultAndAggregateSizingStrategies(t *testing.T) {
	defaultStrat := DefaultSizingStrategy()
	aggStrat := AggregateSizingStrategy()
	aggCustomStrat := AggregateSizingStrategy(types.SizeCalculatorStringUnitLength(5))

	adapter := types.DefaultTypeAdapter
	strVal := types.String("hello_world")                                          // len 11
	nestedList := adapter.NativeToValue([][]int{{1, 2}, {3, 4}})                   // outer len 2, each inner len 2
	nestedMap := adapter.NativeToValue(map[string][]int{"a": {1, 2}, "b": {3, 4}}) // 2 keys, 2 list values

	// Default strategy tracking (flat)
	if sz, ok := defaultStrat.TrackSize(nil, strVal); !ok || sz != 11 {
		t.Errorf("defaultStrat.TrackSize(str) = (%d, %t), want (11, true)", sz, ok)
	}
	if sz, ok := defaultStrat.TrackSize(nil, nestedList); !ok || sz != 2 {
		t.Errorf("defaultStrat.TrackSize(nestedList) = (%d, %t), want (2, true)", sz, ok)
	}
	if sz, ok := defaultStrat.TrackSize(nil, nestedMap); !ok || sz != 2 {
		t.Errorf("defaultStrat.TrackSize(nestedMap) = (%d, %t), want (2, true)", sz, ok)
	}

	// Aggregate strategy tracking (recursive)
	// strVal: 11 bytes at 1 unit/byte = 11 units
	if sz, ok := aggStrat.TrackSize(nil, strVal); !ok || sz != 11 {
		t.Errorf("aggStrat.TrackSize(str) = (%d, %t), want (11, true)", sz, ok)
	}
	// nestedList: 1 (outer list header) + (1 + 2) (inner list 1) + (1 + 2) (inner list 2) = 7
	if sz, ok := aggStrat.TrackSize(nil, nestedList); !ok || sz != 7 {
		t.Errorf("aggStrat.TrackSize(nestedList) = (%d, %t), want (7, true)", sz, ok)
	}
	// nestedMap: 1 (map) + 2 (key strings of len 1) + 2 * (1 + 2) (two inner lists) = 9
	if sz, ok := aggStrat.TrackSize(nil, nestedMap); !ok || sz != 9 {
		t.Errorf("aggStrat.TrackSize(nestedMap) = (%d, %t), want (9, true)", sz, ok)
	}

	// Custom aggregate strategy: stringUnitLength = 5 -> (11 + 4) / 5 = 3 units
	if sz, ok := aggCustomStrat.TrackSize(nil, strVal); !ok || sz != 3 {
		t.Errorf("aggCustomStrat.TrackSize(str) = (%d, %t), want (3, true)", sz, ok)
	}

	// FunctionTracker with aggregate sizing strategy
	model := MemberOverload(overloads.StartsWithString,
		EvalCost(Scale(Arg(0), 0.1)),
	)
	trackerFn := model.FunctionTrackerWithOptions(aggStrat)
	cost := trackerFn([]ref.Val{types.String("target"), strVal}, types.True)
	// strVal aggregate size = 11 -> ceil(11 * 0.1) = 2
	if cost == nil || *cost != 2 {
		t.Errorf("got trackerFn cost %v, want 2", cost)
	}

	// Recursive path exploration during EstimateSize:
	nestedType := types.NewListType(types.NewListType(types.StringType))
	nestedNode := NewAstNode(nil, []string{"nested"}, nestedType, nil)

	hints := map[string]uint64{
		"nested":               5,
		"nested.@items":        10,
		"nested.@items.@items": 20,
	}
	estimator := testHintsEstimator{hints: hints}

	// Default strategy resolves 1-level path hints but does not recursively explore deeper nested container paths:
	defaultCtx := &testEvalContext{estimator: estimator, strategy: defaultStrat}
	defaultEst, ok := defaultStrat.EstimateSize(defaultCtx, nestedNode)
	if !ok || defaultEst.Max != 5 || defaultEst.Elem == nil || defaultEst.Elem.Max != 10 || defaultEst.Elem.Elem != nil {
		t.Errorf("defaultStrat.EstimateSize(nested) = (%v, %t), want (Max: 5, Elem.Max: 10, Elem.Elem: nil, true)", defaultEst, ok)
	}

	// Aggregate strategy recursively explores nested @items paths (@items.@items):
	aggCtx := &testEvalContext{estimator: estimator, strategy: aggStrat}
	aggEst, ok := aggStrat.EstimateSize(aggCtx, nestedNode)
	if !ok || aggEst.Max != 5 {
		t.Fatalf("aggStrat.EstimateSize(nested) = (%v, %t), want (Max: 5, true)", aggEst, ok)
	}
	if aggEst.Elem == nil || aggEst.Elem.Max != 10 {
		t.Fatalf("aggEst.Elem = %v, want Max 10", aggEst.Elem)
	}
	if aggEst.Elem.Elem == nil || aggEst.Elem.Elem.Max != 20 {
		t.Fatalf("aggEst.Elem.Elem = %v, want Max 20", aggEst.Elem.Elem)
	}
}

type testHintsEstimator struct {
	hints map[string]uint64
}

func (t testHintsEstimator) EstimateSize(node AstNode) *SizeEstimate {
	if node == nil || len(node.Path()) == 0 {
		return nil
	}
	key := strings.Join(node.Path(), ".")
	if val, ok := t.hints[key]; ok {
		sz := FixedSizeEstimate(val)
		return &sz
	}
	return nil
}

func (t testHintsEstimator) EstimateCallCost(function, overloadID string, target *AstNode, args []AstNode) *CallEstimate {
	return nil
}
