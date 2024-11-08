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

package ext

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/cel-go/cel"
)

func TestTwoVarComprehensions(t *testing.T) {
	compreTests := []struct {
		expr string
	}{
		// list.all()
		{expr: "[1, 2, 3, 4].all(i, v, i < 5 && v > 0)"},
		{expr: "[1, 2, 3, 4].all(i, v, i < v)"},
		{expr: "[1, 2, 3, 4].all(i, v, i > v) == false"},
		{expr: `
		cel.bind(listA, [1, 2, 3, 4],
		cel.bind(listB, [1, 2, 3, 4, 5],
		   listA.all(i, v, listB[?i].hasValue() && listB[i] == v)
		))
		`},
		{expr: `
		cel.bind(listA, [1, 2, 3, 4, 5, 6],
		cel.bind(listB, [1, 2, 3, 4, 5],
		   listA.all(i, v, listB[?i].hasValue() && listB[i] == v)
		)) == false
		`},
		// list.exists()
		{expr: `
		cel.bind(l, ['hello', 'world', 'hello!', 'worlds'],
		  l.exists(i, v,
		    v.startsWith('hello') && l[?(i+1)].optMap(next, next.endsWith('world')).orValue(false)
		  )
		)
		`},
		// list.existsOne()
		{expr: `
		cel.bind(l, ['hello', 'world', 'hello!', 'worlds'],
		  l.existsOne(i, v,
		    v.startsWith('hello') && l[?(i+1)].optMap(next, next.endsWith('world')).orValue(false)
		  )
		)
		`},
		{expr: `
		cel.bind(l, ['hello', 'goodbye', 'hello!', 'goodbye'],
		  l.exists_one(i, v,
		    v.startsWith('hello') && l[?(i+1)].optMap(next, next == "goodbye").orValue(false)
		  )
		) == false
		`},
		// list.transformList()
		{expr: `
		['Hello', 'world'].transformList(i, v, "[%d]%s".format([i, v.lowerAscii()])) == ["[0]hello", "[1]world"]
		`},
		{expr: `
		['hello', 'world'].transformList(i, v, v.startsWith('greeting'), "[%d]%s".format([i, v])) == []
		`},
		{expr: `
		[1, 2, 3].transformList(indexVar, valueVar, (indexVar * valueVar) + valueVar) == [1, 4, 9]
		`},
		{expr: `
		[1, 2, 3].transformList(indexVar, valueVar, indexVar % 2 == 0, (indexVar * valueVar) + valueVar) == [1, 9]
		`},
		// list.transformMap()
		{expr: `
		['Hello', 'world'].transformMap(i, v, [v.lowerAscii()]) == {0: ['hello'], 1: ['world']}
		`},
		{expr: `
		// round-tripping example
		['world', 'Hello'].transformMap(i, v, [v.lowerAscii()])
		  .transformList(k, v, v) // extract the list back form the map
		  .flatten()
		  .sort() == ['hello', 'world']
		`},
		{expr: `
		[1, 2, 3].transformMap(indexVar, valueVar,
	      (indexVar * valueVar) + valueVar) == {0: 1, 1: 4, 2: 9}
        `},
		{expr: `
		[1, 2, 3].transformMap(indexVar, valueVar, indexVar % 2 == 0,
	  	  (indexVar * valueVar) + valueVar) == {0: 1, 2: 9}
		`},
		// list.transformMapEntry()
		{expr: `
		"key1:value1 key2:value2 key3:value3".split(" ")
		.transformMapEntry(i, v,
		  cel.bind(entry, v.split(":"),
		    entry.size() == 2 ? {entry[0]: entry[1]} : {}
		  )
		) == {'key1': 'value1', 'key2': 'value2', 'key3': 'value3'}
		`},
		{expr: `
		"key1:value1:extra key2:value2 key3".split(" ")
		.transformMapEntry(i, v,
		  cel.bind(entry, v.split(":"), {?entry[0]: entry[?1]})
		) == {'key1': 'value1', 'key2': 'value2'}
		`},
		// map.all()
		{expr: `
		{'hello': 'world', 'hello!': 'world'}.all(k, v, k.startsWith('hello') && v == 'world')
		`},
		{expr: `
		{'hello': 'world', 'hello!': 'worlds'}.all(k, v, k.startsWith('hello') && v.endsWith('world')) == false
		`},
		// map.exists()
		{expr: `
		{'hello': 'world', 'hello!': 'worlds'}.exists(k, v, k.startsWith('hello') && v.endsWith('world'))
		`},
		// map.existsOne()
		{expr: `
		{'hello': 'world', 'hello!': 'worlds'}.existsOne(k, v, k.startsWith('hello') && v.endsWith('world'))
		`},
		// map.exists_one()
		{expr: `
		{'hello': 'world', 'hello!': 'worlds'}.exists_one(k, v, k.startsWith('hello') && v.endsWith('world'))
		`},
		{expr: `
		{'hello': 'world', 'hello!': 'wow, world'}.exists_one(k, v, k.startsWith('hello') && v.endsWith('world')) == false
		`},
		// map.transformList()
		{expr: `
		{'Hello': 'world'}.transformList(k, v, "%s=%s".format([k.lowerAscii(), v])) == ["hello=world"]
		`},
		{expr: `
		{'hello': 'world'}.transformList(k, v, k.startsWith('greeting'), "%s=%s".format([k, v])) == []
		`},
		{expr: `
		{'greeting': 'hello', 'farewell': 'goodbye'}
		  .transformList(k, _, k).sort() == ['farewell', 'greeting']
		`},
		{expr: `
		{'greeting': 'hello', 'farewell': 'goodbye'}
		  .transformList(_, v, v).sort() == ['goodbye', 'hello']
		`},
		// map.transformMap()
		{expr: `
		{'hello': 'world', 'goodbye': 'cruel world'}.transformMap(k, v, "%s, %s!".format([k, v]))
		   == {'hello': 'hello, world!', 'goodbye': 'goodbye, cruel world!'}
		`},
		{expr: `
		{'hello': 'world', 'goodbye': 'cruel world'}.transformMap(k, v, v.startsWith('world'), "%s, %s!".format([k, v]))
		   == {'hello': 'hello, world!'}
		`},
		// map.transformMapEntry()
		{expr: `
		{'hello': 'world', 'greetings': 'tacocat'}.transformMapEntry(k, v, {k.reverse(): v.reverse()})
		   == {'olleh': 'dlrow', 'sgniteerg': 'tacocat'}
		`},
		{expr: `
		{'hello': 'world', 'greetings': 'tacocat'}.transformMapEntry(k, v, v.reverse() == v, {k.reverse(): v.reverse()})
		   == {'sgniteerg': 'tacocat'}
		`},
		{expr: `
		{'hello': 'world', 'greetings': 'tacocat'}.transformMapEntry(k, v, {}) == {}
		`},
	}

	env := testCompreEnv(t)
	for i, tst := range compreTests {
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

			for _, ast := range asts {
				prg, err := env.Program(ast)
				if err != nil {
					t.Fatalf("env.Program() failed: %v", err)
				}
				out, _, err := prg.Eval(cel.NoVars())
				if err != nil {
					t.Fatalf("prg.Eval() failed: %v", err)
				}
				if out.Value() != true {
					t.Errorf("prg.Eval() got %v, wanted true for expr: %s", out.Value(), tc.expr)
				}
			}
		})
	}
}

func TestTwoVarComprehensionsStaticErrors(t *testing.T) {
	tests := []struct {
		expr string
		err  string
	}{
		{
			expr: "[].all(i, i, i < i)",
			err:  "duplicate variable name: i",
		},
		{
			expr: "[].all(__result__, i, __result__ < i)",
			err:  "iteration variable overwrites accumulator variable",
		},
		{
			expr: "[].all(j, __result__, __result__ < j)",
			err:  "iteration variable overwrites accumulator variable",
		},
		{
			expr: "[].all(i.j, k, i.j < k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "[].all(j, i.k, j < i.k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "1.all(j, k, j < k)",
			err:  "cannot be range",
		},
		{
			expr: "[].exists(i.j, k, i.j < k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "[].exists(j, i.k, j < i.k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "''.exists(j, k, j < k)",
			err:  "cannot be range",
		},
		{
			expr: "[].exists_one(i.j, k, i.j < k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "[].existsOne(j, i.k, j < i.k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "[].exists_one(i.j, k, i.j < k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "''.existsOne(j, k, j < k)",
			err:  "cannot be range",
		},
		{
			expr: "[].transformList(i.j, k, i.j + k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "[].transformList(j, i.k, j + i.k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "{}.transformMap(i.j, k, i.j + k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "{}.transformMap(j, i.k, j + i.k)",
			err:  "argument must be a simple name",
		},
		{
			expr: "{}.transformMapEntry(j, i.k, {j: i.k})",
			err:  "argument must be a simple name",
		},
		{
			expr: "{}.transformMapEntry(i.j, k, {k: i.j})",
			err:  "argument must be a simple name",
		},
		{
			expr: "{}.transformMapEntry(j, k, 'bad filter', {k: j})",
			err:  "no matching overload",
		},
		{
			expr: "[1, 2].transformList(i, v, v % 2 == 0 ? [v] : v)",
			err:  "no matching overload",
		},
		{
			expr: `{'hello': 'world', 'greetings': 'tacocat'}.transformMapEntry(k, v, []) == {}`,
			err:  "no matching overload"},
	}
	env := testCompreEnv(t)
	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			_, iss := env.Compile(tc.expr)
			if iss.Err() == nil || !strings.Contains(iss.Err().Error(), tc.err) {
				t.Errorf("env.Compile(%q) got %v, wanted error %v", tc.expr, iss.Err(), tc.err)
			}
		})
	}
}

func TestTwoVarComprehensionsRuntimeErrors(t *testing.T) {
	tests := []struct {
		expr string
		err  string
	}{
		{
			expr: "[1, 1].transformMapEntry(i, v, {v: i})",
			err:  "insert failed: key 1 already exists",
		},
		{
			expr: `[0, 0u].transformMapEntry(i, v, {v: i})`,
			err:  "insert failed: key 0 already exists",
		},
		{
			// Test to ensure comprehension iteration limits are respected
			expr: `[0, 1, 2, 3].transformMapEntry(i, v, {v: i})`,
			err:  "operation interrupted",
		},
	}
	env := testCompreEnv(t, cel.ComprehensionLimits(2, 3))
	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Compile(%q) failed with error %v", tc.expr, iss.Err())
			}
			prg, err := env.Program(ast)
			if err != nil {
				t.Fatalf("env.Program(ast) failed: %v", err)
			}
			in := cel.NoVars()
			_, _, err = prg.Eval(in)
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("prg.Eval() got %v, wanted %v", err, tc.err)
			}
		})
	}
}

func testCompreEnv(t *testing.T, opts ...cel.EnvOption) *cel.Env {
	t.Helper()
	baseOpts := []cel.EnvOption{
		TwoVarComprehensions(),
		Bindings(),
		Lists(),
		Strings(),
		cel.OptionalTypes(),
		cel.EnableMacroCallTracking()}
	env, err := cel.NewEnv(append(baseOpts, opts...)...)
	if err != nil {
		t.Fatalf("cel.NewEnv(TwoVarComprehensions()) failed: %v", err)
	}
	return env
}
