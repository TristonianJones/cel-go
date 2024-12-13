// Copyright 2023 Google LLC
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

package ext

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker"
	"github.com/google/cel-go/common/types"

	proto2pb "github.com/google/cel-go/test/proto2pb"
)

func TestLists(t *testing.T) {
	listsTests := []struct {
		expr string
		err  string

		estimatedCost *checker.CostEstimate
		actualCost    uint64
		hints         map[string]uint64
	}{
		{
			expr:          `lists.range(4) == [0,1,2,3]`,
			estimatedCost: &checker.CostEstimate{Min: 26, Max: 26},
			actualCost:    26,
		},
		{
			expr:          `lists.range(4) == [0,1,2,3]`,
			estimatedCost: &checker.CostEstimate{Min: 26, Max: 26},
			actualCost:    26,
		},
		{
			expr:          `lists.range(0) == []`,
			estimatedCost: &checker.CostEstimate{Min: 21, Max: 21},
			actualCost:    21,
		},
		{
			expr:          `[5,1,2,3].reverse() == [3,2,1,5]`,
			estimatedCost: &checker.CostEstimate{Min: 36, Max: 36},
			actualCost:    36,
		},
		{
			expr:          `[].reverse() == []`,
			estimatedCost: &checker.CostEstimate{Min: 31, Max: 31},
			actualCost:    31,
		},
		{
			expr:          `[1].reverse() == [1]`,
			estimatedCost: &checker.CostEstimate{Min: 33, Max: 33},
			actualCost:    33,
		},
		{
			expr:          `['are', 'you', 'as', 'bored', 'as', 'I', 'am'].reverse() == ['am', 'I', 'as', 'bored', 'as', 'you', 'are']`,
			estimatedCost: &checker.CostEstimate{Min: 39, Max: 39},
			actualCost:    39,
		},
		{
			expr:          `[false, true, true].reverse().reverse() == [false, true, true]`,
			estimatedCost: &checker.CostEstimate{Min: 49, Max: 49},
			actualCost:    49,
		},
		{
			// create list + create list + equals == 21
			// slice = create list + 4 elems + call == 15
			expr:          `[1,2,3,4].slice(0, 4) == [1,2,3,4]`,
			estimatedCost: &checker.CostEstimate{Min: 36, Max: 36},
			actualCost:    36,
		},
		{
			expr:          `[1,2,3,4].slice(0, 0) == []`,
			estimatedCost: &checker.CostEstimate{Min: 31, Max: 31},
			actualCost:    31,
		},
		{
			expr:          `[1,2,3,4].slice(1, 1) == []`,
			estimatedCost: &checker.CostEstimate{Min: 31, Max: 31},
			actualCost:    31,
		},
		{
			expr:          `[1,2,3,4].slice(4, 4) == []`,
			estimatedCost: &checker.CostEstimate{Min: 31, Max: 31},
			actualCost:    31,
		},
		{
			expr:          `[1,2,3,4].slice(1, 3) == [2, 3]`,
			estimatedCost: &checker.CostEstimate{Min: 34, Max: 34},
			actualCost:    34,
		},
		{
			expr: `[1,2,3,4].slice(3, 0)`,
			err:  "cannot slice(3, 0), start index must be less than or equal to end index",
		},
		{
			expr: `[1,2,3,4].slice(0, 10)`,
			err:  "cannot slice(0, 10), list is length 4",
		},
		{
			expr: `[1,2,3,4].slice(-5, 10)`,
			err:  "cannot slice(-5, 10), negative indexes not supported",
		},
		{
			expr: `[1,2,3,4].slice(-5, -3)`,
			err:  "cannot slice(-5, -3), negative indexes not supported",
		},
		{
			// list creations: 3 x 10
			// operators: 2 x 1
			// equals: 0 for empty list
			expr:          `dyn([]).flatten() == []`,
			estimatedCost: &checker.CostEstimate{Min: 32, Max: 32},
			actualCost:    32,
		},
		{
			// list creations: 3 x 10
			// estimated list size: 4
			// operators: 2 x 1
			// equals: 1, ceil(size(4) * 0.1)
			expr:          `dyn([1,2,3,4]).flatten() == [1,2,3,4]`,
			estimatedCost: &checker.CostEstimate{Min: 37, Max: 37},
			actualCost:    37,
		},
		{
			// list creations: 6 x 10
			// estimated list size: 2
			// operators: 1
			// equals: 1, ceil(size(3) * 0.1)
			expr:          `[1,[2,[3,4]]].flatten() == [1,2,[3,4]]`,
			estimatedCost: &checker.CostEstimate{Min: 64, Max: 64},
			actualCost:    64,
		},
		{
			// list creations: 6 x 10
			// estimated list size: 5
			// operators: 1
			// equals: 1, ceil(size(4) * 0.1)
			expr:          `[1,2,[],[],[3,4]].flatten() == [1,2,3,4]`,
			estimatedCost: &checker.CostEstimate{Min: 67, Max: 67},
			actualCost:    67,
		},
		{
			// list creations: 5 x 10
			// estimated list size: costFactor(16) x depth(2) x size(2)
			// operators: 1
			// equals: 1, ceil(size(4) * 0.1)
			expr:          `[1,[2,[3,4]]].flatten(2) == [1,2,3,4]`,
			estimatedCost: &checker.CostEstimate{Min: 116, Max: 116},
			actualCost:    55,
		},
		{
			expr: `[1,[2,[3,[4]]]].flatten(-1) == [1,2,3,4]`,
			err:  "level must be non-negative",
		},
		{
			// list creations: 3 x 10
			// estimated sort cost: 0
			// operators: 1
			// equals: 0
			expr:          `[].sort() == []`,
			estimatedCost: &checker.CostEstimate{Min: 31, Max: 31},
		},
		{
			// list creations: 3 x 10
			// estimated sort cost: 1
			// operators: 1
			// equals: 1
			expr:          `[1].sort() == [1]`,
			estimatedCost: &checker.CostEstimate{Min: 32, Max: 32},
		},
		{
			// list creations: 3 x 10
			// estimated sort cost: 4 * log2(4) * costFactor(int, 0.1) = 1
			// operators: 1
			// equals: 1
			expr:          `[4, 3, 2, 1].sort() == [1, 2, 3, 4]`,
			estimatedCost: &checker.CostEstimate{Min: 33, Max: 33},
		},
		{
			// list creations: 3 x 10
			// estimated sort cost: 4 x log2(4) x costFactor(string, 1) = 8
			// operators: 1
			// equals: 1
			expr:          `["d", "a", "b", "c"].sort() == ["a", "b", "c", "d"]`,
			estimatedCost: &checker.CostEstimate{Min: 40, Max: 40},
		},
		{
			expr: `["d", 3, 2, "c"].sort() == ["a", "b", "c", "d"]`,
			err:  "list elements must have the same type",
		},
		{
			expr:          `[].sortBy(e, e) == []`,
			estimatedCost: &checker.CostEstimate{Min: 54, Max: 54},
		},
		{
			// cel.bind(
			//  sortBy_inputs,
			//  ["a"],
			//  sortBy_inputs.sortByAssociatedKeys(
			//    sortBy_inputs.map(e, e)
			//  )
			// ) == ["a"]
			// comp: iterRange=1 (ident), accuInit=10, stepCost=__result__ + ["a"] (13), result=1 (ident),
			// sortByAssoc: target=1, arg=25, cost=11 // 37
			// bind: iterRange=10, ["a"]=10 + sortByAssoc // 57
			// equals: bind=57, op=1, ["a"]=10 // 68
			expr:          `["a"].sortBy(e, e) == ["a"]`,
			estimatedCost: &checker.CostEstimate{Min: 68, Max: 68},
		},
		{
			// cel.bind(
			//  sortBy_inputs,
			//  [-3, 1, -5, -2, 4],
			//  sortBy_inputs.sortByAssociatedKeys(
			//    sortBy_inputs.map(e, e)
			//  )
			// ) == [-5, 4, -3, -2, 1]
			// comp: iterRange=1 (ident), accuInit=10, stepCost=__result__ + [-(int * int)] (16*5), result=1 (ident), // 92
			// sortByAssoc: target=1, arg=92, cost=14 // 107
			// bind: iterRange=10, [-3, 1, -5, -2, 4]=10 + sortByAssoc // 127
			// equals: bind=126, op=1, [-5, 4, -3, -2, 1]=10 // 138
			expr:          `[-3, 1, -5, -2, 4].sortBy(e, -(e * e)) == [-5, 4, -3, -2, 1]`,
			estimatedCost: &checker.CostEstimate{Min: 138, Max: 138},
		},
		{
			expr:          `[-3, 1, -5, -2, 4].map(e, e * 2).sortBy(e, -(e * e)) == [-10, 8, -6, -4, 2]`,
			estimatedCost: &checker.CostEstimate{Min: 219, Max: 219},
		},
		{

			// lists.range(3) // 25
			// comp: iterRange=1, accuIdent=10, stepCost=(16*3), result=1 // 58
			// sortByAssoc: target=1, arg=58, cost=13 // 72
			// bind: iterRange=10, accu=10, sortByAssoc // 92
			// equals: bind=92, op=1, [2, 1, 0]=10
			expr:          `lists.range(3).sortBy(e, -e) == [2, 1, 0]`,
			estimatedCost: &checker.CostEstimate{Min: 103, Max: 103},
		},
		{
			expr:          `["a", "c", "b", "first"].sortBy(e, e == "first" ? "" : e) == ["first", "a", "b", "c"]`,
			estimatedCost: &checker.CostEstimate{Min: 127, Max: 131},
		},
		{
			expr:          `[ExampleType{name: 'foo'}, ExampleType{name: 'bar'}, ExampleType{name: 'baz'}].sortBy(e, e.name) == [ExampleType{name: 'bar'}, ExampleType{name: 'baz'}, ExampleType{name: 'foo'}]`,
			estimatedCost: &checker.CostEstimate{Min: 349, Max: 349},
		},
		{
			expr:          `[].distinct() == []`,
			estimatedCost: &checker.CostEstimate{Min: 31, Max: 31},
			actualCost:    31,
		},
		{
			// list creations: 3 x 10
			// estimated O(n^2) computation: 1x1 * costFactor(int, 0.1)
			// operators: 1
			// equals: 1
			expr:          `[1].distinct() == [1]`,
			estimatedCost: &checker.CostEstimate{Min: 33, Max: 33},
			actualCost:    33,
		},
		{
			// list creations: 3 x 10
			// estimated O(n^2) computation: 8x8 * costFactor(int, 0.1)
			// operators: 1
			// equals: 1
			expr:          `[-2, 5, -2, 1, 1, 5, -2, 1].distinct() == [-2, 5, 1]`,
			estimatedCost: &checker.CostEstimate{Min: 39, Max: 39},
			actualCost:    39,
		},
		{
			// list creations: 3 x 10
			// estimated O(n^2) computation: 8x8 * costFactor(string, 1.0)
			// operators: 1
			// equals: 1
			expr:          `['cabbies', 'a', 'a', 'b', 'a', 'b', 'c', 'c'].distinct() == ['cabbies', 'a', 'b', 'c']`,
			estimatedCost: &checker.CostEstimate{Min: 96, Max: 96},
			actualCost:    44,
		},
		{
			// list creations: 3 x 10
			// estimated O(n^2) computation: 6x6 * costFactor(dyn, 1.0)
			// operators: 1
			// equals: 1
			expr:          `[1, 2.0, "c", 3, "c", 1].distinct() == [1, 2.0, "c", 3]`,
			estimatedCost: &checker.CostEstimate{Min: 68, Max: 68},
			actualCost:    36,
		},
		{
			// list creations: 3 x 10
			// estimated O(n^2) computation: 3x3 * costFactor(dyn, 1.0)
			// operators: 1
			// equals: 1
			expr:          `[1, 1.0, 2].distinct() == [1, 2]`,
			estimatedCost: &checker.CostEstimate{Min: 41, Max: 41},
			actualCost:    33,
		},
		{
			// list creations: 8 x 10
			// estimated O(n^2) computation: 3x3 * costFactor(dyn, 1.0)
			// operators: 1
			// equals: 1
			expr:          `[[1], [1], [2]].distinct() == [[1], [2]]`,
			estimatedCost: &checker.CostEstimate{Min: 91, Max: 91},
			actualCost:    83,
		},
		{
			// list creations: 3 x 10
			// object creations: 5 x 40
			// estimated O(n^2) computation: 3x3 * costFactor(dyn, 1.0)
			// operators: 1
			// equals: 1
			expr:          `[ExampleType{name: 'a'}, ExampleType{name: 'b'}, ExampleType{name: 'a'}].distinct() == [ExampleType{name: 'a'}, ExampleType{name: 'b'}]`,
			estimatedCost: &checker.CostEstimate{Min: 241, Max: 241},
			actualCost:    233,
		},
	}

	env := testListsEnv(t)
	for i, tst := range listsTests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			var asts []*cel.Ast
			pAst, iss := env.Parse(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Parse(%v) failed: %v", tc.expr, iss.Err())
			}
			asts = append(asts, pAst)
			cAst, iss := env.Check(pAst)
			if iss.Err() != nil {
				t.Fatalf("env.Check(%v) failed: %v", tc.expr, iss.Err())
			}
			asts = append(asts, cAst)

			if tc.estimatedCost != nil {
				hints := map[string]uint64{}
				if len(tc.hints) != 0 {
					hints = tc.hints
				}

				est, err := env.EstimateCost(cAst, testCostEstimator{hints: hints})
				if err != nil {
					t.Fatalf("env.EstimateCost() failed: %v", err)
				}
				if !reflect.DeepEqual(est, *tc.estimatedCost) {
					t.Errorf("env.EstimateCost() got %v, wanted %v", est, tc.estimatedCost)
				}
			}

			for _, ast := range asts {
				prgOpts := []cel.ProgramOption{}
				if ast.IsChecked() {
					prgOpts = append(prgOpts, cel.CostTracking(nil))
				}
				prg, err := env.Program(ast, prgOpts...)
				if err != nil {
					t.Fatalf("env.Program() failed: %v", err)
				}
				out, det, err := prg.Eval(cel.NoVars())
				if tc.err != "" {
					if err == nil {
						t.Fatalf("got value %v, wanted error %s for expr: %s",
							out.Value(), tc.err, tc.expr)
					}
					if !strings.Contains(err.Error(), tc.err) {
						t.Errorf("got error %v, wanted error %s for expr: %s", err, tc.err, tc.expr)
					}
				} else if err != nil {
					t.Fatal(err)
				} else if out.Value() != true {
					t.Errorf("got %v, wanted true for expr: %s", out.Value(), tc.expr)
				}
				if tc.estimatedCost != nil {
					if det.ActualCost() != nil && *det.ActualCost() != tc.actualCost {
						t.Errorf("prg.Eval() had cost %v, wanted %v", *det.ActualCost(), tc.actualCost)
					}
				}
			}
		})
	}
}

func TestListsRuntimeErrors(t *testing.T) {
	env, err := cel.NewEnv(Lists(ListsVersion(1)))
	if err != nil {
		t.Fatalf("cel.NewEnv() failed: %v", err)
	}
	listsTests := []struct {
		expr string
		err  string
	}{
		{
			expr: "dyn({}).flatten()",
			err:  "no such overload",
		},
		{
			expr: "dyn({}).flatten(0)",
			err:  "no such overload",
		},
		{
			expr: "[].flatten(-1)",
			err:  "level must be non-negative",
		},
		{
			expr: "[].flatten(dyn('1'))",
			err:  "no such overload",
		},
	}
	for i, tst := range listsTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Compile(%q) failed: %v", tc.expr, iss.Err())
			}
			prg, err := env.Program(ast)
			if err != nil {
				t.Fatalf("env.Program() failed: %v", err)
			}
			_, _, err = prg.Eval(cel.NoVars())
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("prg.Eval() got %v, wanted %v", err, tc.err)
			}
		})
	}
}

func TestListsVersion(t *testing.T) {
	versionCases := []struct {
		version            uint32
		supportedFunctions map[string]string
	}{
		{
			version: 0,
			supportedFunctions: map[string]string{
				"slice": "[1, 2, 3, 4, 5].slice(2, 4) == [3, 4]",
			},
		},
		{
			version: 1,
			supportedFunctions: map[string]string{
				"flatten": "[[1, 2], [3, 4]].flatten() == [1, 2, 3, 4]",
			},
		},
		{
			version: 2,
			supportedFunctions: map[string]string{
				"distinct": "[1, 2, 2, 1].distinct() == [1, 2]",
				"range":    "lists.range(5) == [0, 1, 2, 3, 4]",
				"reverse":  "[1, 2, 3].reverse() == [3, 2, 1]",
				"sort":     "[2, 1, 3].sort() == [1, 2, 3]",
				"sortBy":   "[{'field': 'lo'}, {'field': 'hi'}].sortBy(m, m.field) == [{'field': 'hi'}, {'field': 'lo'}]",
			},
		},
	}
	for _, lib := range versionCases {
		env, err := cel.NewEnv(Lists(ListsVersion(lib.version)))
		if err != nil {
			t.Fatalf("cel.NewEnv(Lists(ListsVersion(%d))) failed: %v", lib.version, err)
		}
		t.Run(fmt.Sprintf("version=%d", lib.version), func(t *testing.T) {
			for _, tc := range versionCases {
				for name, expr := range tc.supportedFunctions {
					supported := lib.version >= tc.version
					t.Run(fmt.Sprintf("%s-supported=%t", name, supported), func(t *testing.T) {
						ast, iss := env.Compile(expr)
						if supported {
							if iss.Err() != nil {
								t.Errorf("unexpected error: %v", iss.Err())
							}
						} else {
							if iss.Err() == nil || !strings.Contains(iss.Err().Error(), "undeclared reference") {
								t.Errorf("got error %v, wanted error %s for expr: %s, version: %d", iss.Err(), "undeclared reference", expr, tc.version)
							}
							return
						}
						prg, err := env.Program(ast)
						if err != nil {
							t.Fatalf("env.Program() failed: %v", err)
						}
						out, _, err := prg.Eval(cel.NoVars())
						if err != nil {
							t.Fatalf("prg.Eval() failed: %v", err)
						}
						if out != types.True {
							t.Errorf("prg.Eval() got %v, wanted true", out)
						}
					})
				}
			}
		})
	}
}

func testListsEnv(t *testing.T, opts ...cel.EnvOption) *cel.Env {
	t.Helper()
	baseOpts := []cel.EnvOption{
		Lists(),
		Bindings(),
		cel.Container("google.expr.proto2.test"),
		cel.Types(&proto2pb.ExampleType{}, &proto2pb.ExternalMessageType{}),
	}
	env, err := cel.NewEnv(append(baseOpts, opts...)...)
	if err != nil {
		t.Fatalf("cel.NewEnv(Lists()) failed: %v", err)
	}
	return env
}
