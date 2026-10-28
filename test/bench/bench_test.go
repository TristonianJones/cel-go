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

package bench

import (
	"testing"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/ext"

	proto3pb "cel.dev/expr/conformance/proto3"
)

func BenchmarkReferenceCases(b *testing.B) {
	stdenv, err := cel.NewEnv()
	if err != nil {
		b.Fatalf("cel.NewEnv() failed: %v", err)
	}
	RunReferenceCases(b, stdenv)
}

func BenchmarkReferenceCheckerCases(b *testing.B) {
	RunReferenceDynamicEnvCases(b)
}

type runtimeBenchmarkCase struct {
	name      string
	expr      string
	msg       *proto3pb.TestAllTypes
	nestedMsg *proto3pb.NestedTestAllTypes
	want      bool
}

func newDefaultBenchMsg() *proto3pb.TestAllTypes {
	return &proto3pb.TestAllTypes{
		SingleInt64:    42,
		SingleString:   "h",
		RepeatedString: []string{"a", "b", "c", "d", "e", "f", "g", "item_42"},
		RepeatedInt64:  []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		RepeatedNestedMessage: []*proto3pb.TestAllTypes_NestedMessage{
			{Bb: 1},
			{Bb: 2},
			{Bb: 3},
			{Bb: 4},
			{Bb: 5},
			{Bb: 6},
			{Bb: 7},
			{Bb: 8},
		},
		MapStringString: map[string]string{
			"k1": "v1",
			"k2": "v2",
			"k3": "v3",
			"k4": "v4",
			"k5": "v5",
			"k6": "v6",
			"k7": "v7",
			"k8": "v8",
		},
	}
}

func newDefaultBenchNestedMsg() *proto3pb.NestedTestAllTypes {
	return &proto3pb.NestedTestAllTypes{
		Payload: &proto3pb.TestAllTypes{
			MapStringString: map[string]string{"env": "prod"},
			RepeatedString:  []string{"alpha", "beta"},
		},
		Child: &proto3pb.NestedTestAllTypes{
			Payload: &proto3pb.TestAllTypes{
				StandaloneMessage: &proto3pb.TestAllTypes_NestedMessage{Bb: 42},
			},
		},
	}
}

func newNestedChain(depth int) *proto3pb.NestedTestAllTypes {
	curr := &proto3pb.NestedTestAllTypes{
		Payload: &proto3pb.TestAllTypes{SingleInt64: 42},
	}
	for range depth {
		curr = &proto3pb.NestedTestAllTypes{Child: curr}
	}
	return curr
}

func runtimeBenchmarkCases() []runtimeBenchmarkCase {
	defaultMsg := newDefaultBenchMsg()
	defaultNested := newDefaultBenchNestedMsg()

	return []runtimeBenchmarkCase{
		{
			name: "POLICY",
			expr: "!(nested_msg.child.payload.single_string in ['10.0.1.4', '10.0.1.5', '10.0.1.6']) && " +
				"((nested_msg.payload.single_string.startsWith('v1') && " +
				"msg.single_string in ['v1', 'v2', 'admin']) || " +
				"(nested_msg.payload.single_string.startsWith('v2') && " +
				"msg.single_string in ['v2', 'admin']) || " +
				"(nested_msg.payload.single_string.startsWith('/admin') && " +
				"msg.single_string == 'admin' && " +
				"nested_msg.child.payload.single_string in ['10.0.1.1', '10.0.1.2', '10.0.1.3']))",
			msg: &proto3pb.TestAllTypes{
				SingleString: "admin",
			},
			nestedMsg: &proto3pb.NestedTestAllTypes{
				Payload: &proto3pb.TestAllTypes{SingleString: "/admin/edit"},
				Child: &proto3pb.NestedTestAllTypes{
					Payload: &proto3pb.TestAllTypes{SingleString: "10.0.1.2"},
				},
			},
			want: true,
		},
		{
			name:      "SCALAR_SELECT",
			expr:      "msg.single_int64 == 42",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "HAS_PRESENT",
			expr:      "has(nested_msg.child.payload.standalone_message.bb)",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "HAS_ABSENT",
			expr:      "has(nested_msg.child.child.payload.standalone_message.bb)",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      false,
		},
		{
			name:      "MAP_INDEX",
			expr:      "nested_msg.payload.map_string_string['env'] == 'prod'",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "REPEATED_INDEX",
			expr:      "nested_msg.payload.repeated_string[1] == 'beta'",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "SET_MEMBERSHIP",
			expr:      "msg.single_string in ['a', 'b', 'c', 'd', 'e', 'f', 'g', 'h']",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "COMPREHENSION_REGEX",
			expr:      "msg.repeated_string.exists(e, e.matches('^item_[0-9]+$'))",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "COMPREHENSION_MAP",
			expr:      "msg.repeated_int64.map(x, x * 2).size() == 16",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "COMPREHENSION_STRING_FILTER",
			expr:      "msg.repeated_string.filter(s, s.startsWith('item_')).size() == 1",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "COMPREHENSION_MESSAGE_FILTER_MAP",
			expr:      "msg.repeated_nested_message.filter(m, m.bb > 4).map(m, m.bb).size() == 4",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "COMPREHENSION_MAP_LOOKUP",
			expr:      "msg.map_string_string.all(k, msg.map_string_string[k].startsWith('v'))",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "COMPREHENSION_TWO_VAR_TRANSFORM_MAP",
			expr:      "msg.map_string_string.transformMap(k, v, k != 'k1', v + '_ok').size() == 7",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name: "OPTIONAL_SELECT",
			expr: "nested_msg.?child.?payload.?standalone_message.?bb.orValue(0) == 42 && " +
				"nested_msg.?child.?child.?payload.?standalone_message.?bb.orValue(0) == 0",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name: "OPTIONAL_INDEX",
			expr: "msg.map_string_string[?'k1'].orValue('') == 'v1' && " +
				"!msg.map_string_string[?'k9'].hasValue() && " +
				"msg.repeated_string[?7].orValue('') == 'item_42' && " +
				"!msg.repeated_string[?8].hasValue()",
			msg:       defaultMsg,
			nestedMsg: defaultNested,
			want:      true,
		},
		{
			name:      "SELECT_DEPTH_2",
			expr:      "nested_msg.child.child.payload.single_int64 == 42",
			msg:       &proto3pb.TestAllTypes{},
			nestedMsg: newNestedChain(2),
			want:      true,
		},
		{
			name: "REPEATED_NESTED_SELECT",
			expr: "nested_msg.child.child.child.payload.single_int64 == 42 && " +
				"nested_msg.child.child.child.payload.single_int64 == 42 && " +
				"nested_msg.child.child.child.payload.single_int64 == 42 && " +
				"nested_msg.child.child.child.payload.single_int64 == 42 && " +
				"nested_msg.child.child.child.payload.single_int64 == 42 && " +
				"nested_msg.child.child.child.payload.single_int64 == 42 && " +
				"nested_msg.child.child.child.payload.single_int64 == 42 && " +
				"nested_msg.child.child.child.payload.single_int64 == 42",
			msg:       &proto3pb.TestAllTypes{},
			nestedMsg: newNestedChain(3),
			want:      true,
		},
		{
			name:      "SELECT_DEPTH_4",
			expr:      "nested_msg.child.child.child.child.payload.single_int64 == 42",
			msg:       &proto3pb.TestAllTypes{},
			nestedMsg: newNestedChain(4),
			want:      true,
		},
		{
			name:      "SELECT_DEPTH_8",
			expr:      "nested_msg.child.child.child.child.child.child.child.child.payload.single_int64 == 42",
			msg:       &proto3pb.TestAllTypes{},
			nestedMsg: newNestedChain(8),
			want:      true,
		},
		{
			name:      "SELECT_DEPTH_16",
			expr:      "nested_msg.child.child.child.child.child.child.child.child.child.child.child.child.child.child.child.child.payload.single_int64 == 42",
			msg:       &proto3pb.TestAllTypes{},
			nestedMsg: newNestedChain(16),
			want:      true,
		},
	}
}

func newRuntimeBenchmarkEnv(tb testing.TB) *cel.Env {
	tb.Helper()
	env, err := cel.NewEnv(
		cel.Types(&proto3pb.TestAllTypes{}, &proto3pb.NestedTestAllTypes{}),
		cel.Variable("msg", cel.ObjectType("cel.expr.conformance.proto3.TestAllTypes")),
		cel.Variable("nested_msg", cel.ObjectType("cel.expr.conformance.proto3.NestedTestAllTypes")),
		cel.OptionalTypes(),
		ext.Bindings(),
		ext.TwoVarComprehensions(),
	)
	if err != nil {
		tb.Fatalf("cel.NewEnv() failed: %v", err)
	}
	return env
}

func TestRuntimeBenchmarks(t *testing.T) {
	env := newRuntimeBenchmarkEnv(t)
	optsByLevel := []struct {
		name string
		opts []cel.ProgramOption
	}{
		{name: "Default"},
		{name: "OptOptimize", opts: []cel.ProgramOption{cel.EvalOptions(cel.OptOptimize)}},
	}

	for _, tc := range runtimeBenchmarkCases() {
		for _, lvl := range optsByLevel {
			t.Run(tc.name+"/"+lvl.name, func(t *testing.T) {
				ast, iss := env.Compile(tc.expr)
				if iss.Err() != nil {
					t.Fatalf("env.Compile(%q) failed: %v", tc.expr, iss.Err())
				}
				prg, err := env.Program(ast, lvl.opts...)
				if err != nil {
					t.Fatalf("env.Program() failed: %v", err)
				}
				act, err := cel.NewActivation(map[string]any{
					"msg":        tc.msg,
					"nested_msg": tc.nestedMsg,
				})
				if err != nil {
					t.Fatalf("cel.NewActivation() failed: %v", err)
				}
				out, _, err := prg.Eval(act)
				if err != nil {
					t.Fatalf("prg.Eval() failed: %v", err)
				}
				if out != types.Bool(tc.want) {
					t.Fatalf("prg.Eval() = %v, want %v", out, tc.want)
				}
			})
		}
	}
}

func BenchmarkRuntimeCachedEval(b *testing.B) {
	env := newRuntimeBenchmarkEnv(b)
	optsByLevel := []struct {
		name string
		opts []cel.ProgramOption
	}{
		{name: "M0"},
		{name: "M1", opts: []cel.ProgramOption{cel.EvalOptions(cel.OptOptimize)}},
	}

	for _, tc := range runtimeBenchmarkCases() {
		for _, lvl := range optsByLevel {
			b.Run(tc.name+"/"+lvl.name, func(b *testing.B) {
				ast, iss := env.Compile(tc.expr)
				if iss.Err() != nil {
					b.Fatalf("env.Compile(%q) failed: %v", tc.expr, iss.Err())
				}
				prg, err := env.Program(ast, lvl.opts...)
				if err != nil {
					b.Fatalf("env.Program() failed: %v", err)
				}
				act, err := cel.NewActivation(map[string]any{
					"msg":        tc.msg,
					"nested_msg": tc.nestedMsg,
				})
				if err != nil {
					b.Fatalf("cel.NewActivation() failed: %v", err)
				}
				if out, _, err := prg.Eval(act); err != nil || out != types.Bool(tc.want) {
					b.Fatalf("prg.Eval() = (%v, %v), want %v", out, err, tc.want)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					prg.Eval(act)
				}
			})
		}
	}
}

func BenchmarkRuntimePlanAndEval(b *testing.B) {
	env := newRuntimeBenchmarkEnv(b)
	for _, tc := range runtimeBenchmarkCases() {
		b.Run(tc.name+"/M0", func(b *testing.B) {
			ast, iss := env.Compile(tc.expr)
			if iss.Err() != nil {
				b.Fatalf("env.Compile(%q) failed: %v", tc.expr, iss.Err())
			}
			act, err := cel.NewActivation(map[string]any{
				"msg":        tc.msg,
				"nested_msg": tc.nestedMsg,
			})
			if err != nil {
				b.Fatalf("cel.NewActivation() failed: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				prg, err := env.Program(ast)
				if err != nil {
					b.Fatalf("env.Program() failed: %v", err)
				}
				prg.Eval(act)
			}
		})
	}
}
