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

package fuzzing

import (
	"testing"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/interpreter"
	"google.golang.org/protobuf/proto"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

var (
	typeNameToTypeValue = map[string]*types.Type{
		"bool":      types.BoolType,
		"bytes":     types.BytesType,
		"double":    types.DoubleType,
		"null_type": types.NullType,
		"int":       types.IntType,
		"list":      types.ListType,
		"map":       types.MapType,
		"string":    types.StringType,
		"type":      types.TypeType,
		"uint":      types.UintType,
	}
	adapter = types.DefaultTypeAdapter
)

func valueToRefValue(v *exprpb.Value) (ref.Val, bool) {
	switch t := v.GetKind().(type) {
	case *exprpb.Value_NullValue:
		return types.NullValue, true
	case *exprpb.Value_BoolValue:
		return types.Bool(t.BoolValue), true
	case *exprpb.Value_Int64Value:
		return types.Int(t.Int64Value), true
	case *exprpb.Value_Uint64Value:
		return types.Uint(t.Uint64Value), true
	case *exprpb.Value_DoubleValue:
		return types.Double(t.DoubleValue), true
	case *exprpb.Value_StringValue:
		return types.String(t.StringValue), true
	case *exprpb.Value_BytesValue:
		return types.Bytes(t.BytesValue), true
	case *exprpb.Value_EnumValue:
		return types.Int(t.EnumValue.GetValue()), true
	case *exprpb.Value_ObjectValue:
		o, err := anypb.UnmarshalNew(t.ObjectValue, proto.UnmarshalOptions{
			AllowPartial:   true,
			DiscardUnknown: true,
		})
		if err == nil {
			return adapter.NativeToValue(o), true
		}
	case *exprpb.Value_MapValue:
		m := make(map[ref.Val]ref.Val)
		for _, e := range t.MapValue.GetEntries() {
			key, keyOk := valueToRefValue(e.GetKey())
			val, valOk := valueToRefValue(e.GetValue())
			if keyOk && valOk {
				switch key.(type) {
				case types.Bool, types.Int, types.Uint, types.String:
					m[key] = val
				}
			}
		}
		return adapter.NativeToValue(m), true
	case *exprpb.Value_ListValue:
		l := make([]ref.Val, 0, len(t.ListValue.GetValues()))
		for _, e := range t.ListValue.GetValues() {
			if elem, ok := valueToRefValue(e); ok {
				l = append(l, elem)
			}
		}
		return adapter.NativeToValue(l), true
	case *exprpb.Value_TypeValue:
		typeName := t.TypeValue
		tv, ok := typeNameToTypeValue[typeName]
		if ok {
			return tv, true
		}
		return types.NewObjectTypeValue(typeName), true
	}
	return nil, false
}

type activation map[string]*exprpb.Value

func (a activation) ResolveName(name string) (any, bool) {
	if v, ok := a[name]; ok {
		return valueToRefValue(v)
	}
	return nil, false
}

func (a activation) Parent() interpreter.Activation {
	return nil
}

var _ interpreter.Activation = activation{}

func FuzzInterpreter(f *testing.F) {
	f.Add([]byte{})
	if env, err := getCELFuzzEnv(); err == nil {
		if env, err = env.Extend(cel.Variable("x", cel.StringType)); err == nil {
			if ast, issues := env.Compile("x + ' world'"); issues.Err() == nil {
				if ce, err := cel.AstToCheckedExpr(ast); err == nil {
					seed := &InterpreterFuzzable{
						CheckedExpr: ce,
						Activation: map[string]*exprpb.Value{
							"x": {Kind: &exprpb.Value_StringValue{StringValue: "hello"}},
						},
					}
					if b, err := proto.Marshal(seed); err == nil {
						f.Add(b)
					}
				}
			}
		}
	}
	if b, err := proto.Marshal(&InterpreterFuzzable{
		CheckedExpr: &exprpb.CheckedExpr{
			Expr: &exprpb.Expr{
				Id: 1,
				ExprKind: &exprpb.Expr_CallExpr{
					CallExpr: &exprpb.Expr_Call{
						Function: "_!=_",
					},
				},
			},
		},
	}); err == nil {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		var p InterpreterFuzzable
		err := proto.UnmarshalOptions{
			AllowPartial:   true,
			DiscardUnknown: true,
		}.Unmarshal(in, &p)
		if err != nil {
			return
		}
		env, err := getCELFuzzEnv()
		if err != nil {
			panic(err)
		}
		prog, err := env.Program(cel.CheckedExprToAst(p.GetCheckedExpr()))
		if err != nil {
			return
		}
		_, _, _ = prog.Eval(activation(p.GetActivation()))
	})
}
