// Copyright 2022 Google LLC
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

package cost_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/cost"
	"cel.dev/cel-go/common/decls"
	"cel.dev/cel-go/common/overloads"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/ext"

	proto3pb "cel.dev/cel-go/test/proto3pb"
)

// testCelEnv is the CEL environment shared by the cost estimator and tracker tests.
//
// Beyond the standard library it supplies the ext macros and functions these tests
// exercise: cel.bind from ext.Bindings, and the two-variable comprehensions (all, exists,
// existsOne, transformList, transformMap, transformMapEntry) from ext.TwoVarComprehensions
// along with the cel.@mapInsert function they expand to. These were previously hand-rolled
// in this file.
var testCelEnv = func() *cel.Env {
	env, err := cel.NewEnv(
		cel.Types(&proto3pb.TestAllTypes{}),
		cel.CrossTypeNumericComparisons(true),
		cel.OptionalTypes(),
		ext.Bindings(),
		ext.TwoVarComprehensions(),
		cel.Function("max",
			cel.MemberOverload("list_bytes_max",
				[]*cel.Type{cel.ListType(cel.BytesType)}, cel.BytesType)),
	)
	if err != nil {
		panic(fmt.Sprintf("cel.NewEnv() failed: %v", err))
	}
	return env
}()

// compile type checks an expression against the shared test environment extended with the
// given variable declarations, and returns the checked AST.
func compile(t *testing.T, expr string, vars ...*decls.VariableDecl) *ast.AST {
	t.Helper()
	env, err := testCelEnv.Extend(cel.VariableDecls(vars...))
	if err != nil {
		t.Fatalf("env.Extend() failed: %v", err)
	}
	checked, iss := env.Compile(expr)
	if iss.Err() != nil {
		t.Fatalf("env.Compile(%q) failed: %v", expr, iss.Err())
	}
	return checked.NativeRep()
}

func TestCost(t *testing.T) {
	allTypes := types.NewObjectType("google.expr.proto3.test.TestAllTypes")
	allList := types.NewListType(allTypes)
	intList := types.NewListType(types.IntType)
	nestedList := types.NewListType(allList)

	allMap := types.NewMapType(types.StringType, allTypes)
	nestedMap := types.NewMapType(types.StringType, allMap)

	zeroCost := cost.CostEstimate{}
	oneCost := cost.FixedCostEstimate(1)
	cases := []struct {
		name    string
		expr    string
		vars    []*decls.VariableDecl
		hints   map[string]uint64
		options []cost.Option
		wanted  cost.CostEstimate
	}{
		{
			name:   "const",
			expr:   `"Hello World!"`,
			wanted: zeroCost,
		},
		{
			name:   "identity",
			expr:   `input`,
			vars:   []*decls.VariableDecl{decls.NewVariable("input", intList)},
			wanted: cost.CostEstimate{Min: 1, Max: 1},
		},
		{
			name: "select: map",
			expr: `input['key']`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", types.NewMapType(types.StringType, types.StringType))},
			wanted: cost.CostEstimate{Min: 2, Max: 2},
		},
		{
			name:   "select: field",
			expr:   `input.single_int32`,
			vars:   []*decls.VariableDecl{decls.NewVariable("input", allTypes)},
			wanted: cost.CostEstimate{Min: 2, Max: 2},
		},
		{
			name:    "select: field test only no has() cost",
			expr:    `has(input.single_int32)`,
			vars:    []*decls.VariableDecl{decls.NewVariable("input", types.NewObjectType("google.expr.proto3.test.TestAllTypes"))},
			wanted:  cost.CostEstimate{Min: 1, Max: 1},
			options: []cost.Option{cost.PresenceTestHasCost(false)},
		},
		{
			name:   "select: field test only",
			expr:   `has(input.single_int32)`,
			vars:   []*decls.VariableDecl{decls.NewVariable("input", types.NewObjectType("google.expr.proto3.test.TestAllTypes"))},
			wanted: cost.CostEstimate{Min: 2, Max: 2},
		},
		{
			name:    "select: non-proto field test has() cost",
			expr:    `has(input.testAttr.nestedAttr)`,
			vars:    []*decls.VariableDecl{decls.NewVariable("input", nestedMap)},
			wanted:  cost.CostEstimate{Min: 3, Max: 3},
			options: []cost.Option{cost.PresenceTestHasCost(true)},
		},
		{
			name:    "select: non-proto field test no has() cost",
			expr:    `has(input.testAttr.nestedAttr)`,
			vars:    []*decls.VariableDecl{decls.NewVariable("input", nestedMap)},
			wanted:  cost.CostEstimate{Min: 2, Max: 2},
			options: []cost.Option{cost.PresenceTestHasCost(false)},
		},
		{
			name:   "select: non-proto field test",
			expr:   `has(input.testAttr.nestedAttr)`,
			vars:   []*decls.VariableDecl{decls.NewVariable("input", nestedMap)},
			wanted: cost.CostEstimate{Min: 3, Max: 3},
		},
		{
			name:   "estimated function call",
			expr:   `input.getFullYear()`,
			vars:   []*decls.VariableDecl{decls.NewVariable("input", types.TimestampType)},
			wanted: cost.CostEstimate{Min: 8, Max: 8},
		},
		{
			name:   "create list",
			expr:   `[1, 2, 3]`,
			wanted: cost.CostEstimate{Min: 10, Max: 10},
		},
		{
			name:   "create struct",
			expr:   `google.expr.proto3.test.TestAllTypes{single_int32: 1, single_float: 3.14, single_string: 'str'}`,
			wanted: cost.CostEstimate{Min: 40, Max: 40},
		},
		{
			name:   "create map",
			expr:   `{"a": 1, "b": 2, "c": 3}`,
			wanted: cost.CostEstimate{Min: 30, Max: 30},
		},
		{
			name:   "all comprehension",
			vars:   []*decls.VariableDecl{decls.NewVariable("input", allList)},
			hints:  map[string]uint64{"input": 100},
			expr:   `input.all(x, true)`,
			wanted: cost.CostEstimate{Min: 2, Max: 302},
		},
		{
			name:   "nested all comprehension",
			vars:   []*decls.VariableDecl{decls.NewVariable("input", nestedList)},
			hints:  map[string]uint64{"input": 50, "input.@items": 10},
			expr:   `input.all(x, x.all(y, true))`,
			wanted: cost.CostEstimate{Min: 2, Max: 1752},
		},
		{
			name:   "all comprehension on literal",
			expr:   `[1, 2, 3].all(x, true)`,
			wanted: cost.CostEstimate{Min: 20, Max: 20},
		},
		{
			name:   "variable cost function",
			vars:   []*decls.VariableDecl{decls.NewVariable("input", types.StringType)},
			hints:  map[string]uint64{"input": 500},
			expr:   `input.matches('[0-9]')`,
			wanted: cost.CostEstimate{Min: 3, Max: 103},
		},
		{
			name:   "variable cost function with constant",
			expr:   `'123'.matches('[0-9]')`,
			wanted: cost.CostEstimate{Min: 2, Max: 2},
		},
		{
			name:   "or",
			expr:   `true || false`,
			wanted: zeroCost,
		},
		{
			name: "or accumulated branch cost",
			expr: `a || b || c || d`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("a", types.BoolType),
				decls.NewVariable("b", types.BoolType),
				decls.NewVariable("c", types.BoolType),
				decls.NewVariable("d", types.BoolType),
			},
			wanted: cost.CostEstimate{Min: 1, Max: 4},
		},
		{
			name:   "and",
			expr:   `true && false`,
			wanted: zeroCost,
		},
		{
			name: "and accumulated branch cost",
			expr: `a && b && c && d`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("a", types.BoolType),
				decls.NewVariable("b", types.BoolType),
				decls.NewVariable("c", types.BoolType),
				decls.NewVariable("d", types.BoolType),
			},
			wanted: cost.CostEstimate{Min: 1, Max: 4},
		},
		{
			name:   "lt",
			expr:   `1 < 2`,
			wanted: oneCost,
		},
		{
			name:   "lte",
			expr:   `1 <= 2`,
			wanted: oneCost,
		},
		{
			name:   "eq",
			expr:   `1 == 2`,
			wanted: oneCost,
		},
		{
			name:   "gt",
			expr:   `2 > 1`,
			wanted: oneCost,
		},
		{
			name:   "gte",
			expr:   `2 >= 1`,
			wanted: oneCost,
		},
		{
			name:   "in",
			expr:   `2 in [1, 2, 3]`,
			wanted: cost.CostEstimate{Min: 13, Max: 13},
		},
		{
			name:   "plus",
			expr:   `1 + 1`,
			wanted: oneCost,
		},
		{
			name:   "minus",
			expr:   `1 - 1`,
			wanted: oneCost,
		},
		{
			name:   "/",
			expr:   `1 / 1`,
			wanted: oneCost,
		},
		{
			name:   "/",
			expr:   `1 * 1`,
			wanted: oneCost,
		},
		{
			name:   "%",
			expr:   `1 % 1`,
			wanted: oneCost,
		},
		{
			name:   "ternary",
			expr:   `true ? 1 : 2`,
			wanted: zeroCost,
		},
		{
			name:   "string size",
			expr:   `size("123")`,
			wanted: oneCost,
		},
		{
			name:   "bytes size",
			expr:   `size(b"123")`,
			wanted: oneCost,
		},
		{
			name:   "bytes to string conversion",
			vars:   []*decls.VariableDecl{decls.NewVariable("input", types.BytesType)},
			hints:  map[string]uint64{"input": 500},
			expr:   `string(input)`,
			wanted: cost.CostEstimate{Min: 1, Max: 51},
		},
		{
			name:  "bytes to string conversion equality",
			vars:  []*decls.VariableDecl{decls.NewVariable("input", types.BytesType)},
			hints: map[string]uint64{"input": 500},
			// equality check ensures that the resultSize calculation is included in cost
			expr:   `string(input) == string(input)`,
			wanted: cost.CostEstimate{Min: 3, Max: 152},
		},
		{
			name:   "string to bytes conversion",
			vars:   []*decls.VariableDecl{decls.NewVariable("input", types.StringType)},
			hints:  map[string]uint64{"input": 500},
			expr:   `bytes(input)`,
			wanted: cost.CostEstimate{Min: 1, Max: 51},
		},
		{
			name:  "string to bytes conversion equality",
			vars:  []*decls.VariableDecl{decls.NewVariable("input", types.StringType)},
			hints: map[string]uint64{"input": 500},
			// equality check ensures that the resultSize calculation is included in cost
			expr:   `bytes(input) == bytes(input)`,
			wanted: cost.CostEstimate{Min: 3, Max: 302},
		},
		{
			name:   "int to string conversion",
			expr:   `string(1)`,
			wanted: cost.CostEstimate{Min: 1, Max: 1},
		},
		{
			name: "contains",
			expr: `input.contains(arg1)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", types.StringType),
				decls.NewVariable("arg1", types.StringType),
			},
			hints:  map[string]uint64{"input": 500, "arg1": 500},
			wanted: cost.CostEstimate{Min: 2, Max: 2502},
		},
		{
			name: "matches",
			expr: `input.matches('\\d+a\\d+b')`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", types.StringType),
			},
			hints:  map[string]uint64{"input": 500},
			wanted: cost.CostEstimate{Min: 3, Max: 103},
		},
		{
			name: "matches global",
			expr: `matches(input, '\\d+a\\d+b')`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", types.StringType),
			},
			hints:  map[string]uint64{"input": 500},
			wanted: cost.CostEstimate{Min: 3, Max: 103},
		},
		{
			name: "startsWith",
			expr: `input.startsWith(arg1)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", types.StringType),
				decls.NewVariable("arg1", types.StringType),
			},
			hints:  map[string]uint64{"arg1": 500},
			wanted: cost.CostEstimate{Min: 2, Max: 52},
		},
		{
			name: "endsWith",
			expr: `input.endsWith(arg1)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", types.StringType),
				decls.NewVariable("arg1", types.StringType),
			},
			hints:  map[string]uint64{"arg1": 500},
			wanted: cost.CostEstimate{Min: 2, Max: 52},
		},
		{
			name: "size receiver",
			expr: `input.size()`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", types.StringType),
			},
			wanted: cost.CostEstimate{Min: 2, Max: 2},
		},
		{
			name: "size",
			expr: `size(input)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", types.StringType),
			},
			wanted: cost.CostEstimate{Min: 2, Max: 2},
		},
		{
			name: "ternary eval",
			expr: `(x > 2 ? input1 : input2).all(y, true)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("x", types.IntType),
				decls.NewVariable("input1", allList),
				decls.NewVariable("input2", allList),
			},
			hints:  map[string]uint64{"input1": 1, "input2": 1},
			wanted: cost.CostEstimate{Min: 4, Max: 7},
		},
		{
			name: "comprehension over map",
			expr: `input.all(k, input[k].single_int32 > 3)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", allMap),
			},
			hints:  map[string]uint64{"input": 10},
			wanted: cost.CostEstimate{Min: 2, Max: 82},
		},
		{
			name: "comprehension over nested map of maps",
			expr: `input.all(k, input[k].all(x, true))`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", nestedMap),
			},
			hints:  map[string]uint64{"input": 5, "input.@values": 10},
			wanted: cost.CostEstimate{Min: 2, Max: 187},
		},
		{
			name: "string size of map keys",
			expr: `input.all(k, k.contains(k))`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", nestedMap),
			},
			hints:  map[string]uint64{"input": 5, "input.@keys": 10},
			wanted: cost.CostEstimate{Min: 2, Max: 32},
		},
		{
			name: "comprehension variable shadowing",
			expr: `input.all(k, input[k].all(k, true) && k.contains(k))`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", nestedMap),
			},
			hints:  map[string]uint64{"input": 2, "input.@values": 2, "input.@keys": 5},
			wanted: cost.CostEstimate{Min: 2, Max: 34},
		},
		{
			name: "comprehension variable shadowing",
			expr: `input.all(k, input[k].all(k, true) && k.contains(k))`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("input", nestedMap),
			},
			hints:  map[string]uint64{"input": 2, "input.@values": 2, "input.@keys": 5},
			wanted: cost.CostEstimate{Min: 2, Max: 34},
		},
		{
			name: "list concat",
			expr: `(list1 + list2).all(x, true)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("list1", types.NewListType(types.IntType)),
				decls.NewVariable("list2", types.NewListType(types.IntType)),
			},
			hints:  map[string]uint64{"list1": 10, "list2": 10},
			wanted: cost.CostEstimate{Min: 4, Max: 64},
		},
		{
			name: "str concat",
			expr: `"abcdefg".contains(str1 + str2)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("str1", types.StringType),
				decls.NewVariable("str2", types.StringType),
			},
			hints:  map[string]uint64{"str1": 10, "str2": 10},
			wanted: cost.CostEstimate{Min: 2, Max: 6},
		},
		{
			name: "str concat custom cost estimate",
			expr: `"abcdefg".contains(str1 + str2)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("str1", types.StringType),
				decls.NewVariable("str2", types.StringType),
			},
			hints: map[string]uint64{"str1": 10, "str2": 10},
			options: []cost.Option{
				cost.OverloadCostEstimate(overloads.ContainsString,
					func(estimator cost.Estimator, target *cost.AstNode, args []cost.AstNode) *cost.CallEstimate {
						if target != nil && len(args) == 1 {
							strSize := estimateSize(estimator, *target).MultiplyByCostFactor(0.2)
							subSize := estimateSize(estimator, args[0]).MultiplyByCostFactor(0.2)
							return &cost.CallEstimate{CostEstimate: strSize.Multiply(subSize)}
						}
						return nil
					}),
			},
			wanted: cost.CostEstimate{Min: 2, Max: 12},
		},
		{
			name: "list size comparison",
			expr: `list1.size() == list2.size()`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("list1", types.NewListType(types.IntType)),
				decls.NewVariable("list2", types.NewListType(types.IntType)),
			},
			wanted: cost.CostEstimate{Min: 5, Max: 5},
		},
		{
			name: "list size from ternary",
			expr: `x > y ? list1.size() : list2.size()`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("x", types.IntType),
				decls.NewVariable("y", types.IntType),
				decls.NewVariable("list1", types.NewListType(types.IntType)),
				decls.NewVariable("list2", types.NewListType(types.IntType)),
			},
			wanted: cost.CostEstimate{Min: 5, Max: 5},
		},
		{
			name: "list size from concat",
			expr: `([x, y] + list1 + list2).size()`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("x", types.IntType),
				decls.NewVariable("y", types.IntType),
				decls.NewVariable("list1", types.NewListType(types.IntType)),
				decls.NewVariable("list2", types.NewListType(types.IntType)),
			},
			hints: map[string]uint64{
				"list1": 10,
				"list2": 20,
			},
			wanted: cost.CostEstimate{Min: 17, Max: 17},
		},
		{
			name: "list cost tracking through comprehension",
			expr: `[list1, list2].exists(l, l.exists(v, v.startsWith('hi')))`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("list1", types.NewListType(types.StringType)),
				decls.NewVariable("list2", types.NewListType(types.StringType)),
			},
			hints: map[string]uint64{
				"list1":        10,
				"list1.@items": 64,
				"list2":        20,
				"list2.@items": 128,
			},
			wanted: cost.CostEstimate{Min: 21, Max: 265},
		},
		{
			name: "str endsWith equality",
			expr: `str1.endsWith("abcdefghijklmnopqrstuvwxyz") == str2.endsWith("abcdefghijklmnopqrstuvwxyz")`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("str1", types.StringType),
				decls.NewVariable("str2", types.StringType),
			},
			wanted: cost.CostEstimate{Min: 9, Max: 9},
		},
		{
			name:   "nested subexpression operators",
			expr:   `((5 != 6) == (1 == 2)) == ((3 <= 4) == (9 != 9))`,
			wanted: cost.CostEstimate{Min: 7, Max: 7},
		},
		{
			name: "str size estimate",
			expr: `string(timestamp1) == string(timestamp2)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("timestamp1", types.TimestampType),
				decls.NewVariable("timestamp2", types.TimestampType),
			},
			wanted: cost.CostEstimate{Min: 5, Max: 1844674407370955268},
		},
		{
			name: "timestamp equality check",
			expr: `timestamp1 == timestamp2`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("timestamp1", types.TimestampType),
				decls.NewVariable("timestamp2", types.TimestampType),
			},
			wanted: cost.CostEstimate{Min: 3, Max: 3},
		},
		{
			name: "duration inequality check",
			expr: `duration1 != duration2`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("duration1", types.DurationType),
				decls.NewVariable("duration2", types.DurationType),
			},
			wanted: cost.CostEstimate{Min: 3, Max: 3},
		},
		{
			name:   ".filter list literal",
			expr:   `[1,2,3,4,5].filter(x, x % 2 == 0)`,
			wanted: cost.CostEstimate{Min: 41, Max: 101},
		},
		{
			name:   ".map list literal",
			expr:   `[1,2,3,4,5].map(x, x)`,
			wanted: cost.CostEstimate{Min: 86, Max: 86},
		},
		{
			name:   ".map.filter list literal",
			expr:   `[1,2,3,4,5].map(x, x).filter(x, x % 2 == 0)`,
			wanted: cost.CostEstimate{Min: 117, Max: 177},
		},
		{
			name:   ".map.exists list literal",
			expr:   `[1,2,3,4,5].map(x, x).exists(x, x == 5) == true`,
			wanted: cost.CostEstimate{Min: 108, Max: 118},
		},
		{
			name:   ".map.map list literal",
			expr:   `[1,2,3,4,5].map(x, x).map(x, x)`,
			wanted: cost.CostEstimate{Min: 162, Max: 162},
		},
		{
			name:   ".map list literal selection",
			expr:   `[1,2,3,4,5].map(x, x)[4]`,
			wanted: cost.CostEstimate{Min: 88, Max: 88},
		},
		{
			name:   "nested array selection",
			expr:   `[[1,2],[1,2],[1,2],[1,2],[1,2]][4]`,
			wanted: cost.CostEstimate{Min: 62, Max: 62},
		},
		{
			name:   "nested map selection",
			expr:   `{'a': [1,2], 'b': [1,2], 'c': [1,2], 'd': [1,2], 'e': [1,2]}.b`,
			wanted: cost.CostEstimate{Min: 82, Max: 82},
		},
		{
			name:   "comprehension on nested list",
			expr:   `[[1, 1], [2, 2], [3, 3], [4, 4], [5, 5]].all(y, y.all(y, y == 1))`,
			wanted: cost.CostEstimate{Min: 76, Max: 136},
		},
		{
			name:   "comprehension on transformed nested list",
			expr:   `[1,2,3,4,5].map(x, [x, x]).all(y, y.all(y, y == 1))`,
			wanted: cost.CostEstimate{Min: 157, Max: 217},
		},
		{
			name:   "comprehension on nested literal list",
			expr:   `["a", "ab", "abc", "abcd", "abcde"].map(x, [x, x]).all(y, y.all(y, y.startsWith('a')))`,
			wanted: cost.CostEstimate{Min: 157, Max: 217},
		},
		{
			name: "comprehension on nested variable list",
			expr: `input.map(x, [x, x]).all(y, y.all(y, y.startsWith('a')))`,
			vars: []*decls.VariableDecl{decls.NewVariable("input", types.NewListType(types.StringType))},
			hints: map[string]uint64{
				"input":        5,
				"input.@items": 10,
			},
			wanted: cost.CostEstimate{Min: 13, Max: 208},
		},
		{
			name:   "comprehension chaining with concat",
			expr:   `[1,2,3,4,5].map(x, x).map(x, x) + [1]`,
			wanted: cost.CostEstimate{Min: 173, Max: 173},
		},
		{
			name:   "nested comprehension",
			expr:   `[1,2,3].all(i, i in [1,2,3].map(j, j + j))`,
			wanted: cost.CostEstimate{Min: 20, Max: 230},
		},
		{
			name:   "nested dyn comprehension",
			expr:   `dyn([1,2,3]).all(i, i in dyn([1,2,3]).map(j, j + j))`,
			wanted: cost.CostEstimate{Min: 21, Max: 234},
		},
		{
			name:   "literal map access",
			expr:   `{'hello': 'hi'}['hello'] != {'hello': 'bye'}['hello']`,
			wanted: cost.CostEstimate{Min: 65, Max: 65},
		},
		{
			name:   "literal list access",
			expr:   `['hello', 'hi'][0] != ['hello', 'bye'][1]`,
			wanted: cost.CostEstimate{Min: 25, Max: 25},
		},
		{
			// Optional index over a computed operand costs the same as its non-optional
			// counterpart: the planner qualifies a relative attribute in both cases.
			name:   "literal map optional access",
			expr:   `{'hello': 'hi'}[?'hello']`,
			wanted: cost.CostEstimate{Min: 32, Max: 32},
		},
		{
			name:   "literal map optional select",
			expr:   `{'hello': 'hi'}.?hello`,
			wanted: cost.CostEstimate{Min: 32, Max: 32},
		},
		{
			name:   "literal list optional access",
			expr:   `['hello', 'hi'][?0]`,
			wanted: cost.CostEstimate{Min: 12, Max: 12},
		},
		{
			// An optional select extends the attribute chain, so the trailing selection
			// must not be charged an extra attribute resolution.
			name: "optional select chain",
			expr: `self.?val1.val2`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.DynType)),
			},
			wanted: cost.CostEstimate{Min: 3, Max: 3},
		},
		{
			name: "optional index chain",
			expr: `self[?'val1'].val2`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.DynType)),
			},
			wanted: cost.CostEstimate{Min: 3, Max: 3},
		},
		{
			name:   "type call",
			expr:   `type(1)`,
			wanted: cost.CostEstimate{Min: 1, Max: 1},
		},
		{
			name: "type call variable",
			expr: `type(self.val1)`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.IntType)),
			},
			wanted: cost.CostEstimate{Min: 3, Max: 3},
		},
		{
			name: "type call variable equality",
			expr: `type(self.val1) == int`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.IntType)),
			},
			wanted: cost.CostEstimate{Min: 5, Max: 5},
		},
		{
			name:   "type literal equality cost",
			expr:   `type(1) == int`,
			wanted: cost.CostEstimate{Min: 3, Max: 3},
		},
		{
			name:   "type variable equality cost",
			expr:   `type(1) == int`,
			wanted: cost.CostEstimate{Min: 3, Max: 3},
		},
		{
			name: "namespace variable equality",
			expr: `self.val1 == 1.0`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("self.val1", types.DoubleType),
			},
			wanted: cost.CostEstimate{Min: 2, Max: 2},
		},
		{
			name: "simple map variable equality",
			expr: `self.val1 == 1.0`,
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.DoubleType)),
			},
			wanted: cost.CostEstimate{Min: 3, Max: 3},
		},
		{
			name: "date-time math",
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.TimestampType)),
			},
			expr:   `self.val1 == timestamp('2011-08-18T00:00:00.000+01:00') + duration('19h3m37s10ms')`,
			wanted: cost.FixedCostEstimate(6),
		},
		{
			name: "date-time math self-conversion",
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.TimestampType)),
			},
			expr:   `timestamp(self.val1) == timestamp('2011-08-18T00:00:00.000+01:00') + duration('19h3m37s10ms')`,
			wanted: cost.FixedCostEstimate(7),
		},
		{
			name: "boolean vars equal",
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.BoolType)),
			},
			expr:   `self.val1 != self.val2`,
			wanted: cost.FixedCostEstimate(5),
		},
		{
			name: "boolean var equals literal",
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.BoolType)),
			},
			expr:   `self.val1 != true`,
			wanted: cost.FixedCostEstimate(3),
		},
		{
			name: "double var equals literal",
			vars: []*decls.VariableDecl{
				decls.NewVariable("self", types.NewMapType(types.StringType, types.DoubleType)),
			},
			expr:   `self.val1 == 1.0`,
			wanted: cost.FixedCostEstimate(3),
		},
		{
			name: "bytes list max",
			expr: "[bytes('012345678901'), bytes('012345678901'), bytes('012345678901'), bytes('012345678901'), bytes('012345678901')].max()",
			options: []cost.Option{
				cost.OverloadCostEstimate("list_bytes_max",
					func(estimator cost.Estimator, target *cost.AstNode, args []cost.AstNode) *cost.CallEstimate {
						if target != nil {
							// Charge 1 cost for comparing each element in the list
							elCost := cost.CostEstimate{Min: 1, Max: 1}
							// If the list contains strings or bytes, add the cost of traversing all the strings/bytes as a way
							// of estimating the additional comparison cost.
							if elNode := listElementNode(*target); elNode != nil {
								k := elNode.Type().Kind()
								if k == types.StringKind || k == types.BytesKind {
									sz := sizeEstimate(estimator, elNode)
									elCost = elCost.Add(sz.MultiplyByCostFactor(cost.StringTraversalCostFactor))
								}
								return &cost.CallEstimate{CostEstimate: sizeEstimate(estimator, *target).MultiplyByCost(elCost)}
							}
						}
						return nil
					}),
			},
			wanted: cost.CostEstimate{Min: 25, Max: 35},
		},
		// cel.bind test cases
		{
			name:   "bind: literal init and scalar result",
			expr:   `cel.bind(a, 'hello', a + '!')`,
			wanted: cost.CostEstimate{Min: 12, Max: 12},
		},
		{
			name:   "bind: nested binds",
			expr:   `cel.bind(a, 'hello!', cel.bind(b, 'goodbye', a + ' and, ' + b))`,
			wanted: cost.CostEstimate{Min: 26, Max: 26},
		},
		{
			name:   "bind: shadowed bind",
			expr:   `cel.bind(a, cel.bind(a, 'world', a + '!'), 'hello ' + a)`,
			wanted: cost.CostEstimate{Min: 25, Max: 25},
		},
		{
			name:   "bind: with variable list and index",
			expr:   `cel.bind(a, input, a[0])`,
			vars:   []*decls.VariableDecl{decls.NewVariable("input", intList)},
			wanted: cost.CostEstimate{Min: 13, Max: 13},
		},
		{
			name:   "bind: with variable map and index",
			expr:   `cel.bind(m, input, m['key'])`,
			vars:   []*decls.VariableDecl{decls.NewVariable("input", types.NewMapType(types.StringType, types.StringType))},
			wanted: cost.CostEstimate{Min: 13, Max: 13},
		},
		{
			name:   "bind: with comprehension and size hints",
			vars:   []*decls.VariableDecl{decls.NewVariable("input", allList)},
			hints:  map[string]uint64{"input": 100},
			expr:   `cel.bind(a, input, a.all(x, true))`,
			wanted: cost.CostEstimate{Min: 13, Max: 313},
		},
		{
			name:   "bind: nested with list and size hints",
			vars:   []*decls.VariableDecl{decls.NewVariable("input", nestedList)},
			hints:  map[string]uint64{"input": 50, "input.@items": 10},
			expr:   `cel.bind(a, input, a.all(x, x.all(y, true)))`,
			wanted: cost.CostEstimate{Min: 13, Max: 1763},
		},
		{
			name:   "bind: unused bind variable",
			expr:   `cel.bind(a, [1, 2, 3], 42)`,
			wanted: cost.CostEstimate{Min: 20, Max: 20},
		},
		{
			name:   "bind: derived size propagation to comprehension",
			expr:   `cel.bind(v, [1, 2, 3], v.all(x, true))`,
			wanted: cost.CostEstimate{Min: 31, Max: 31},
		},

		// Two-variable comprehension test cases
		{
			name:   "two-var all: list literal",
			expr:   `[1, 2, 3].all(i, v, i < v)`,
			wanted: cost.CostEstimate{Min: 20, Max: 29},
		},
		{
			name:   "two-var all: list variable with hints",
			vars:   []*decls.VariableDecl{decls.NewVariable("input", allList)},
			hints:  map[string]uint64{"input": 100},
			expr:   `input.all(i, v, true)`,
			wanted: cost.CostEstimate{Min: 2, Max: 302},
		},
		{
			name:   "two-var all: map literal",
			expr:   `{"a": 1, "b": 2}.all(k, v, k != "" && v > 0)`,
			wanted: cost.CostEstimate{Min: 37, Max: 43},
		},
		{
			name:   "two-var all: map variable with hints",
			vars:   []*decls.VariableDecl{decls.NewVariable("input", allMap)},
			hints:  map[string]uint64{"input": 50},
			expr:   `input.all(k, v, true)`,
			wanted: cost.CostEstimate{Min: 2, Max: 152},
		},
		{
			name:   "two-var exists: list literal",
			expr:   `[1, 2, 3].exists(i, v, i == 1 && v == 2)`,
			wanted: cost.CostEstimate{Min: 23, Max: 35},
		},
		{
			name:   "two-var exists: map literal",
			expr:   `{"a": 1, "b": 2}.exists(k, v, k == "a" && v == 1)`,
			wanted: cost.CostEstimate{Min: 39, Max: 47},
		},
		{
			name:   "two-var existsOne: list literal",
			expr:   `[1, 2, 3].existsOne(i, v, v == 1)`,
			wanted: cost.CostEstimate{Min: 21, Max: 24},
		},
		{
			name:   "two-var exists_one: list literal",
			expr:   `[1, 2, 3].exists_one(i, v, v == 1)`,
			wanted: cost.CostEstimate{Min: 21, Max: 24},
		},
		{
			name:   "two-var transformList: 3-arg list literal",
			expr:   `[1, 2, 3].transformList(i, v, i + v)`,
			wanted: cost.CostEstimate{Min: 66, Max: 66},
		},
		{
			name:   "two-var transformList: 4-arg with filter list literal",
			expr:   `[1, 2, 3].transformList(i, v, i % 2 == 0, i + v)`,
			wanted: cost.CostEstimate{Min: 33, Max: 75},
		},
		{
			name:   "two-var transformList: 3-arg map literal",
			expr:   `{"a": 1, "b": 2}.transformList(k, v, k)`,
			wanted: cost.CostEstimate{Min: 67, Max: 67},
		},
		{
			name:   "two-var transformMap: 3-arg map literal",
			expr:   `{"a": 1, "b": 2}.transformMap(k, v, v + 1)`,
			wanted: cost.CostEstimate{Min: 71, Max: 71},
		},
		{
			name:   "two-var transformMap: 4-arg with filter map literal",
			expr:   `{"a": 1, "b": 2}.transformMap(k, v, v > 1, v + 1)`,
			wanted: cost.CostEstimate{Min: 67, Max: 75},
		},
		{
			name:   "two-var transformMapEntry: 3-arg map literal",
			expr:   `{"a": 1, "b": 2}.transformMapEntry(k, v, {v: k})`,
			wanted: cost.CostEstimate{Min: 129, Max: 129},
		},
		{
			name:   "two-var transformMapEntry: 4-arg with filter map literal",
			expr:   `{"a": 1, "b": 2}.transformMapEntry(k, v, v > 1, {v: k})`,
			wanted: cost.CostEstimate{Min: 67, Max: 133},
		},
		{
			name:   "two-var nested all",
			expr:   `[1, 2].all(i, v, [1, 2].all(j, w, i + j < v + w))`,
			wanted: cost.CostEstimate{Min: 17, Max: 79},
		},
		{
			name:   "bind with two-var comprehension",
			expr:   `cel.bind(l, [1, 2, 3], l.all(i, v, i < v))`,
			wanted: cost.CostEstimate{Min: 31, Max: 40},
		},
		{
			name:   "bind with two-var transformList",
			expr:   `cel.bind(m, {"a": 1, "b": 2}, m.transformList(k, v, k))`,
			wanted: cost.CostEstimate{Min: 78, Max: 78},
		},
	}

	for _, tst := range cases {
		tc := tst
		t.Run(tc.name, func(t *testing.T) {
			if tc.hints == nil {
				tc.hints = map[string]uint64{}
			}
			checked := compile(t, tc.expr, tc.vars...)
			est, err := cost.Cost(checked, testCostEstimator{hints: tc.hints}, tc.options...)
			if err != nil {
				t.Fatalf("Cost() failed: %v", err)
			}
			if est.Min != tc.wanted.Min || est.Max != tc.wanted.Max {
				t.Fatalf("Got cost interval [%v, %v], wanted [%v, %v]",
					est.Min, est.Max, tc.wanted.Min, tc.wanted.Max)
			}
		})
	}
}

type testCostEstimator struct {
	hints map[string]uint64
}

func (tc testCostEstimator) EstimateSize(element cost.AstNode) *cost.SizeEstimate {
	if l, ok := tc.hints[strings.Join(element.Path(), ".")]; ok {
		return &cost.SizeEstimate{Min: 0, Max: l}
	}
	if element.Type() == types.BytesType {
		return &cost.SizeEstimate{Min: 0, Max: 12}
	}
	return nil
}

func (tc testCostEstimator) EstimateCallCost(function, overloadID string, target *cost.AstNode, args []cost.AstNode) *cost.CallEstimate {
	switch overloadID {
	case overloads.TimestampToYear:
		return &cost.CallEstimate{CostEstimate: cost.CostEstimate{Min: 7, Max: 7}}
	}
	return nil
}

func estimateSize(estimator cost.Estimator, node cost.AstNode) cost.SizeEstimate {
	if l := node.ComputedSize(); l != nil {
		return *l
	}
	if l := estimator.EstimateSize(node); l != nil {
		return *l
	}
	return cost.SizeEstimate{Min: 0, Max: math.MaxUint64}
}

func listElementNode(list cost.AstNode) cost.AstNode {
	if params := list.Type().Parameters(); len(params) > 0 {
		lt := params[0]
		nodePath := list.Path()
		if nodePath != nil {
			// Provide path if we have it so that a OpenAPIv3 maxLength validation can be looked up, if it exists
			// for this node.
			path := make([]string, len(nodePath)+1)
			copy(path, nodePath)
			path[len(nodePath)] = "@items"
			return cost.NewAstNode(nil, path, lt, nil)
		} else {
			// Provide just the type if no path is available so that worst case size can be looked up based on type.
			return cost.NewAstNode(nil, nil, lt, nil)
		}
	}
	return nil
}

func sizeEstimate(estimator cost.Estimator, t cost.AstNode) cost.SizeEstimate {
	if sz := t.ComputedSize(); sz != nil {
		return *sz
	}
	if sz := estimator.EstimateSize(t); sz != nil {
		return *sz
	}
	return cost.SizeEstimate{Min: 0, Max: math.MaxUint64}
}

type testCustomSizingStrategy struct{}

func (testCustomSizingStrategy) EstimateSize(ctx cost.EstimateContext, node cost.AstNode) (cost.SizeEstimate, bool) {
	if node.Path() != nil && len(node.Path()) > 0 && node.Path()[0] == "custom_str" {
		return cost.SizeEstimate{Min: 10, Max: 20}, true
	}
	if node.Path() != nil && len(node.Path()) > 0 && node.Path()[0] == "custom_list" {
		return cost.SizeEstimate{Min: 1, Max: 5, Elem: &cost.SizeEstimate{Min: 15, Max: 30}}, true
	}
	return cost.SizeEstimate{}, false
}

func (testCustomSizingStrategy) TrackSize(ctx cost.TrackContext, value ref.Val) (uint64, bool) {
	return cost.ActualSize(value), true
}

func TestCustomSizingStrategy(t *testing.T) {
	checked := compile(t, "custom_str.contains('abc')",
		decls.NewVariable("custom_str", types.StringType))

	res, err := cost.Cost(checked, nil, cost.EstimateSizingStrategy(testCustomSizingStrategy{}))
	if err != nil {
		t.Fatalf("Cost() failed: %v", err)
	}
	// 'abc' has length 3, cost traversal factor 0.1 -> ceil(3 * 0.1) = 1
	// custom_str has min 10, max 20 -> min ceil(10 * 0.1) = 1, max ceil(20 * 0.1) = 2
	// contains cost: min 1 * 1 = 1, max 2 * 1 = 2
	// ident cost = 1
	// total = ident(1) + call(min 1, max 2) = min 2, max 3
	if res.Min != 2 || res.Max != 3 {
		t.Errorf("got cost %v, wanted {Min: 2, Max: 3}", res)
	}
}
