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

package types_test

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/pb"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/common/types/traits"
	"cel.dev/cel-go/ext"
	"cel.dev/cel-go/test"

	structpb "google.golang.org/protobuf/types/known/structpb"

	proto3pb "cel.dev/cel-go/test/proto3pb"
)

func TestNativeTypes(t *testing.T) {
	var nativeTests = []struct {
		expr    string
		out     any
		in      any
		envOpts []any
	}{
		{
			expr: `types_test.TestAllTypes{
				NestedVal: types_test.TestNestedType{NestedMapVal: {1: false}},
				BoolVal: true,
				BytesVal: b'hello',
				DurationVal: duration('5s'),
				DoubleVal: 1.5,
				FloatVal: 2.5,
				Int32Val: 10,
				Int64Val: 20,
				StringVal: 'hello world',
				TimestampVal: timestamp('2011-08-06T01:23:45Z'),
				Uint32Val: 100u,
				Uint64Val: 200u,
				ListVal: [
					types_test.TestNestedType{
						NestedListVal:['goodbye', 'cruel', 'world'],
						NestedMapVal: {42: true},
						custom_name: 'name',
					},
				],
				ArrayVal: [
					types_test.TestNestedType{
						NestedListVal:['goodbye', 'cruel', 'world'],
						NestedMapVal: {42: true},
						custom_name: 'name',
					},
				],
				MapVal: {'map-key': types_test.TestAllTypes{BoolVal: true}},
				CustomSliceVal: [types_test.TestNestedSliceType{Value: 'none'}],
				CustomMapVal: {'even': types_test.TestMapVal{Value: 'more'}},
				custom_name: 'name',
			}`,
			out: &TestAllTypes{
				NestedVal:    &TestNestedType{NestedMapVal: map[int64]bool{1: false}},
				BoolVal:      true,
				BytesVal:     []byte("hello"),
				DurationVal:  time.Second * 5,
				DoubleVal:    1.5,
				FloatVal:     2.5,
				Int32Val:     10,
				Int64Val:     20,
				StringVal:    "hello world",
				TimestampVal: mustParseTime(t, "2011-08-06T01:23:45Z"),
				Uint32Val:    uint32(100),
				Uint64Val:    uint64(200),
				ListVal: []*TestNestedType{
					{
						NestedListVal:    []string{"goodbye", "cruel", "world"},
						NestedMapVal:     map[int64]bool{42: true},
						NestedCustomName: "name",
					},
				},
				ArrayVal: [1]*TestNestedType{{
					NestedListVal:    []string{"goodbye", "cruel", "world"},
					NestedMapVal:     map[int64]bool{42: true},
					NestedCustomName: "name",
				}},
				MapVal:         map[string]TestAllTypes{"map-key": {BoolVal: true}},
				CustomSliceVal: []TestNestedSliceType{{Value: "none"}},
				CustomMapVal:   map[string]TestMapVal{"even": {Value: "more"}},
				CustomName:     "name",
			},
			envOpts: []any{types.ParseStructTags(true)},
		},

		{
			expr: `types_test.TestAllTypes{
				nestedVal: types_test.TestNestedType{NestedMapVal: {1: false}},
				boolVal: true,
				BytesVal: b'hello',
				DurationVal: duration('5s'),
				DoubleVal: 1.5,
				FloatVal: 2.5,
				Int32Val: 10,
				Int64Val: 20,
				StringVal: 'hello world',
				TimestampVal: timestamp('2011-08-06T01:23:45Z'),
				Uint32Val: 100u,
				Uint64Val: 200u,
				ListVal: [
					types_test.TestNestedType{
						NestedListVal:['goodbye', 'cruel', 'world'],
						NestedMapVal: {42: true},
						custom_name: 'name',
					},
				],
				ArrayVal: [
					types_test.TestNestedType{
						NestedListVal:['goodbye', 'cruel', 'world'],
						NestedMapVal: {42: true},
						custom_name: 'name',
					},
				],
				MapVal: {'map-key': types_test.TestAllTypes{boolVal: true}},
				CustomSliceVal: [types_test.TestNestedSliceType{Value: 'none'}],
				CustomMapVal: {'even': types_test.TestMapVal{Value: 'more'}},
				CustomName: 'name',
			}`,
			out: &TestAllTypes{
				NestedVal:    &TestNestedType{NestedMapVal: map[int64]bool{1: false}},
				BoolVal:      true,
				BytesVal:     []byte("hello"),
				DurationVal:  time.Second * 5,
				DoubleVal:    1.5,
				FloatVal:     2.5,
				Int32Val:     10,
				Int64Val:     20,
				StringVal:    "hello world",
				TimestampVal: mustParseTime(t, "2011-08-06T01:23:45Z"),
				Uint32Val:    uint32(100),
				Uint64Val:    uint64(200),
				ListVal: []*TestNestedType{
					{
						NestedListVal:    []string{"goodbye", "cruel", "world"},
						NestedMapVal:     map[int64]bool{42: true},
						NestedCustomName: "name",
					},
				},
				ArrayVal: [1]*TestNestedType{{
					NestedListVal:    []string{"goodbye", "cruel", "world"},
					NestedMapVal:     map[int64]bool{42: true},
					NestedCustomName: "name",
				}},
				MapVal:         map[string]TestAllTypes{"map-key": {BoolVal: true}},
				CustomSliceVal: []TestNestedSliceType{{Value: "none"}},
				CustomMapVal:   map[string]TestMapVal{"even": {Value: "more"}},
				CustomName:     "name",
			},
			envOpts: []any{types.ParseStructTag("json")},
		},
		{
			expr: `types_test.TestAllTypes{
				NestedVal: types_test.TestNestedType{NestedMapVal: {1: false}},
				BoolVal: true,
				BytesVal: b'hello',
				DurationVal: duration('5s'),
				DoubleVal: 1.5,
				FloatVal: 2.5,
				Int32Val: 10,
				Int64Val: 20,
				StringVal: 'hello world',
				TimestampVal: timestamp('2011-08-06T01:23:45Z'),
				Uint32Val: 100u,
				Uint64Val: 200u,
				ListVal: [
					types_test.TestNestedType{
						NestedListVal:['goodbye', 'cruel', 'world'],
						NestedMapVal: {42: true},
						NestedCustomName: 'name',
					},
				],
				ArrayVal: [
					types_test.TestNestedType{
						NestedListVal:['goodbye', 'cruel', 'world'],
						NestedMapVal: {42: true},
						NestedCustomName: 'name',
					},
				],
				MapVal: {'map-key': types_test.TestAllTypes{BoolVal: true}},
				CustomSliceVal: [types_test.TestNestedSliceType{Value: 'none'}],
				CustomMapVal: {'even': types_test.TestMapVal{Value: 'more'}},
				CustomName: 'name',
			}`,
			out: &TestAllTypes{
				NestedVal:    &TestNestedType{NestedMapVal: map[int64]bool{1: false}},
				BoolVal:      true,
				BytesVal:     []byte("hello"),
				DurationVal:  time.Second * 5,
				DoubleVal:    1.5,
				FloatVal:     2.5,
				Int32Val:     10,
				Int64Val:     20,
				StringVal:    "hello world",
				TimestampVal: mustParseTime(t, "2011-08-06T01:23:45Z"),
				Uint32Val:    uint32(100),
				Uint64Val:    uint64(200),
				ListVal: []*TestNestedType{
					{
						NestedListVal:    []string{"goodbye", "cruel", "world"},
						NestedMapVal:     map[int64]bool{42: true},
						NestedCustomName: "name",
					},
				},
				ArrayVal: [1]*TestNestedType{{
					NestedListVal:    []string{"goodbye", "cruel", "world"},
					NestedMapVal:     map[int64]bool{42: true},
					NestedCustomName: "name",
				}},
				MapVal:         map[string]TestAllTypes{"map-key": {BoolVal: true}},
				CustomSliceVal: []TestNestedSliceType{{Value: "none"}},
				CustomMapVal:   map[string]TestMapVal{"even": {Value: "more"}},
				CustomName:     "name",
			},
		},
		{
			expr: `types_test.TestAllTypes{
					PbVal: test.TestAllTypes{single_int32: 123}
				}.PbVal`,
			out: &proto3pb.TestAllTypes{SingleInt32: 123},
		},
		{
			expr: `types_test.TestAllTypes{PbVal: test.TestAllTypes{}} ==
			types_test.TestAllTypes{PbVal: test.TestAllTypes{single_bool: false}}`,
		},
		{expr: `types_test.TestNestedType{} == TestNestedType{}`},
		{expr: `types_test.TestAllTypes{}.BoolVal != true`},
		{expr: `!has(types_test.TestAllTypes{}.BoolVal) && !has(types_test.TestAllTypes{}.NestedVal)`},
		{expr: `type(types_test.TestAllTypes) == type`},
		{expr: `type(types_test.TestAllTypes{}) == types_test.TestAllTypes`},
		{expr: `type(types_test.TestAllTypes{}) == types_test.TestAllTypes`},
		{expr: `types_test.TestAllTypes != test.TestAllTypes`},
		{expr: `types_test.TestAllTypes{BoolVal: true} != dyn(test.TestAllTypes{single_bool: true})`},
		{expr: `types_test.TestAllTypes{}.NestedVal == types_test.TestNestedType{}`},
		{expr: `types_test.TestNestedType{} == types_test.TestAllTypes{}.NestedStructVal`},
		{expr: `types_test.TestAllTypes{}.NestedStructVal == types_test.TestNestedType{}`},
		{expr: `types_test.TestAllTypes{}.ListVal.size() == 0`},
		{expr: `types_test.TestAllTypes{}.MapVal.size() == 0`},
		{expr: `types_test.TestAllTypes{}.TimestampVal == timestamp(0)`},
		{expr: `test.TestAllTypes{}.single_timestamp == timestamp(0)`},
		{expr: `[TestAllTypes{BoolVal: true}, TestAllTypes{BoolVal: false}].exists(t, t.BoolVal == true)`},
		{expr: `[TestAllTypes{CustomName: 'Alice'}, TestAllTypes{CustomName: 'Bob'}].exists(t, t.CustomName == 'Alice')`},
		{expr: `[TestAllTypes{custom_name: 'Alice'}, TestAllTypes{custom_name: 'Bob'}].exists(t, t.custom_name == 'Alice')`, envOpts: []any{types.ParseStructTags(true)}},
		{expr: `TestAllTypes{BytesArrayVal: b'1234'}.BytesArrayVal != b'123'`},
		{expr: `TestAllTypes{BytesArrayVal: b'1234'}.BytesArrayVal == b'1234'`},
		{
			expr: `tests.all(t, t.Int32Val > 17)`,
			in: map[string]any{
				"tests": []*TestAllTypes{{Int32Val: 18}, {Int32Val: 19}, {Int32Val: 20}},
			},
		},
	}
	for i, tst := range nativeTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
			env := testNativeEnv(t, tc.envOpts...)
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
					t.Fatal(err)
				}
				in := tc.in
				if in == nil {
					in = cel.NoVars()
				}
				out, _, err := prg.Eval(in)
				if err != nil {
					t.Fatal(err)
				}
				want := tc.out
				if want == nil {
					want = true
				}
				wantPB, isPB := want.(proto.Message)
				if isPB && !pb.Equal(wantPB, out.Value().(proto.Message)) {
					t.Errorf("got %v, wanted %v for expr: %s", out.Value(), want, tc.expr)
				}
				if !isPB && !reflect.DeepEqual(out.Value(), want) {
					t.Errorf("got %v, wanted %v for expr: %s", out.Value(), want, tc.expr)
				}
			}
		})
	}
}

func TestNativeFindStructFieldNames(t *testing.T) {
	env := testNativeEnv(t, types.ParseStructTags(true))
	provider := env.CELTypeProvider()
	tests := []struct {
		typeName string
		fields   []string
	}{
		{
			typeName: "types_test.TestNestedType",
			fields:   []string{"NestedListVal", "NestedMapVal", "custom_name"},
		},
		{
			typeName: "google.expr.proto3.test.TestAllTypes.NestedMessage",
			fields:   []string{"bb"},
		},
		{
			typeName: "invalid.TypeName",
			fields:   []string{},
		},
	}

	for _, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%s", tc.typeName), func(t *testing.T) {
			fields, _ := provider.FindStructFieldNames(tc.typeName)
			sort.Strings(fields)
			sort.Strings(tc.fields)
			if !reflect.DeepEqual(fields, tc.fields) {
				t.Errorf("got %v, wanted %v", fields, tc.fields)
			}
		})
	}
}

func TestNativeTypesStaticErrors(t *testing.T) {
	var nativeTests = []struct {
		expr string
		err  string
	}{
		{
			expr: `TestAllTypos{}`,
			err: `ERROR: <input>:1:13: undeclared reference to 'TestAllTypos' (in container 'types_test')
			 | TestAllTypos{}
			 | ............^`,
		},
		{
			expr: `types_test.TestAllTypes{bool_val: false}`,
			err: `ERROR: <input>:1:33: undefined field 'bool_val'
			| types_test.TestAllTypes{bool_val: false}
			| ................................^`,
		},
		{
			expr: `types_test.TestAllTypes{UnsupportedVal: null}`,
			err: `ERROR: <input>:1:39: undefined field 'UnsupportedVal'
			| types_test.TestAllTypes{UnsupportedVal: null}
			| ......................................^`,
		},
		{
			expr: `types_test.TestAllTypes{UnsupportedListVal: null}`,
			err: `ERROR: <input>:1:43: undefined field 'UnsupportedListVal'
			| types_test.TestAllTypes{UnsupportedListVal: null}
			| ..........................................^`,
		},
		{
			expr: `types_test.TestAllTypes{UnsupportedMapVal: null}`,
			err: `ERROR: <input>:1:42: undefined field 'UnsupportedMapVal'
			| types_test.TestAllTypes{UnsupportedMapVal: null}
			| .........................................^`,
		},
	}
	env := testNativeEnv(t)
	for i, tst := range nativeTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
			_, iss := env.Compile(tc.expr)
			if iss.Err() == nil {
				t.Fatalf("env.Compile(%v) succeeded, wanted error", tc.expr)
			}
			if !test.Compare(iss.Err().Error(), tc.err) {
				t.Errorf("env.Compile(%v) got %v, wanted error %s", tc.expr, iss.Err(), tc.err)
			}
		})
	}
}

func TestNativeTypesJsonSerialization(t *testing.T) {
	tests := []struct {
		expr                 string
		out                  string
		additionalEnvOptions []any
	}{
		{
			expr: `[b'string']`,
			out:  `["c3RyaW5n"]`,
		},
		{
			expr: `TestAllTypes{
				BoolVal: true,
				DurationVal: duration('5s'),
				DoubleVal: 1.5,
				FloatVal: 2.0,
				Int32Val: 23,
				Int64Val: 64,
				MapVal: {
					'map-key': types_test.TestAllTypes{
						BoolVal: true
					}
				},
				NestedVal: TestNestedType{
					NestedListVal: ["first", "second"],
				},
				StringVal: "string",
				CustomName: "name",
			}`,
			out: `{
				"CustomName":  "name",
				"DoubleVal":  1.5,
				"DurationVal":  "5s",
				"FloatVal":  2,
				"Int32Val":  23,
				"Int64Val":  64,
				"MapVal": {
	              "map-key": {
    	            "boolVal": true
        	      }
            	},
				"StringVal":  "string",
				"boolVal":  true,
				"nestedVal": {
					"NestedListVal": [
					  "first",
					  "second"
					]
				}
			  }`,
		},
		{
			expr: `TestAllTypes{
				BoolVal: true,
				DurationVal: duration('5s'),
				DoubleVal: 1.5,
				FloatVal: 2.0,
				Int32Val: 23,
				Int64Val: 64,
				MapVal: {
					'map-key': types_test.TestAllTypes{
						BoolVal: true
					}
				},
				NestedVal: TestNestedType{
					NestedListVal: ["first", "second"],
				},
				StringVal: "string",
                custom_name: "name",
			}`,
			out: `{
				"DoubleVal":  1.5,
				"DurationVal":  "5s",
				"FloatVal":  2,
				"Int32Val":  23,
				"Int64Val":  64,
				"MapVal": {
	              "map-key": {
    	            "boolVal": true
        	      }
            	},
				"StringVal":  "string",
				"boolVal":  true,
				"custom_name": "name",
				"nestedVal": {
					"NestedListVal": [
					  "first",
					  "second"
					]
				}
			  }`,
			additionalEnvOptions: []any{types.ParseStructTags(true)},
		},
		{
			expr: `TestSpecialJSONTags{
				ignored: "sensitive",
				hyphen_name: "hyphen-val",
				quoted_hyphen: "quoted-val",
				renamed: "renamed-val",
				empty_int: 0,
				empty_str: "",
				pop_int: 42,
				keep_zero: 0,
				keep_empty: "",
				keep_false: false,
				cel_field: "divergent-val",
			}`,
			out: `{
				"-": "quoted-val",
				"custom_json_name": "renamed-val",
				"json_field": "divergent-val",
				"keep_empty": "",
				"keep_false": false,
				"keep_zero": 0,
				"pop_int": 42
			}`,
			additionalEnvOptions: []any{types.ParseStructTags(true)},
		},
		{
			expr: `TestEmbeddedTypes{
				name: "alice",
				Skipped: "secret",
				NestedListVal: ["a", "b"],
				custom_name: "nested",
			}`,
			out: `{
				"embedded": {
					"NestedListVal": [
						"a",
						"b"
					],
					"custom_name": "nested"
				},
				"name": "alice"
			}`,
			additionalEnvOptions: []any{types.ParseStructTags(true)},
		},
		{
			expr: `TestEmbeddedTypes{
				name: "bob",
				Skipped: "secret",
			}`,
			out: `{
				"name": "bob"
			}`,
			additionalEnvOptions: []any{types.ParseStructTags(true)},
		},
		{
			expr: `TestEmbeddedPointerTypes{
				NestedListVal: ["x"],
				custom_name: "ptr_nested",
			}`,
			out: `{
				"embedded": {
					"NestedListVal": [
						"x"
					],
					"custom_name": "ptr_nested"
				}
			}`,
			additionalEnvOptions: []any{types.ParseStructTags(true)},
		},
		{
			expr:                 `TestEmbeddedPointerTypes{}`,
			out:                  `{}`,
			additionalEnvOptions: []any{types.ParseStructTags(true)},
		},
	}
	for i, tst := range tests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			env := testNativeEnv(t, tst.additionalEnvOptions...)
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Compile(%v) failed: %v", tc.expr, iss.Err())
			}
			prg, err := env.Program(ast)
			if err != nil {
				t.Fatalf("env.Program() failed: %v", err)
			}
			out, _, err := prg.Eval(cel.NoVars())
			if err != nil {
				t.Fatalf("prg.Eval() failed: %v", err)
			}
			conv, err := out.ConvertToNative(reflect.TypeOf(&structpb.Value{}))
			if err != nil {
				t.Fatalf("out.ConvertToNative(Value) failed: %v", err)
			}
			json := protojson.Format(conv.(proto.Message))
			if !test.Compare(json, tc.out) {
				t.Errorf("expr %v converted to %v, wanted %v", tc.expr, json, tc.out)
			}
		})
	}
}

func TestNativeTypesRuntimeErrors(t *testing.T) {
	var nativeTests = []struct {
		expr string
		err  string
	}{
		{
			expr: `TestAllTypos{}`,
			err:  `unknown type: TestAllTypos`,
		},
		{
			expr: `types_test.TestAllTypes{bool_val: false}`,
			err:  `no such field: bool_val`,
		},
		{
			expr: `types_test.TestAllTypes{UnsupportedVal: null}`,
			err:  `no such field: UnsupportedVal`,
		},
		{
			expr: `types_test.TestAllTypes{UnsupportedListVal: null}`,
			err:  `no such field: UnsupportedListVal`,
		},
		{
			expr: `types_test.TestAllTypes{UnsupportedMapVal: null}`,
			err:  `no such field: UnsupportedMapVal`,
		},
		{
			expr: `types_test.TestAllTypes{privateVal: null}`,
			err:  `no such field: privateVal`,
		},
		{
			expr: `types_test.TestAllTypes{}.UnsupportedMapVal`,
			err:  `no such field: UnsupportedMapVal`,
		},
		{
			expr: `types_test.TestAllTypes{}.privateVal`,
			err:  `no such field: privateVal`,
		},
		{
			expr: `types_test.TestAllTypes{BoolVal: 'false'}`,
			err:  `unsupported native conversion from string to 'bool'`,
		},
		{
			expr: `has(types_test.TestAllTypes{}.BadFieldName)`,
			err:  `no such field: BadFieldName`,
		},
		{
			expr: `types_test.TestAllTypes{}[42]`,
			err:  `no such overload`,
		},
		{
			expr: `types_test.TestAllTypes{Int32Val: 9223372036854775807}`,
			err:  `integer overflow`,
		},
		{
			expr: `types_test.TestAllTypes{Uint32Val: 9223372036854775807u}`,
			err:  `unsigned integer overflow`,
		},
	}
	env := testNativeEnv(t)
	for i, tst := range nativeTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
			ast, iss := env.Parse(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Parse(%v) failed: %v", tc.expr, iss.Err())
			}
			prg, err := env.Program(ast)
			if err != nil {
				if !strings.Contains(err.Error(), tc.err) {
					t.Fatal(err)
				}
				return
			}
			out, _, err := prg.Eval(cel.NoVars())
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				var got any = err
				if err == nil {
					got = out
				}
				t.Fatalf("prg.Eval() got %v, wanted error %v", got, tc.err)
			}
		})
	}
}

func TestNativeTypesErrors(t *testing.T) {
	envTests := []struct {
		nativeType any
		err        string
	}{
		{
			nativeType: reflect.TypeOf(1),
			err:        "unsupported reflect.Type",
		},
		{
			nativeType: reflect.ValueOf(1),
			err:        "unsupported reflect.Type",
		},
		{
			nativeType: 1,
			err:        "must be reflect.Type",
		},
	}
	for i, tst := range envTests {
		tc := tst
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			_, err := cel.NewEnv(ext.NativeTypes(tc.nativeType))
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("cel.NewEnv(NativeTypes(%v)) got error %v, wanted %v", tc.nativeType, err, tc.err)
			}
		})
	}
}

func TestNativeTypesConvertToNative(t *testing.T) {
	env := testNativeEnv(t, ext.NativeTypes(reflect.TypeOf(TestNestedType{})))
	adapter := env.CELTypeAdapter()
	conversions := []struct {
		in     any
		inType *cel.Type
		out    any
		err    string
	}{
		{
			in:     &TestAllTypes{BoolVal: true},
			inType: cel.ObjectType("types_test.TestAllTypes"),
			out:    &TestAllTypes{BoolVal: true},
		},
		{
			in:     TestAllTypes{BoolVal: true},
			inType: cel.ObjectType("types_test.TestAllTypes"),
			out:    &TestAllTypes{BoolVal: true},
		},
		{
			in:     &TestAllTypes{BoolVal: true},
			inType: cel.ObjectType("types_test.TestAllTypes"),
			out:    TestAllTypes{BoolVal: true},
		},
		{
			in:     nil,
			inType: cel.NullType,
			out:    types.NullValue,
		},
		{
			in:     &TestAllTypes{BoolVal: true},
			inType: cel.ObjectType("types_test.TestAllTypes"),
			out:    &proto3pb.TestAllTypes{},
			err:    "type conversion error",
		},
		{
			in:     [3]int32{1, 2, 3},
			inType: cel.ListType(cel.IntType),
			out:    []int32{1, 2, 3},
		},
		{
			in:     &[3]byte{1, 2, 3},
			inType: cel.BytesType,
			out:    []byte{1, 2, 3},
		},
		{
			in:     [3]byte{1, 2, 3},
			inType: cel.BytesType,
			out:    []byte{1, 2, 3},
		},
	}
	for _, c := range conversions {
		inVal := adapter.NativeToValue(c.in)
		if types.IsError(inVal) {
			t.Fatalf("adapter.NativeToValue(%v) failed: %v", c.in, inVal)
		}
		if inVal.Type().TypeName() != c.inType.TypeName() {
			t.Fatalf("adapter.NativeToValue() got type %v, wanted type %v", inVal.Type(), c.inType)
		}
		out, err := inVal.ConvertToNative(reflect.TypeOf(c.out))
		if err != nil {
			if c.err != "" {
				if !strings.Contains(err.Error(), c.err) {
					t.Fatalf("%v.ConvertToNative(%T) got %v, wanted error %v", c.in, c.out, err, c.err)
				}
				return
			}
			t.Fatalf("%v.ConvertToNative(%T) failed: %v", c.in, c.out, err)
		}
		if !reflect.DeepEqual(out, c.out) {
			t.Errorf("%v.ConvertToNative(%T) got %v, wanted %v", c.in, c.out, out, c.out)
		}
	}
}

func TestConvertToTypeErrors(t *testing.T) {
	env := testNativeEnv(t, ext.NativeTypes(reflect.TypeOf(TestNestedType{})))
	adapter := env.CELTypeAdapter()
	conversions := []struct {
		in  any
		out any
		err string
	}{
		{
			in:  &TestAllTypes{BoolVal: true},
			out: &TestAllTypes{BoolVal: true},
		},
		{
			in:  TestAllTypes{BoolVal: true},
			out: &TestAllTypes{BoolVal: true},
		},
		{
			in:  &TestAllTypes{BoolVal: true},
			out: TestAllTypes{BoolVal: true},
		},
		{
			in:  &TestAllTypes{BoolVal: true},
			out: &proto3pb.TestAllTypes{},
			err: "type conversion error",
		},
	}
	for _, c := range conversions {
		inVal := adapter.NativeToValue(c.in)
		outVal := adapter.NativeToValue(c.out)
		if types.IsError(inVal) {
			t.Fatalf("adapter.NativeToValue(%v) failed: %v", c.in, inVal)
		}
		if types.IsError(outVal) {
			t.Fatalf("adapter.NativeToValue(%v) failed: %v", c.out, outVal)
		}
		conv := inVal.ConvertToType(outVal.Type())
		if c.err != "" {
			if !types.IsError(conv) {
				t.Fatalf("%v.ConvertToType(%v) got %v, wanted error %v", c.in, outVal.Type(), conv, c.err)
			}
			convErr := conv.(*types.Err)
			if !strings.Contains(convErr.Error(), c.err) {
				t.Fatalf("%v.ConvertToType(%v) got %v, wanted error %v", c.in, outVal.Type(), conv, c.err)
			}
			return
		}
		if conv != inVal {
			t.Errorf("%v.ConvertToType(%v) got %v, wanted %v", c.in, outVal.Type(), conv, c.err)
		}
		conv = inVal.ConvertToType(types.TypeType)
		if conv.Type() != types.TypeType || conv.(ref.Type) != inVal.Type() {
			t.Errorf("%v.ConvertToType(Type) got %v, wanted %v", inVal, conv, inVal.Type())
		}
	}
}

func TestNativeTypesWithOptional(t *testing.T) {
	var nativeTests = []struct {
		expr string
	}{
		{expr: `!optional.ofNonZeroValue(types_test.TestAllTypes{}).hasValue()`},
		{expr: `!types_test.TestAllTypes{}.?BoolVal.orValue(false)`},
		{expr: `!types_test.TestAllTypes{}.?BoolVal.hasValue()`},
		{expr: `!types_test.TestAllTypes{BoolVal: false}.?BoolVal.hasValue()`},
		{expr: `types_test.TestAllTypes{BoolVal: true}.?BoolVal.hasValue()`},
		{expr: `types_test.TestAllTypes{}.NestedVal.?NestedMapVal.orValue({}).size() == 0`},
	}
	env := testNativeEnv(t, cel.OptionalTypes())
	for i, tst := range nativeTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
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
					t.Fatal(err)
				}
				out, _, err := prg.Eval(cel.NoVars())
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(out.Value(), true) {
					t.Errorf("got %v, wanted true for expr: %s", out.Value(), tc.expr)
				}
			}
		})
	}
}

func TestNativeTypesWithCELTypedFields(t *testing.T) {
	var nativeTests = []struct {
		expr string
	}{
		{
			expr: `types_test.TestRefValFieldType{optional_name: optional.of('my name')}.optional_name.orValue('') == 'my name'`,
		},
		{
			expr: `types_test.TestRefValFieldType{IntVal: 2}.IntVal >= 1`,
		},
		{
			expr: `types_test.TestRefValFieldType{time: timestamp('2001-01-01T00:00:00Z')}.time > timestamp('1970-01-01T00:00:00Z')`,
		},
	}
	env := testNativeEnv(t, cel.OptionalTypes(), types.ParseStructTag("cel"))
	for i, tst := range nativeTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
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
					t.Fatal(err)
				}
				out, _, err := prg.Eval(cel.NoVars())
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(out.Value(), true) {
					t.Errorf("got %v, wanted true for expr: %s", out.Value(), tc.expr)
				}
			}
		})
	}
}

func TestNativeTypeConvertToType(t *testing.T) {
	var nativeTests = []struct {
		tag string
	}{
		{tag: "cel"},
		{tag: "json"},
	}

	for i, tst := range nativeTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
			handler := func(f reflect.StructField) string {
				tag, found := f.Tag.Lookup(tc.tag)
				if found {
					splits := strings.Split(tag, ",")
					if len(splits) > 0 {
						return splits[0]
					}
				}
				return f.Name
			}
			nt, err := types.NewNativeType(reflect.TypeFor[*TestAllTypes](), types.ParseStructField(handler))
			if err != nil {
				t.Fatalf("NewNativeType() failed: %v", err)
			}
			if nt.ConvertToType(types.TypeType) != types.TypeType {
				t.Error("ConvertToType(Type) failed")
			}
			if !types.IsError(nt.ConvertToType(types.StringType)) {
				t.Errorf("ConvertToType(String) got %v, wanted error", nt.ConvertToType(types.StringType))
			}
		})
	}
}

func TestNativeTypeConvertToNative(t *testing.T) {
	nt, err := types.NewNativeType(reflect.TypeFor[*TestAllTypes]())
	if err != nil {
		t.Fatalf("NewNativeType() failed: %v", err)
	}
	out, err := nt.ConvertToNative(reflect.TypeOf(1))
	if err == nil {
		t.Errorf("nt.ConvertToNative(1) produced %v, wanted error", out)
	}
}

func TestNativeTypeHasTrait(t *testing.T) {
	nt, err := types.NewNativeType(reflect.TypeFor[*TestAllTypes]())
	if err != nil {
		t.Fatalf("NewNativeType() failed: %v", err)
	}
	if !nt.HasTrait(traits.IndexerType) || !nt.HasTrait(traits.FieldTesterType) {
		t.Error("nt.HasTrait() failed indicate support for presence test and field access.")
	}
}

func TestNativeTypeValue(t *testing.T) {
	nt, err := types.NewNativeType(reflect.TypeFor[*TestAllTypes]())
	if err != nil {
		t.Fatalf("NewNativeType() failed: %v", err)
	}
	if nt.Value() != nt.String() {
		t.Errorf("nt.Value() got %v, wanted %v", nt.Value(), nt.String())
	}
}

func TestNativeStructWithMultipleSameFieldNames(t *testing.T) {
	tagHandler := func(f reflect.StructField) string {
		tag, found := f.Tag.Lookup("cel")
		if found {
			splits := strings.Split(tag, ",")
			if len(splits) > 0 {
				return splits[0]
			}
		}
		return f.Name
	}
	_, err := types.NewNativeType(
		reflect.TypeFor[TestStructWithMultipleSameNames](),
		types.ParseStructField(tagHandler),
	)
	if err == nil {
		t.Fatal("NewNativeType() did not fail as expected")
	}
	if !strings.Contains(err.Error(), "field name already exists") {
		t.Fatalf("NewNativeType() expected duplicated field name error, but got: %v", err)
	}
}

func TestNativeStructEmbedded(t *testing.T) {
	var nativeTests = []struct {
		expr string
		in   any
		out  any
	}{
		{
			expr: `test.embedded.custom_name == "name"`,
			in: map[string]any{
				"test": &TestEmbeddedTypes{
					TestNestedType: TestNestedType{NestedCustomName: "name"},
					Skipped:        "should-be-hidden",
				},
			},
			out: true,
		},
		{
			expr: `dyn(test.embedded)["-"] == "error"`,
			in: map[string]any{
				"test": &TestEmbeddedTypes{
					TestNestedType: TestNestedType{NestedCustomName: "name"},
					Skipped:        "should-be-hidden",
				},
			},
			out: errors.New("no such field: -"),
		},
		{
			expr: `test.embedded == types_test.TestNestedType{custom_name: "name"}`,
			in: map[string]any{
				"test": &TestEmbeddedTypes{
					TestNestedType: TestNestedType{NestedCustomName: "name"},
					Skipped:        "should-be-hidden",
				},
			},
			out: true,
		},
		{
			expr: `test.Name == "name"`,
			in: map[string]any{
				"test": &TestEmbeddedTypes{
					Custom: Custom{Name: "name"},
				},
			},
			out: true,
		},
	}

	envOpts := []cel.EnvOption{
		ext.NativeTypes(
			reflect.TypeFor[*TestEmbeddedTypes](),
			reflect.TypeFor[*TestNestedType](),
			types.ParseStructTag("json"),
		),
		cel.Variable("test", cel.ObjectType("types_test.TestEmbeddedTypes")),
	}

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		t.Fatalf("cel.NewEnv(NativeTypes()) failed: %v", err)
	}

	for i, tst := range nativeTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
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
					t.Fatal(err)
				}
				out, _, err := prg.Eval(tc.in)
				if err != nil {
					if !errors.Is(err, tc.out.(error)) {
						t.Fatalf("got %v, wanted %v for expr: %s", err, tc.out, tc.expr)
					}
					continue
				}
				if !reflect.DeepEqual(out.Value(), tc.out) {
					t.Errorf("got %v, wanted %v for expr: %s", out.Value(), tc.out, tc.expr)
				}
			}
		})
	}
}

func TestNativeStructEmbeddedPointer(t *testing.T) {
	nativeTests := []struct {
		expr string
		in   map[string]any
		out  any
	}{
		{
			expr: `!has(test.custom_name) && test.custom_name == ""`,
			in: map[string]any{
				"test": &TestEmbeddedPointerTypes{
					TestNestedType: nil,
				},
			},
			out: true,
		},
		{
			expr: `has(test.custom_name) && test.custom_name == "name"`,
			in: map[string]any{
				"test": &TestEmbeddedPointerTypes{
					TestNestedType: &TestNestedType{NestedCustomName: "name"},
				},
			},
			out: true,
		},
		{
			expr: `types_test.TestEmbeddedPointerTypes{custom_name: "name"}.custom_name == "name"`,
			in:   nil,
			out:  true,
		},
	}

	envOpts := []cel.EnvOption{
		ext.NativeTypes(
			reflect.TypeFor[*TestEmbeddedPointerTypes](),
			reflect.TypeFor[*TestNestedType](),
			types.ParseStructTag("json"),
		),
		cel.Variable("test", cel.ObjectType("types_test.TestEmbeddedPointerTypes")),
	}

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		t.Fatalf("cel.NewEnv(NativeTypes()) failed: %v", err)
	}

	for i, tst := range nativeTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
			pAst, iss := env.Parse(tc.expr)
			if iss.Err() != nil {
				t.Fatalf("env.Parse(%v) failed: %v", tc.expr, iss.Err())
			}
			cAst, iss := env.Check(pAst)
			if iss.Err() != nil {
				t.Fatalf("env.Check(%v) failed: %v", tc.expr, iss.Err())
			}
			for _, ast := range []*cel.Ast{pAst, cAst} {
				prg, err := env.Program(ast)
				if err != nil {
					t.Fatal(err)
				}
				out, _, err := prg.Eval(tc.in)
				if err != nil {
					t.Fatalf("prg.Eval() failed: %v", err)
				}
				if !reflect.DeepEqual(out.Value(), tc.out) {
					t.Errorf("got %v, wanted %v for expr: %s", out.Value(), tc.out, tc.expr)
				}
			}
		})
	}
}

func TestNativeStructHiddenField(t *testing.T) {
	envOpts := []cel.EnvOption{
		ext.NativeTypes(
			reflect.TypeFor[*TestEmbeddedTypes](),
			types.ParseStructTag("json"),
		),
		cel.Variable("test", cel.ObjectType("types_test.TestEmbeddedTypes")),
	}

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		t.Fatalf("cel.NewEnv(NativeTypes()) failed: %v", err)
	}

	// 1. Static reference compilation failure case
	// Attempting to compile `test.Password` should fail static analysis because the field is skipped/hidden.
	_, iss := env.Compile("test.Password")
	if iss.Err() == nil {
		t.Error("env.Compile('test.Password') succeeded, expected a compilation/check error")
	}

	// 2. Dynamic reference runtime evaluation failure case
	// Using dyn(test).Password should compile successfully (since dyn disables static type checks),
	// but it must fail at runtime during evaluation because the field is not exposed.
	ast, iss := env.Compile("dyn(test).Password")
	if iss.Err() != nil {
		t.Fatalf("env.Compile('dyn(test).Password') failed: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("env.Program() failed: %v", err)
	}
	in := map[string]any{
		"test": &TestEmbeddedTypes{
			Skipped: "sensitive_password",
		},
	}
	out, _, err := prg.Eval(in)
	if err == nil {
		t.Errorf("prg.Eval() succeeded and returned %v, expected runtime error accessing hidden field", out)
	}
}

type TestNestedStruct struct {
	ListVal []*TestNestedType
}

func TestNativeNestedStruct(t *testing.T) {
	var nativeTests = []struct {
		expr string
		in   any
	}{
		{
			expr: `test.ListVal.exists(x, x.custom_name == "name")`,
			in: map[string]any{
				"test": &TestNestedStruct{ListVal: []*TestNestedType{{NestedCustomName: "name"}}},
			},
		},
	}

	envOpts := []cel.EnvOption{
		ext.NativeTypes(
			reflect.ValueOf(&TestNestedStruct{}),
			types.ParseStructTag("json"),
		),
		cel.Variable("test", cel.ObjectType("types_test.TestNestedStruct")),
	}

	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		t.Fatalf("cel.NewEnv(NativeTypes()) failed: %v", err)
	}

	for i, tst := range nativeTests {
		tc := tst
		t.Run(fmt.Sprintf("[%d]", i), func(t *testing.T) {
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
					t.Fatal(err)
				}
				out, _, err := prg.Eval(tc.in)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(out.Value(), true) {
					t.Errorf("got %v, wanted true for expr: %s", out.Value(), tc.expr)
				}
			}
		})
	}
}

func TestNativeTypesVersion(t *testing.T) {
	_, err := cel.NewEnv(ext.NativeTypes(ext.NativeTypesVersion(0)))
	if err != nil {
		t.Fatalf("NewEnv(NativeTypes(NativeTypesVersion(0))) failed: %v", err)
	}
}

func TestTypeResolutionRace(t *testing.T) {
	customType := reflect.TypeFor[*Custom]()
	env, err := cel.NewEnv(
		cel.Container("types_test"),
		ext.NativeTypes(
			types.ParseStructTag("cel"),
			customType,
		),
	)
	if err != nil {
		t.Fatal("NewEnv:", err)
	}

	tests := []struct {
		name string
		expr string
	}{
		{name: "custom1", expr: `Custom{ name: "name1" }`},
		{name: "custom2", expr: `Custom{ name: "name2" }`},
		{name: "custom3", expr: `Custom{ name: "name3" }`},
		{name: "custom4", expr: `Custom{ name: "name4" }`},
		{name: "custom5", expr: `Custom{ name: "name5" }`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ast, iss := env.Compile(test.expr)
			if err := iss.Err(); err != nil {
				t.Fatal("Compile:", err)
			}
			prg, err := env.Program(ast)
			if err != nil {
				t.Fatalf("env.Program() failed: %s", err)
			}
			prg.Eval(cel.NoVars())
		})
	}
}

func TestNativeToValueDelegatesUnregisteredStructs(t *testing.T) {
	custom := &recordingAdapter{base: types.DefaultTypeAdapter}
	env, err := cel.NewEnv(
		cel.CustomTypeAdapter(custom),
		ext.NativeTypes(reflect.TypeOf(registeredNativeStruct{})),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv() failed: %v", err)
	}
	adapter := env.CELTypeAdapter()

	// An unregistered struct must reach the composed base adapter.
	got := adapter.NativeToValue(unregisteredNativeStruct{Name: "x"})
	if !custom.saw {
		t.Error("base adapter was not consulted for an unregistered struct")
	}
	if got.Equal(types.String("from-base-adapter")) != types.True {
		t.Errorf("NativeToValue(unregisteredNativeStruct) = %v, want the base adapter's value", got)
	}

	// A registered native type must still be wrapped as a native object.
	custom.saw = false
	gotReg := adapter.NativeToValue(registeredNativeStruct{Name: "y"})
	if custom.saw {
		t.Error("base adapter was consulted for a registered native type")
	}
	if tn := gotReg.Type().TypeName(); !strings.Contains(tn, "registeredNativeStruct") {
		t.Errorf("NativeToValue(registeredNativeStruct).Type() = %q, want a native object type", tn)
	}
}

func TestNativeObjectCalculateSize(t *testing.T) {
	env, err := cel.NewEnv(
		ext.NativeTypes(
			reflect.TypeOf(TestAllTypes{}),
			reflect.TypeOf(TestNestedType{}),
			reflect.TypeOf(TestEmbeddedPointerTypes{}),
		),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv() failed: %v", err)
	}
	adapter := env.CELTypeAdapter()

	tests := []struct {
		name string
		val  any
		want uint32
	}{
		{
			name: "empty_struct",
			val:  &TestNestedType{},
			want: 1, // 1 (container)
		},
		{
			name: "nil_embedded_pointer",
			val:  &TestEmbeddedPointerTypes{},
			want: 1, // 1 (container); promoted fields through the nil embedded pointer count as unset
		},
		{
			name: "struct_with_scalar_and_list",
			val: &TestNestedType{
				NestedListVal: []string{"a", "b", "c"},
			},
			want: 5, // 1 (root struct) + ["a", "b", "c"] (1 list container + 3 elements = 4) = 5
		},
		{
			name: "struct_with_nested_map",
			val: &TestNestedType{
				NestedMapVal: map[int64]bool{1: true, 2: false},
			},
			want: 6, // 1 (root struct) + map (1 container + (1+1) + (1+1) = 5) = 6
		},
		{
			name: "nested_struct",
			val: &TestAllTypes{
				StringVal: "hello",
				NestedVal: &TestNestedType{
					NestedListVal: []string{"a", "b"},
				},
			},
			// 1 (root struct) + "hello"(1 unit) + NestedVal(1 container + ["a", "b"](1+2=3) = 4) = 6
			want: 6,
		},
		{
			name: "bytes_and_time",
			val: &TestAllTypes{
				BytesVal:     []byte("test"),
				DurationVal:  time.Second,
				TimestampVal: time.Unix(100, 0),
			},
			// 1 (root struct) + "test"(1 unit) + duration(1) + timestamp(1) = 4
			want: 4,
		},
		{
			name: "slice_of_structs",
			val: &TestAllTypes{
				ListVal: []*TestNestedType{
					{NestedListVal: []string{"x"}},
					{NestedListVal: []string{"y", "z"}},
				},
			},
			want: 9,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val := adapter.NativeToValue(tc.val)
			sizer, ok := val.(types.AggregateSizeVisitor)
			if !ok {
				t.Fatalf("expected types.AggregateSizeVisitor implementation for %T", val)
			}
			if got := sizer.AggregateSize(types.NewSizeCalculator()); got != tc.want {
				t.Errorf("got aggregate size %d, want %d", got, tc.want)
			}
		})
	}
}

func BenchmarkNativeTypesEval(b *testing.B) {
	benchmarks := []struct {
		name    string
		expr    string
		in      any
		envOpts []any
	}{
		{
			name: "FieldAccess",
			expr: "t.Int32Val + t.Int64Val",
			in: map[string]any{
				"t": &TestAllTypes{Int32Val: 10, Int64Val: 20},
			},
		},
		{
			name: "NestedFieldAccess",
			expr: "t.NestedVal.NestedCustomName == 'name'",
			in: map[string]any{
				"t": &TestAllTypes{
					NestedVal: &TestNestedType{NestedCustomName: "name"},
				},
			},
		},
		{
			name: "StructCreation",
			expr: `types_test.TestAllTypes{
				BoolVal: true,
				Int32Val: 10,
				Int64Val: 20,
				StringVal: 'hello world',
			}`,
		},
		{
			name: "FieldPresence",
			expr: "has(t.BoolVal) && has(t.NestedVal)",
			in: map[string]any{
				"t": &TestAllTypes{
					BoolVal:   true,
					NestedVal: &TestNestedType{},
				},
			},
		},
		{
			name:    "StructTagFieldAccess",
			expr:    "t.custom_name == 'name'",
			envOpts: []any{types.ParseStructTags(true)},
			in: map[string]any{
				"t": &TestAllTypes{CustomName: "name"},
			},
		},
		{
			name: "ListExists",
			expr: "tests.exists(t, t.Int32Val > 15)",
			in: map[string]any{
				"tests": []*TestAllTypes{
					{Int32Val: 10},
					{Int32Val: 20},
				},
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			envOpts := append([]any{
				cel.Variable("t", cel.ObjectType("types_test.TestAllTypes")),
			}, bm.envOpts...)
			env := testNativeEnv(b, envOpts...)
			ast, iss := env.Compile(bm.expr)
			if iss.Err() != nil {
				b.Fatalf("env.Compile(%q) failed: %v", bm.expr, iss.Err())
			}
			prg, err := env.Program(ast, cel.EvalOptions(cel.OptOptimize))
			if err != nil {
				b.Fatalf("env.Program() failed: %v", err)
			}
			input := bm.in
			if input == nil {
				input = cel.NoVars()
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				prg.Eval(input)
			}
		})
	}
}

func BenchmarkNativeToValue(b *testing.B) {
	env := testNativeEnv(b)
	adapter := env.CELTypeAdapter()

	nested := &TestNestedType{
		NestedListVal:    []string{"a", "b", "c"},
		NestedMapVal:     map[int64]bool{1: true},
		NestedCustomName: "test",
	}
	allTypes := &TestAllTypes{
		BoolVal:   true,
		Int32Val:  10,
		Int64Val:  20,
		StringVal: "hello world",
		NestedVal: nested,
		ListVal:   []*TestNestedType{nested},
	}
	allTypesSlice := []*TestAllTypes{allTypes, allTypes}

	benchmarks := []struct {
		name string
		val  any
	}{
		{name: "TestNestedType", val: nested},
		{name: "TestAllTypes", val: allTypes},
		{name: "SliceTestAllTypes", val: allTypesSlice},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				adapter.NativeToValue(bm.val)
			}
		})
	}
}

func BenchmarkConvertToNative(b *testing.B) {
	env := testNativeEnv(b)
	adapter := env.CELTypeAdapter()

	allTypes := &TestAllTypes{
		BoolVal:   true,
		Int32Val:  10,
		Int64Val:  20,
		StringVal: "hello world",
	}
	celVal := adapter.NativeToValue(allTypes)
	targetType := reflect.TypeOf(&TestAllTypes{})

	allTypesSlice := []*TestAllTypes{allTypes, allTypes}
	celSliceVal := adapter.NativeToValue(allTypesSlice)
	sliceTargetType := reflect.TypeOf([]*TestAllTypes{})

	b.Run("TestAllTypes", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := celVal.ConvertToNative(targetType)
			if err != nil {
				b.Fatalf("ConvertToNative failed: %v", err)
			}
		}
	})

	b.Run("SliceTestAllTypes", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, err := celSliceVal.ConvertToNative(sliceTargetType)
			if err != nil {
				b.Fatalf("ConvertToNative failed: %v", err)
			}
		}
	})
}

// testEnv initializes the test environment common to all tests.
func testNativeEnv(t testing.TB, opts ...any) *cel.Env {
	t.Helper()
	envOpts := []cel.EnvOption{
		cel.Container("types_test"),
		cel.Abbrevs("google.expr.proto3.test"),
		cel.Types(&proto3pb.TestAllTypes{}),
		cel.Variable("tests", cel.ListType(cel.ObjectType("types_test.TestAllTypes"))),
	}
	nativeOpts := []any{
		reflect.ValueOf(&TestAllTypes{}),
		reflect.ValueOf(&TestRefValFieldType{}),
		reflect.ValueOf(&TestEmbeddedTypes{}),
		reflect.ValueOf(&TestEmbeddedPointerTypes{}),
		reflect.ValueOf(&TestSpecialJSONTags{}),
	}
	for _, o := range opts {
		switch opt := o.(type) {
		case types.NativeTypeOption:
			nativeOpts = append(nativeOpts, opt)
		case cel.EnvOption:
			envOpts = append(envOpts, opt)
		default:
			t.Fatalf("invalid option type: %s", reflect.TypeOf(o).Name())
		}
	}

	envOpts = append(envOpts,
		ext.NativeTypes(
			nativeOpts...,
		),
	)
	env, err := cel.NewEnv(envOpts...)
	if err != nil {
		t.Fatalf("cel.NewEnv(NativeTypes()) failed: %v", err)
	}
	return env
}

func mustParseTime(t *testing.T, timestamp string) time.Time {
	t.Helper()
	out, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatalf("time.Parse(%q) failed: %v", timestamp, err)
	}
	return out
}

type Custom struct {
	Name string `cel:"name"`
}

type TestStructWithMultipleSameNames struct {
	Name       string
	CustomName string `cel:"Name"`
}

type TestNestedType struct {
	NestedListVal    []string
	NestedMapVal     map[int64]bool
	NestedCustomName string `cel:"custom_name" json:"custom_name,omitempty"`
}

type TestAllTypes struct {
	NestedVal       *TestNestedType `json:"nestedVal,omitempty"`
	NestedStructVal TestNestedType  `json:"nestedStructVal,omitempty"`
	BoolVal         bool            `json:"boolVal"`
	BytesVal        []byte
	DurationVal     time.Duration
	DoubleVal       float64
	FloatVal        float32
	Int32Val        int32
	Int64Val        int64
	StringVal       string
	TimestampVal    time.Time
	Uint32Val       uint32
	Uint64Val       uint64
	ListVal         []*TestNestedType
	ArrayVal        [1]*TestNestedType
	BytesArrayVal   [4]byte
	MapVal          map[string]TestAllTypes
	PbVal           *proto3pb.TestAllTypes
	CustomSliceVal  []TestNestedSliceType
	CustomMapVal    map[string]TestMapVal
	CustomName      string `cel:"custom_name"`

	// channel types are not supported
	UnsupportedVal     chan string
	UnsupportedListVal []chan string
	UnsupportedMapVal  map[int]chan string

	// unexported types can be found but not set or accessed
	privateVal map[string]string
}

type TestNestedSliceType struct {
	Value string
}

type TestMapVal struct {
	Value string
}

type TestEmbeddedTypes struct {
	Custom
	TestNestedType `json:"embedded,omitempty"`
	Skipped        string `json:"-"`
}

type TestEmbeddedPointerTypes struct {
	*TestNestedType `json:"embedded,omitempty"`
}

type TestSpecialJSONTags struct {
	Ignored              string `json:"-" cel:"ignored"`
	HyphenName           string `json:"-," cel:"hyphen_name"`
	QuotedHyphen         string `json:"'-'" cel:"quoted_hyphen"`
	Renamed              string `json:"custom_json_name" cel:"renamed"`
	OmitEmptyInt         int    `json:"empty_int,omitempty" cel:"empty_int"`
	OmitEmptyStr         string `json:"empty_str,omitempty" cel:"empty_str"`
	PopulatedInt         int    `json:"pop_int,omitempty" cel:"pop_int"`
	NonOmitEmptyZero     int    `json:"keep_zero" cel:"keep_zero"`
	NonOmitEmptyEmptyStr string `json:"keep_empty" cel:"keep_empty"`
	NonOmitEmptyFalse    bool   `json:"keep_false" cel:"keep_false"`
	DivergentField       string `cel:"cel_field" json:"json_field"`
}

type TestRefValFieldType struct {
	OptionalName *types.Optional `cel:"optional_name"`
	IntVal       types.Int
	CELTime      types.Timestamp `cel:"time"`
}

// registeredNativeStruct is registered with NativeTypes in the delegation test.
type registeredNativeStruct struct {
	Name string
}

// unregisteredNativeStruct is not registered, so NativeToValue should hand it to
// the composed base adapter rather than wrapping it as a native object.
type unregisteredNativeStruct struct {
	Name string
}

// recordingAdapter converts unregisteredNativeStruct into a sentinel string and
// records that it was asked to, so the test can confirm nativeTypeProvider
// delegated the value. Everything else falls through to the base adapter.
type recordingAdapter struct {
	base types.Adapter
	saw  bool
}

func (a *recordingAdapter) NativeToValue(value any) ref.Val {
	if _, ok := value.(unregisteredNativeStruct); ok {
		a.saw = true
		return types.String("from-base-adapter")
	}
	return a.base.NativeToValue(value)
}

func TestNativeTypeAlias(t *testing.T) {
	type CustomStruct struct {
		Name string
	}

	desc := types.NativeTypeFor[CustomStruct](types.NativeTypeAlias("custom.MyStruct"))
	if desc.ReflectType() != reflect.TypeFor[CustomStruct]() {
		t.Fatalf("ReflectType() got %v, wanted %v", desc.ReflectType(), reflect.TypeFor[CustomStruct]())
	}

	nt, err := types.NewNativeType(desc.ReflectType(), desc.Options()...)
	if err != nil {
		t.Fatalf("NewNativeType() failed: %v", err)
	}

	if nt.TypeName() != "custom.MyStruct" {
		t.Errorf("nt.TypeName() got %s, wanted custom.MyStruct", nt.TypeName())
	}
	if nt.String() != "custom.MyStruct" {
		t.Errorf("nt.String() got %s, wanted custom.MyStruct", nt.String())
	}
}

type iterableOnlyWrapper struct {
	ref.Val
	iterable traits.Iterable
}

func (w iterableOnlyWrapper) Iterator() traits.Iterator {
	return w.iterable.Iterator()
}

func BenchmarkListComprehensions(b *testing.B) {
	env := testNativeEnv(b,
		cel.Variable("ptrList", cel.ListType(cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("valList", cel.ListType(cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("strList", cel.ListType(cel.StringType)),
		cel.Variable("intList", cel.ListType(cel.IntType)),
	)
	adapter := env.CELTypeAdapter()

	ptrSlice := make([]*TestAllTypes, 10)
	valSlice := make([]TestAllTypes, 10)
	strSlice := make([]string, 10)
	intSlice := make([]int64, 10)
	for i := 0; i < 10; i++ {
		v := int32(i)
		ptrSlice[i] = &TestAllTypes{Int32Val: v}
		valSlice[i] = TestAllTypes{Int32Val: v}
		strSlice[i] = fmt.Sprintf("item-%d", i)
		intSlice[i] = int64(i * 100)
	}

	ptrLister := types.NewList(adapter, ptrSlice)
	valLister := types.NewList(adapter, valSlice)
	strLister := types.NewList(adapter, strSlice)
	intLister := types.NewList(adapter, intSlice)

	cases := []struct {
		name string
		expr string
		in   map[string]any
	}{
		{
			name: "PtrStruct/Foldable",
			expr: "ptrList.exists(x, x.Int32Val == 9)",
			in:   map[string]any{"ptrList": ptrLister},
		},
		{
			name: "PtrStruct/Iterable",
			expr: "ptrList.exists(x, x.Int32Val == 9)",
			in:   map[string]any{"ptrList": iterableOnlyWrapper{Val: ptrLister, iterable: ptrLister}},
		},
		{
			name: "ValStruct/Foldable",
			expr: "valList.exists(x, x.Int32Val == 9)",
			in:   map[string]any{"valList": valLister},
		},
		{
			name: "ValStruct/Iterable",
			expr: "valList.exists(x, x.Int32Val == 9)",
			in:   map[string]any{"valList": iterableOnlyWrapper{Val: valLister, iterable: valLister}},
		},
		{
			name: "String/Foldable",
			expr: "strList.exists(x, x == 'item-9')",
			in:   map[string]any{"strList": strLister},
		},
		{
			name: "String/Iterable",
			expr: "strList.exists(x, x == 'item-9')",
			in:   map[string]any{"strList": iterableOnlyWrapper{Val: strLister, iterable: strLister}},
		},
		{
			name: "Int64/Foldable",
			expr: "intList.exists(x, x == 900)",
			in:   map[string]any{"intList": intLister},
		},
		{
			name: "Int64/Iterable",
			expr: "intList.exists(x, x == 900)",
			in:   map[string]any{"intList": iterableOnlyWrapper{Val: intLister, iterable: intLister}},
		},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				b.Fatalf("Compile(%q) failed: %v", tc.expr, iss.Err())
			}
			prg, err := env.Program(ast, cel.EvalOptions(cel.OptOptimize))
			if err != nil {
				b.Fatalf("Program() failed: %v", err)
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				out, _, err := prg.Eval(tc.in)
				if err != nil || out != types.True {
					b.Fatalf("Eval got (%v, %v), want true", out, err)
				}
			}
		})
	}
}

type directIfaceStruct struct {
	Ptr *int32
}

func TestSliceListElemTypePtrAndDirectIface(t *testing.T) {
	v1 := int32(10)
	v2 := int32(20)
	env, err := cel.NewEnv(
		cel.Types(
			reflect.TypeFor[TestAllTypes](),
			reflect.TypeFor[directIfaceStruct](),
		),
		cel.Variable("valList", cel.ListType(cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("directList", cel.ListType(cel.ObjectType("types_test.directIfaceStruct"))),
	)
	if err != nil {
		t.Fatalf("cel.NewEnv() failed: %v", err)
	}
	adapter := env.CELTypeAdapter()

	valSlice := []TestAllTypes{
		{Int32Val: 10, StringVal: "first"},
		{Int32Val: 20, StringVal: "second"},
	}
	valLister := types.NewList(adapter, valSlice)

	// 1. Verify Get(0).Value() returns a copy and mutating it does not mutate the backing slice.
	elem0 := valLister.Get(types.Int(0)).Value().(TestAllTypes)
	elem0.Int32Val = 999
	if valSlice[0].Int32Val != 10 {
		t.Errorf("mutating Get(0).Value() modified backing slice: got %d, want 10", valSlice[0].Int32Val)
	}

	// 2. Verify Contains and Equal on value-struct sliceList.
	if valLister.Contains(adapter.NativeToValue(TestAllTypes{Int32Val: 20, StringVal: "second"})) != types.True {
		t.Errorf("valLister.Contains() got false, want true")
	}
	valListerCopy := types.NewList(adapter, []TestAllTypes{
		{Int32Val: 10, StringVal: "first"},
		{Int32Val: 20, StringVal: "second"},
	})
	if valLister.Equal(valListerCopy) != types.True {
		t.Errorf("valLister.Equal(valListerCopy) got false, want true")
	}

	// 3. Verify filter comprehension returning value structs and ConvertToNative back to []TestAllTypes.
	ast, iss := env.Compile("valList.filter(x, x.Int32Val > 15)")
	if iss.Err() != nil {
		t.Fatalf("Compile failed: %v", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		t.Fatalf("Program failed: %v", err)
	}
	out, _, err := prg.Eval(map[string]any{"valList": valLister})
	if err != nil {
		t.Fatalf("Eval failed: %v", err)
	}
	filteredNative, err := out.ConvertToNative(reflect.TypeFor[[]TestAllTypes]())
	if err != nil {
		t.Fatalf("ConvertToNative([]TestAllTypes) failed: %v", err)
	}
	filteredSlice := filteredNative.([]TestAllTypes)
	if len(filteredSlice) != 1 || filteredSlice[0].Int32Val != 20 || filteredSlice[0].StringVal != "second" {
		t.Errorf("filteredSlice got %+v, want [{Int32Val:20 StringVal:second}]", filteredSlice)
	}
	// Mutating filteredSlice must not affect original valSlice.
	filteredSlice[0].Int32Val = 777
	if valSlice[1].Int32Val != 20 {
		t.Errorf("mutating filteredSlice modified original valSlice: got %d, want 20", valSlice[1].Int32Val)
	}

	// 4. Verify direct-interface struct (single pointer field, isDirectIface == true).
	directSlice := []directIfaceStruct{{Ptr: &v1}, {Ptr: &v2}}
	directLister := types.NewList(adapter, directSlice)
	astDirect, iss := env.Compile("directList.exists(d, d.Ptr == 20)")
	if iss.Err() != nil {
		t.Fatalf("Compile directList failed: %v", iss.Err())
	}
	prgDirect, err := env.Program(astDirect)
	if err != nil {
		t.Fatalf("Program directList failed: %v", err)
	}
	outDirect, _, err := prgDirect.Eval(map[string]any{"directList": directLister})
	if err != nil || outDirect != types.True {
		t.Errorf("directList.exists got (%v, %v), want true", outDirect, err)
	}

	// 5. Verify zero-sized struct slice []struct{}.
	emptyStructLister := types.NewList(adapter, []struct{}{{}, {}})
	if emptyStructLister.Size() != types.Int(2) {
		t.Errorf("emptyStructLister.Size() got %v, want 2", emptyStructLister.Size())
	}
}

type iterableOnlyMapper struct {
	traits.Mapper
}

func (m iterableOnlyMapper) FindStringKey(s string) (any, bool) {
	if nm, ok := m.Mapper.(interface{ FindStringKey(string) (any, bool) }); ok {
		return nm.FindStringKey(s)
	}
	return nil, false
}

func (m iterableOnlyMapper) FindInt64Key(ik int64) (any, bool) {
	if nm, ok := m.Mapper.(interface{ FindInt64Key(int64) (any, bool) }); ok {
		return nm.FindInt64Key(ik)
	}
	return nil, false
}

func (m iterableOnlyMapper) FindNative(key any) (any, bool) {
	if nm, ok := m.Mapper.(interface{ FindNative(any) (any, bool) }); ok {
		return nm.FindNative(key)
	}
	return nil, false
}

func TestNativeMapGeneric(t *testing.T) {
	env := testNativeEnv(
		t,
		cel.OptionalTypes(),
		ext.TwoVarComprehensions(),
		cel.Variable("valMap", cel.MapType(cel.StringType, cel.ObjectType("types_test.TestAllTypes"))),
	)
	adapter := env.CELTypeAdapter()

	valMap := map[string]TestAllTypes{
		"first":  {Int32Val: 10, StringVal: "alpha"},
		"second": {Int32Val: 20, StringVal: "beta"},
	}
	mapper := types.NewMap(adapter, valMap)

	// 1. Verify Get returns an independent copy (does not mutate backing map).
	got := mapper.Get(types.String("first"))
	if types.IsError(got) {
		t.Fatalf("mapper.Get('first') failed: %v", got)
	}
	gotStruct, ok := got.Value().(TestAllTypes)
	if !ok {
		t.Fatalf("mapper.Get('first').Value() got type %T, want TestAllTypes", got.Value())
	}
	gotStruct.Int32Val = 999
	if valMap["first"].Int32Val != 10 {
		t.Errorf("mutating Get('first').Value() modified backing map: got %d, want 10", valMap["first"].Int32Val)
	}

	// 2. Verify Contains and Equal.
	if mapper.Contains(types.String("second")) != types.True {
		t.Errorf("mapper.Contains('second') got false, want true")
	}
	mapperCopy := types.NewMap(adapter, map[string]TestAllTypes{
		"first":  {Int32Val: 10, StringVal: "alpha"},
		"second": {Int32Val: 20, StringVal: "beta"},
	})
	if mapper.Equal(mapperCopy) != types.True {
		t.Errorf("mapper.Equal(mapperCopy) got false, want true")
	}

	// 3. Verify 1-variable comprehension (FoldKeyOnly fast-path).
	ast1, iss := env.Compile("valMap.exists(k, k == 'second')")
	if iss.Err() != nil {
		t.Fatalf("Compile 1-var failed: %v", iss.Err())
	}
	prg1, err := env.Program(ast1)
	if err != nil {
		t.Fatalf("Program 1-var failed: %v", err)
	}
	out1, _, err := prg1.Eval(map[string]any{"valMap": mapper})
	if err != nil || out1 != types.True {
		t.Errorf("1-var exists got (%v, %v), want true", out1, err)
	}

	// 4. Verify 2-variable comprehension (zero-copy valTypePtr fast-path).
	ast2, iss := env.Compile("valMap.exists(k, v, k == 'second' && v.Int32Val == 20)")
	if iss.Err() != nil {
		t.Fatalf("Compile 2-var failed: %v", iss.Err())
	}
	prg2, err := env.Program(ast2)
	if err != nil {
		t.Fatalf("Program 2-var failed: %v", err)
	}
	out2, _, err := prg2.Eval(map[string]any{"valMap": mapper})
	if err != nil || out2 != types.True {
		t.Errorf("2-var exists got (%v, %v), want true", out2, err)
	}

	// 5. Verify transformMap returning value structs and ConvertToNative back to map[string]TestAllTypes
	// ensuring no stack aliasing across loop iterations.
	astTrans, iss := env.Compile("valMap.transformMap(k, v, v.Int32Val > 15, v)")
	if iss.Err() != nil {
		t.Fatalf("Compile transformMap failed: %v", iss.Err())
	}
	prgTrans, err := env.Program(astTrans)
	if err != nil {
		t.Fatalf("Program transformMap failed: %v", err)
	}
	outTrans, _, err := prgTrans.Eval(map[string]any{"valMap": mapper})
	if err != nil {
		t.Fatalf("Eval transformMap failed: %v", err)
	}
	transNative, err := outTrans.ConvertToNative(reflect.TypeFor[map[string]TestAllTypes]())
	if err != nil {
		t.Fatalf("ConvertToNative(map[string]TestAllTypes) failed: %v", err)
	}
	transMap := transNative.(map[string]TestAllTypes)
	if len(transMap) != 1 || transMap["second"].Int32Val != 20 || transMap["second"].StringVal != "beta" {
		t.Errorf("transMap got %+v, want map[second:{Int32Val:20 StringVal:beta}]", transMap)
	}
}

func BenchmarkMapComprehensions(b *testing.B) {
	env := testNativeEnv(
		b,
		cel.OptionalTypes(),
		ext.TwoVarComprehensions(),
		cel.Variable("ptrMap", cel.MapType(cel.StringType, cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("valMap", cel.MapType(cel.StringType, cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("strMap", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("intMap", cel.MapType(cel.IntType, cel.BoolType)),
	)
	adapter := env.CELTypeAdapter()

	ptrMap := make(map[string]*TestAllTypes, 10)
	valMap := make(map[string]TestAllTypes, 10)
	strMap := make(map[string]string, 10)
	intMap := make(map[int64]bool, 10)
	for i := 0; i < 10; i++ {
		k := fmt.Sprintf("k%d", i)
		v := fmt.Sprintf("v%d", i)
		ptrMap[k] = &TestAllTypes{Int32Val: int32(i), StringVal: v}
		valMap[k] = TestAllTypes{Int32Val: int32(i), StringVal: v}
		strMap[k] = v
		intMap[int64(i)] = (i == 9)
	}

	ptrMapper := types.NewMap(adapter, ptrMap)
	valMapper := types.NewMap(adapter, valMap)
	strMapper := types.NewMap(adapter, strMap)
	intMapper := types.NewMap(adapter, intMap)

	ptrLegacy := types.NewDynamicMap(adapter, ptrMap)
	valLegacy := types.NewDynamicMap(adapter, valMap)
	strLegacy := types.NewDynamicMap(adapter, strMap)
	intLegacy := types.NewDynamicMap(adapter, intMap)

	compileBench := func(expr string) cel.Program {
		ast, iss := env.Compile(expr)
		if iss.Err() != nil {
			b.Fatalf("Compile(%q) failed: %v", expr, iss.Err())
		}
		prg, err := env.Program(ast, cel.EvalOptions(cel.OptOptimize))
		if err != nil {
			b.Fatalf("Program(%q) failed: %v", expr, err)
		}
		return prg
	}

	prgPtr1Var := compileBench("ptrMap.exists(k, ptrMap[k].Int32Val == 9)")
	prgPtr2Var := compileBench("ptrMap.exists(k, v, v.Int32Val == 9)")
	prgVal1Var := compileBench("valMap.exists(k, valMap[k].Int32Val == 9)")
	prgVal2Var := compileBench("valMap.exists(k, v, v.Int32Val == 9)")
	prgStr1Var := compileBench("strMap.exists(k, k == 'k9')")
	prgStr2Var := compileBench("strMap.exists(k, v, v == 'v9')")
	prgInt1Var := compileBench("intMap.exists(k, intMap[k])")
	prgInt2Var := compileBench("intMap.exists(k, v, v)")

	cases := []struct {
		name string
		prg  cel.Program
		vars map[string]any
	}{
		{"PtrStruct_1Var_LegacyReflect", prgPtr1Var, map[string]any{"ptrMap": ptrLegacy}},
		{"PtrStruct_1Var_Iterable", prgPtr1Var, map[string]any{"ptrMap": iterableOnlyMapper{ptrMapper}}},
		{"PtrStruct_1Var_Foldable", prgPtr1Var, map[string]any{"ptrMap": ptrMapper}},
		{"PtrStruct_2Var_LegacyReflect", prgPtr2Var, map[string]any{"ptrMap": ptrLegacy}},
		{"PtrStruct_2Var_Foldable", prgPtr2Var, map[string]any{"ptrMap": ptrMapper}},
		{"ValStruct_1Var_LegacyReflect", prgVal1Var, map[string]any{"valMap": valLegacy}},
		{"ValStruct_1Var_Iterable", prgVal1Var, map[string]any{"valMap": iterableOnlyMapper{valMapper}}},
		{"ValStruct_1Var_Foldable", prgVal1Var, map[string]any{"valMap": valMapper}},
		{"ValStruct_2Var_LegacyReflect", prgVal2Var, map[string]any{"valMap": valLegacy}},
		{"ValStruct_2Var_Foldable", prgVal2Var, map[string]any{"valMap": valMapper}},
		{"StringMap_1Var_LegacyReflect", prgStr1Var, map[string]any{"strMap": strLegacy}},
		{"StringMap_1Var_Iterable", prgStr1Var, map[string]any{"strMap": iterableOnlyMapper{strMapper}}},
		{"StringMap_1Var_Foldable", prgStr1Var, map[string]any{"strMap": strMapper}},
		{"StringMap_2Var_LegacyReflect", prgStr2Var, map[string]any{"strMap": strLegacy}},
		{"StringMap_2Var_Foldable", prgStr2Var, map[string]any{"strMap": strMapper}},
		{"Int64Bool_1Var_LegacyReflect", prgInt1Var, map[string]any{"intMap": intLegacy}},
		{"Int64Bool_1Var_Iterable", prgInt1Var, map[string]any{"intMap": iterableOnlyMapper{intMapper}}},
		{"Int64Bool_1Var_Foldable", prgInt1Var, map[string]any{"intMap": intMapper}},
		{"Int64Bool_2Var_LegacyReflect", prgInt2Var, map[string]any{"intMap": intLegacy}},
		{"Int64Bool_2Var_Foldable", prgInt2Var, map[string]any{"intMap": intMapper}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				out, _, err := tc.prg.Eval(tc.vars)
				if err != nil || out != types.True {
					b.Fatalf("Eval got (%v, %v), want true", out, err)
				}
			}
		})
	}
}

func BenchmarkMapLookup(b *testing.B) {
	env := testNativeEnv(
		b,
		cel.Variable("ptrMap", cel.MapType(cel.StringType, cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("valMap", cel.MapType(cel.StringType, cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("strMap", cel.MapType(cel.StringType, cel.StringType)),
		cel.Variable("intMap", cel.MapType(cel.IntType, cel.BoolType)),
	)
	adapter := env.CELTypeAdapter()

	ptrMap := map[string]*TestAllTypes{"k9": {Int32Val: 9}}
	valMap := map[string]TestAllTypes{"k9": {Int32Val: 9}}
	strMap := map[string]string{"k9": "v9"}
	intMap := map[int64]bool{9: true}

	ptrMapper := types.NewMap(adapter, ptrMap)
	valMapper := types.NewMap(adapter, valMap)
	strMapper := types.NewMap(adapter, strMap)
	intMapper := types.NewMap(adapter, intMap)

	ptrLegacy := types.NewDynamicMap(adapter, ptrMap)
	valLegacy := types.NewDynamicMap(adapter, valMap)
	strLegacy := types.NewDynamicMap(adapter, strMap)
	intLegacy := types.NewDynamicMap(adapter, intMap)

	compileBench := func(expr string) cel.Program {
		ast, iss := env.Compile(expr)
		if iss.Err() != nil {
			b.Fatalf("Compile(%q) failed: %v", expr, iss.Err())
		}
		prg, err := env.Program(ast, cel.EvalOptions(cel.OptOptimize))
		if err != nil {
			b.Fatalf("Program(%q) failed: %v", expr, err)
		}
		return prg
	}

	prgPtr := compileBench("ptrMap['k9'].Int32Val == 9")
	prgVal := compileBench("valMap['k9'].Int32Val == 9")
	prgStr := compileBench("strMap['k9'] == 'v9'")
	prgInt := compileBench("intMap[9] == true")

	cases := []struct {
		name string
		prg  cel.Program
		vars map[string]any
	}{
		{"PtrStruct_LegacyReflect", prgPtr, map[string]any{"ptrMap": ptrLegacy}},
		{"PtrStruct_NewMap", prgPtr, map[string]any{"ptrMap": ptrMapper}},
		{"ValStruct_LegacyReflect", prgVal, map[string]any{"valMap": valLegacy}},
		{"ValStruct_NewMap", prgVal, map[string]any{"valMap": valMapper}},
		{"StringMap_LegacyReflect", prgStr, map[string]any{"strMap": strLegacy}},
		{"StringMap_NewMap", prgStr, map[string]any{"strMap": strMapper}},
		{"Int64Bool_LegacyReflect", prgInt, map[string]any{"intMap": intLegacy}},
		{"Int64Bool_NewMap", prgInt, map[string]any{"intMap": intMapper}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				out, _, err := tc.prg.Eval(tc.vars)
				if err != nil || out != types.True {
					b.Fatalf("Eval got (%v, %v), want true", out, err)
				}
			}
		})
	}
}

func TestNewDynamicListAdaptations(t *testing.T) {
	env := testNativeEnv(
		t,
		cel.Variable("ptrList", cel.ListType(cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("valList", cel.ListType(cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("entity", cel.ObjectType("types_test.TestAllTypes")),
	)
	adapter := env.CELTypeAdapter()

	ptrSlice := []*TestAllTypes{{Int32Val: 1}, {Int32Val: 2}, {Int32Val: 9}}
	valSlice := []TestAllTypes{{Int32Val: 1}, {Int32Val: 2}, {Int32Val: 9}}
	entity := &TestAllTypes{
		ListVal: []*TestNestedType{
			{NestedCustomName: "a"},
			{NestedCustomName: "b"},
			{NestedCustomName: "target"},
		},
		CustomSliceVal: []TestNestedSliceType{
			{Value: "x"},
			{Value: "y"},
			{Value: "target"},
		},
	}

	vars := map[string]any{
		"ptrList": types.NewDynamicList(adapter, ptrSlice),
		"valList": types.NewDynamicList(adapter, valSlice),
		"entity":  entity,
	}

	exprs := []string{
		"ptrList.exists(x, x.Int32Val == 9)",
		"ptrList[2].Int32Val == 9",
		"valList.exists(x, x.Int32Val == 9)",
		"valList[2].Int32Val == 9",
		"entity.ListVal.exists(x, x.NestedCustomName == 'target')",
		"entity.ListVal[2].NestedCustomName == 'target'",
		"entity.CustomSliceVal.exists(x, x.Value == 'target')",
		"entity.CustomSliceVal[2].Value == 'target'",
	}

	for _, expr := range exprs {
		ast, iss := env.Compile(expr)
		if iss.Err() != nil {
			t.Fatalf("Compile(%q) failed: %v", expr, iss.Err())
		}
		prg, err := env.Program(ast)
		if err != nil {
			t.Fatalf("Program(%q) failed: %v", expr, err)
		}
		out, _, err := prg.Eval(vars)
		if err != nil || out != types.True {
			t.Fatalf("Eval(%q) got (%v, %v), want true", expr, out, err)
		}
	}
}

func BenchmarkNewDynamicList(b *testing.B) {
	env := testNativeEnv(
		b,
		cel.Variable("ptrList", cel.ListType(cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("valList", cel.ListType(cel.ObjectType("types_test.TestAllTypes"))),
		cel.Variable("strList", cel.ListType(cel.StringType)),
		cel.Variable("entity", cel.ObjectType("types_test.TestAllTypes")),
	)
	adapter := env.CELTypeAdapter()

	var ptrArr [10]*TestAllTypes
	var valArr [10]TestAllTypes
	var strArr [10]string
	ptrSlice := make([]*TestAllTypes, 10)
	valSlice := make([]TestAllTypes, 10)
	strSlice := make([]string, 10)
	nestedPtrs := make([]*TestNestedType, 10)
	nestedVals := make([]TestNestedSliceType, 10)

	for i := 0; i < 10; i++ {
		s := fmt.Sprintf("item-%d", i)
		tVal := TestAllTypes{Int32Val: int32(i), StringVal: s}
		valArr[i] = tVal
		valSlice[i] = tVal
		ptrArr[i] = &valSlice[i]
		ptrSlice[i] = &valSlice[i]
		strArr[i] = s
		strSlice[i] = s
		nestedPtrs[i] = &TestNestedType{NestedCustomName: s}
		nestedVals[i] = TestNestedSliceType{Value: s}
	}

	entity := &TestAllTypes{
		ListVal:        nestedPtrs,
		CustomSliceVal: nestedVals,
	}

	// Compare legacy reflection-backed baseList (NewLegacyDynamicList) against
	// the adapted NewDynamicList (backed by NewList / sliceList) on identical slices.
	ptrSliceList := types.NewDynamicList(adapter, ptrSlice)
	valSliceList := types.NewDynamicList(adapter, valSlice)
	strSliceList := types.NewDynamicList(adapter, strSlice)

	compileBench := func(expr string) cel.Program {
		ast, iss := env.Compile(expr)
		if iss.Err() != nil {
			b.Fatalf("Compile(%q) failed: %v", expr, iss.Err())
		}
		prg, err := env.Program(ast, cel.EvalOptions(cel.OptOptimize))
		if err != nil {
			b.Fatalf("Program(%q) failed: %v", expr, err)
		}
		return prg
	}

	prgPtrFold := compileBench("ptrList.exists(x, x.Int32Val == 9)")
	prgPtrIter := compileBench("ptrList.all(x, x.Int32Val >= 0 && x.Int32Val < 10)")
	prgPtrIdx := compileBench("ptrList[9].Int32Val == 9")

	prgValFold := compileBench("valList.exists(x, x.Int32Val == 9)")
	prgValIter := compileBench("valList.all(x, x.Int32Val >= 0 && x.Int32Val < 10)")
	prgValIdx := compileBench("valList[9].Int32Val == 9")

	prgStrFold := compileBench("strList.exists(x, x == 'item-9')")

	prgFieldPtrFold := compileBench("entity.ListVal.exists(x, x.NestedCustomName == 'item-9')")
	prgFieldPtrIdx := compileBench("entity.ListVal[9].NestedCustomName == 'item-9'")
	prgFieldValFold := compileBench("entity.CustomSliceVal.exists(x, x.Value == 'item-9')")
	prgFieldValIdx := compileBench("entity.CustomSliceVal[9].Value == 'item-9'")

	cases := []struct {
		name string
		prg  cel.Program
		vars map[string]any
	}{
		{"PtrStruct_Fold_NewDynamicList", prgPtrFold, map[string]any{"ptrList": ptrSliceList}},
		{"PtrStruct_Iter_NewDynamicList", prgPtrIter, map[string]any{"ptrList": ptrSliceList}},
		{"PtrStruct_Index_NewDynamicList", prgPtrIdx, map[string]any{"ptrList": ptrSliceList}},
		{"ValStruct_Fold_NewDynamicList", prgValFold, map[string]any{"valList": valSliceList}},
		{"ValStruct_Iter_NewDynamicList", prgValIter, map[string]any{"valList": valSliceList}},
		{"ValStruct_Index_NewDynamicList", prgValIdx, map[string]any{"valList": valSliceList}},
		{"String_Fold_NewDynamicList", prgStrFold, map[string]any{"strList": strSliceList}},
		{"StructField_PtrSlice_Fold", prgFieldPtrFold, map[string]any{"entity": entity}},
		{"StructField_PtrSlice_Index", prgFieldPtrIdx, map[string]any{"entity": entity}},
		{"StructField_ValSlice_Fold", prgFieldValFold, map[string]any{"entity": entity}},
		{"StructField_ValSlice_Index", prgFieldValIdx, map[string]any{"entity": entity}},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				out, _, err := tc.prg.Eval(tc.vars)
				if err != nil || out != types.True {
					b.Fatalf("Eval got (%v, %v), want true", out, err)
				}
			}
		})
	}
	b.Run("ConstructAndFold_PtrSlice_NewDynamicList", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := types.NewDynamicList(adapter, ptrSlice)
			out, _, err := prgPtrFold.Eval(map[string]any{"ptrList": l})
			if err != nil || out != types.True {
				b.Fatalf("Eval got (%v, %v), want true", out, err)
			}
		}
	})
	b.Run("ConstructAndFold_ValSlice_NewDynamicList", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			l := types.NewDynamicList(adapter, valSlice)
			out, _, err := prgValFold.Eval(map[string]any{"valList": l})
			if err != nil || out != types.True {
				b.Fatalf("Eval got (%v, %v), want true", out, err)
			}
		}
	})
}
