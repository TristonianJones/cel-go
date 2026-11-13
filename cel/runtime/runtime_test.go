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

package runtime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"cel.dev/cel-go/cel/async"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/cost"
	"cel.dev/cel-go/common/functions"
	"cel.dev/cel-go/common/operators"
	"cel.dev/cel-go/common/overloads"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/interpreter"

	proto3pb "cel.dev/cel-go/test/proto3pb"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	descpb "google.golang.org/protobuf/types/descriptorpb"
)

func TestRuntimeEnvAndEval(t *testing.T) {
	env, err := NewEnv(
		Container("test.pkg"),
		Abbrevs("google.expr.proto3.test.TestAllTypes"),
		Types(&proto3pb.TestAllTypes{}),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	if env.CELTypeAdapter() == nil || env.CELTypeProvider() == nil {
		t.Fatal("CELTypeAdapter() or CELTypeProvider() returned nil")
	}
	if env.TypeAdapter() == nil || env.TypeProvider() == nil {
		t.Fatal("TypeAdapter() or TypeProvider() returned nil")
	}

	// Build AST directly via common/ast without parser/checker: x + 1 == 3
	fac := ast.NewExprFactory()
	expr := fac.NewCall(1, operators.Equals,
		fac.NewCall(2, operators.Add, fac.NewIdent(3, "x"), fac.NewLiteral(4, types.Int(1))),
		fac.NewLiteral(5, types.Int(3)),
	)
	a := ast.NewAST(expr, nil)

	prg, err := env.Program(a, Optimize())
	if err != nil {
		t.Fatalf("Program() failed: %v", err)
	}
	out, _, err := prg.Eval(map[string]any{"x": int64(2)})
	if err != nil || out != types.True {
		t.Fatalf("Eval() got (%v, %v), want (true, nil)", out, err)
	}
}

func TestRuntimeCustomEnvAndExtend(t *testing.T) {
	customEnv, err := NewCustomEnv(
		FunctionBindings(&functions.Overload{
			Operator: "double_it",
			Unary: func(v ref.Val) ref.Val {
				return v.(types.Int) * 2
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewCustomEnv() failed: %v", err)
	}
	fac := ast.NewExprFactory()
	a := ast.NewAST(fac.NewCall(1, "double_it", fac.NewIdent(2, "x")), nil)
	prg, err := customEnv.Program(a)
	if err != nil {
		t.Fatalf("Program() failed: %v", err)
	}
	out, _, err := prg.Eval(map[string]any{"x": int64(21)})
	if err != nil || out != types.Int(42) {
		t.Fatalf("Eval() got (%v, %v), want (42, nil)", out, err)
	}

	// Extend without mutating functions shares parent dispatcher; mutating isolates child.
	ext1, err := customEnv.Extend(Globals(map[string]any{"x": int64(5)}).toEnvOpt())
	if err != nil {
		t.Fatalf("Extend() failed: %v", err)
	}
	prg1, err := ext1.Program(a)
	if err != nil {
		t.Fatalf("ext1.Program() failed: %v", err)
	}
	if out, _, err := prg1.Eval(NoVars()); err != nil || out != types.Int(10) {
		t.Fatalf("ext1 Eval() got (%v, %v), want (10, nil)", out, err)
	}

	ext2, err := customEnv.Extend(FunctionBindings(&functions.Overload{
		Operator: "double_it",
		Unary: func(v ref.Val) ref.Val {
			return v.(types.Int) * 3
		},
	}))
	if err != nil {
		t.Fatalf("Extend(FunctionBindings) failed: %v", err)
	}
	prg2, err := ext2.Program(a)
	if err != nil {
		t.Fatalf("ext2.Program() failed: %v", err)
	}
	if out, _, err := prg2.Eval(map[string]any{"x": int64(5)}); err != nil || out != types.Int(15) {
		t.Fatalf("ext2 Eval() got (%v, %v), want (15, nil)", out, err)
	}
	// Parent remains unaffected.
	if out, _, err := prg.Eval(map[string]any{"x": int64(5)}); err != nil || out != types.Int(10) {
		t.Fatalf("parent Eval() got (%v, %v), want (10, nil)", out, err)
	}
}

func (po ProgramOption) toEnvOpt() EnvOption {
	return ProgramOptions(po)
}

func TestRuntimeExecutionModes(t *testing.T) {
	env, err := NewEnv(
		CostModelVersion(1),
		CostSizingStrategy(nil),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}

	// [1, 2, 3].all(x, x < 2)
	fac := ast.NewExprFactory()
	comp := fac.NewComprehension(1,
		fac.NewList(2, []ast.Expr{
			fac.NewLiteral(3, types.Int(1)),
			fac.NewLiteral(4, types.Int(2)),
			fac.NewLiteral(5, types.Int(3)),
		}, nil),
		"x", "@result",
		fac.NewLiteral(6, types.True),
		fac.NewCall(7, operators.NotStrictlyFalse, fac.NewAccuIdent(8)),
		fac.NewCall(9, operators.LogicalAnd, fac.NewAccuIdent(10),
			fac.NewCall(11, operators.Less, fac.NewIdent(12, "x"), fac.NewLiteral(13, types.Int(2)))),
		fac.NewAccuIdent(14),
	)
	a := ast.NewAST(comp, nil)

	t.Run("track_state", func(t *testing.T) {
		prg, err := env.Program(a, EvalOptions(OptTrackState))
		if err != nil {
			t.Fatalf("Program() failed: %v", err)
		}
		out, det, err := prg.Eval(NoVars())
		if err != nil || out != types.False {
			t.Fatalf("Eval() got (%v, %v), want (false, nil)", out, err)
		}
		if v, ok := det.State().Value(1); !ok || v != types.False {
			t.Errorf("State().Value(1) got (%v, %t), want (false, true)", v, ok)
		}
		if _, traced := interpreter.TraceRecords(det.State()); traced {
			t.Error("TraceRecords() reported true without OptTraceIterations")
		}
	})

	t.Run("exhaustive_eval_and_tracing", func(t *testing.T) {
		prg, err := env.Program(a, EvalOptions(OptExhaustiveEval, OptTraceIterations))
		if err != nil {
			t.Fatalf("Program() failed: %v", err)
		}
		out, det, err := prg.Eval(NoVars())
		if err != nil || out != types.False {
			t.Fatalf("Eval() got (%v, %v), want (false, nil)", out, err)
		}
		records, traced := interpreter.TraceRecords(det.State())
		if !traced {
			t.Fatal("TraceRecords() reported false with OptTraceIterations")
		}
		var keys []int64
		for _, r := range records {
			if r.IsIteration() {
				keys = append(keys, int64(r.Key.(types.Int)))
			}
		}
		if want := []int64{0, 1, 2}; !reflect.DeepEqual(keys, want) {
			t.Errorf("traced iteration keys = %v, want %v", keys, want)
		}
	})

	t.Run("cost_tracking_and_limit", func(t *testing.T) {
		prg, err := env.Program(a, CostTracking(nil))
		if err != nil {
			t.Fatalf("Program() failed: %v", err)
		}
		_, det, err := prg.Eval(NoVars())
		if err != nil {
			t.Fatalf("Eval() failed: %v", err)
		}
		if det.ActualCost() == nil || *det.ActualCost() == 0 {
			t.Errorf("ActualCost() = %v, want > 0", det.ActualCost())
		}

		limited, err := env.Program(a, CostLimit(1))
		if err != nil {
			t.Fatalf("Program(CostLimit) failed: %v", err)
		}
		_, _, err = limited.Eval(NoVars())
		var cancelErr interpreter.EvalCancelledError
		if !errors.As(err, &cancelErr) || cancelErr.Cause != interpreter.CostLimitExceeded {
			t.Errorf("Eval() with CostLimit(1) got %v, want CostLimitExceeded", err)
		}
	})

	t.Run("memory_tracking_and_limit", func(t *testing.T) {
		prg, err := env.Program(a, MemoryTracking())
		if err != nil {
			t.Fatalf("Program() failed: %v", err)
		}
		_, det, err := prg.Eval(NoVars())
		if err != nil {
			t.Fatalf("Eval() failed: %v", err)
		}
		if det.PeakMemory() == nil {
			t.Error("PeakMemory() = nil, want non-nil")
		}

		// [1, 2, 3].map(x, [x]) triggers memory limit 1.
		mapExpr := fac.NewComprehension(1,
			fac.NewList(2, []ast.Expr{fac.NewLiteral(3, types.Int(1)), fac.NewLiteral(4, types.Int(2))}, nil),
			"x", "@result",
			fac.NewList(5, nil, nil),
			fac.NewLiteral(6, types.True),
			fac.NewCall(7, operators.Add, fac.NewAccuIdent(8), fac.NewList(9, []ast.Expr{fac.NewIdent(10, "x")}, nil)),
			fac.NewAccuIdent(11),
		)
		limited, err := env.Program(ast.NewAST(mapExpr, nil), MemoryLimit(1))
		if err != nil {
			t.Fatalf("Program(MemoryLimit) failed: %v", err)
		}
		_, _, err = limited.Eval(NoVars())
		var cancelErr interpreter.EvalCancelledError
		if !errors.As(err, &cancelErr) || cancelErr.Cause != interpreter.MemoryLimitExceeded {
			t.Errorf("Eval() with MemoryLimit(1) got %v, want MemoryLimitExceeded", err)
		}
	})

	t.Run("partial_eval", func(t *testing.T) {
		// x && y with x unknown, y false -> false
		expr := fac.NewCall(1, operators.LogicalAnd, fac.NewIdent(2, "x"), fac.NewIdent(3, "y"))
		prg, err := env.Program(ast.NewAST(expr, nil), EvalOptions(OptPartialEval))
		if err != nil {
			t.Fatalf("Program() failed: %v", err)
		}
		vars, err := PartialVars(map[string]any{"y": false}, AttributePattern("x"))
		if err != nil {
			t.Fatalf("PartialVars() failed: %v", err)
		}
		out, _, err := prg.Eval(vars)
		if err != nil || out != types.False {
			t.Fatalf("Eval() got (%v, %v), want (false, nil)", out, err)
		}
	})

	t.Run("context_eval_interrupt", func(t *testing.T) {
		prg, err := env.Program(a, InterruptCheckFrequency(1))
		if err != nil {
			t.Fatalf("Program() failed: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err = prg.ContextEval(ctx, NoVars())
		if !errors.Is(err, interpreter.InterruptError{}) {
			t.Errorf("ContextEval(cancelled) got %v, want InterruptError", err)
		}
	})

	t.Run("concurrent_eval_async", func(t *testing.T) {
		asyncEnv, err := NewEnv(FunctionBindings(&functions.Overload{
			Operator: "async_double",
			Async: func(_ context.Context, args ...ref.Val) <-chan ref.Val {
				ch := make(chan ref.Val, 1)
				ch <- args[0].(types.Int) * 2
				close(ch)
				return ch
			},
		}))
		if err != nil {
			t.Fatalf("NewEnv() failed: %v", err)
		}
		call := fac.NewCall(1, "async_double", fac.NewLiteral(2, types.Int(21)))
		prg, err := asyncEnv.Program(ast.NewAST(call, nil),
			AsyncCompletionBufferSize(4),
			AsyncMaxConcurrency(2),
			ConcurrentDrainStrategy(async.DrainReady(time.Millisecond)),
		)
		if err != nil {
			t.Fatalf("Program() failed: %v", err)
		}
		if _, _, err := prg.Eval(NoVars()); !errors.Is(err, errAsyncRequiresConcurrentEval) {
			t.Errorf("Eval() on async program got %v, want errAsyncRequiresConcurrentEval", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		res := <-prg.ConcurrentEval(ctx, NoVars())
		if res.Err != nil || res.Val != types.Int(42) {
			t.Errorf("ConcurrentEval() got (%v, %v), want (42, nil)", res.Val, res.Err)
		}
	})

	t.Run("regex_optimize_and_limit", func(t *testing.T) {
		reEnv, err := NewEnv(RegexProgramSizeLimit(100))
		if err != nil {
			t.Fatalf("NewEnv() failed: %v", err)
		}
		matchExpr := fac.NewCall(1, overloads.Matches, fac.NewIdent(2, "s"), fac.NewLiteral(3, types.String("^a+$")))
		prg, err := reEnv.Program(ast.NewAST(matchExpr, nil), EvalOptions(OptOptimize))
		if err != nil {
			t.Fatalf("Program() failed: %v", err)
		}
		if out, _, err := prg.Eval(map[string]any{"s": "aaa"}); err != nil || out != types.True {
			t.Errorf("Eval() got (%v, %v), want (true, nil)", out, err)
		}
	})

	t.Run("context_proto_vars_and_type_descs", func(t *testing.T) {
		msg := &proto3pb.TestAllTypes{SingleInt64: 99}
		fileDesc := msg.ProtoReflect().Descriptor().ParentFile()
		var files protoregistry.Files
		files.RegisterFile(fileDesc)
		e, err := NewEnv(
			TypeDescs(fileDesc, &files),
			JSONFieldNames(true),
		)
		if err != nil {
			t.Fatalf("NewEnv(TypeDescs) failed: %v", err)
		}
		vars, err := ContextProtoVars(msg, types.JSONFieldNames(true))
		if err != nil {
			t.Fatalf("ContextProtoVars() failed: %v", err)
		}
		prg, err := e.Program(ast.NewAST(fac.NewIdent(1, "singleInt64"), nil))
		if err != nil {
			t.Fatalf("Program() failed: %v", err)
		}
		if out, _, err := prg.Eval(vars); err != nil || out != types.Int(99) {
			t.Errorf("Eval() got (%v, %v), want (99, nil)", out, err)
		}
	})

	t.Run("native_type_and_legacy_provider", func(t *testing.T) {
		type myStruct struct {
			Name string
		}
		e, err := NewEnv(Types(NativeTypeFor[myStruct](NativeTypeAlias("my.Struct"))))
		if err != nil {
			t.Fatalf("NewEnv(NativeTypeFor) failed: %v", err)
		}
		if _, ok := e.TypeProvider().FindType("my.Struct"); !ok {
			t.Error("TypeProvider().FindType(my.Struct) returned false")
		}
		if _, err := NewCustomEnv(CustomTypeAdapter(nil)); err == nil {
			t.Error("CustomTypeAdapter(nil) succeeded, want error")
		}
		if _, err := NewCustomEnv(CustomTypeProvider(nil)); err == nil {
			t.Error("CustomTypeProvider(nil) succeeded, want error")
		}
	})
}

func TestNilProgram(t *testing.T) {
	var prg Program = (*prog)(nil)

	if _, _, err := prg.Eval(NoVars()); err == nil || !strings.Contains(err.Error(), "program is nil") {
		t.Errorf("prg.Eval() got error %v, wanted error containing 'program is nil'", err)
	}

	if _, _, err := prg.ContextEval(context.Background(), NoVars()); err == nil || !strings.Contains(err.Error(), "program is nil") {
		t.Errorf("prg.ContextEval() got error %v, wanted error containing 'program is nil'", err)
	}

	res := <-prg.ConcurrentEval(context.Background(), NoVars())
	if res.Err == nil || !strings.Contains(res.Err.Error(), "program is nil") {
		t.Errorf("prg.ConcurrentEval() got error %v, wanted error containing 'program is nil'", res.Err)
	}

	// Silence unused import if exprpb is only needed for type assertions in future tests.
	_ = (*exprpb.CheckedExpr)(nil)
	_ = reflect.TypeOf((*cost.Tracker)(nil))
}

type mockLegacyTypeProvider struct {
	ref.TypeProvider
}

func TestMaybeInteropProvider(t *testing.T) {
	if _, err := maybeInteropProvider(123); err == nil {
		t.Error("maybeInteropProvider(int) should return error")
	}
	p, err := maybeInteropProvider(&mockLegacyTypeProvider{})
	if err != nil {
		t.Fatalf("maybeInteropProvider(mockLegacyTypeProvider) failed: %v", err)
	}
	if _, ok := p.(*interopCELTypeProvider); !ok {
		t.Errorf("expected *interopCELTypeProvider, got %T", p)
	}
}

func TestRuntimeOptionsCoverage(t *testing.T) {
	env, err := NewEnv(
		CostModel(cost.Overload("test_op", cost.EvalCost(cost.Const(1)))),
		RegexProgramSizeLimit(100),
	)
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	if _, err := NewEnv(RegexProgramSizeLimit(-1)); err == nil {
		t.Error("RegexProgramSizeLimit(-1) succeeded, want error")
	}

	fac := ast.NewExprFactory()
	a := ast.NewAST(fac.NewLiteral(1, types.True), nil)

	// EvalOptions covering OptTrackCost, OptTrackMemory, OptTraceIterations.
	prg, err := env.Program(a,
		EvalOptions(OptTrackCost, OptTrackMemory, OptTraceIterations),
		OptimizeRegex(interpreter.MatchesRegexOptimization),
	)
	if err != nil {
		t.Fatalf("Program() failed: %v", err)
	}
	out, det, err := prg.Eval(NoVars())
	if err != nil || out != types.True {
		t.Fatalf("Eval() got (%v, %v), want (true, nil)", out, err)
	}
	if det.ActualCost() == nil || det.PeakMemory() == nil {
		t.Errorf("ActualCost=%v PeakMemory=%v, want non-nil", det.ActualCost(), det.PeakMemory())
	}
	if _, traced := interpreter.TraceRecords(det.State()); !traced {
		t.Error("TraceRecords() returned false")
	}

	// Functions with AsyncOp + ConcurrentDrainStrategy error.
	if _, err := env.Program(a, ConcurrentDrainStrategy(nil)); err == nil {
		t.Error("ConcurrentDrainStrategy(nil) succeeded, want error")
	}
	asyncPrg, err := env.Program(
		ast.NewAST(fac.NewCall(1, "async_fn"), nil),
		Functions(&functions.Overload{
			Operator: "async_fn",
			Async: func(_ context.Context, _ ...ref.Val) <-chan ref.Val {
				ch := make(chan ref.Val, 1)
				ch <- types.True
				close(ch)
				return ch
			},
		}),
	)
	if err != nil {
		t.Fatalf("Program(Functions async) failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if res := <-asyncPrg.ConcurrentEval(ctx, NoVars()); res.Err != nil || res.Val != types.True {
		t.Errorf("ConcurrentEval() got (%v, %v), want (true, nil)", res.Val, res.Err)
	}

	// Unsupported Types / TypeDescs inputs.
	if _, err := NewCustomEnv(Types(123)); err == nil {
		t.Error("Types(123) succeeded, want error")
	}
	if _, err := NewCustomEnv(TypeDescs(123)); err == nil {
		t.Error("TypeDescs(123) succeeded, want error")
	}

	// interopCELTypeProvider.FindStructFieldNames returns false.
	ip := &interopCELTypeProvider{TypeProvider: &mockLegacyTypeProvider{}}
	if _, ok := ip.FindStructFieldNames("foo"); ok {
		t.Error("FindStructFieldNames() returned true, want false")
	}
}

func TestRuntimeEdgeAndErrorPaths(t *testing.T) {
	// nil FunctionBindings, invalid Container/Abbrevs, and ContextProtoVars(nil).
	if _, err := NewCustomEnv(FunctionBindings(nil)); err != nil {
		t.Fatalf("FunctionBindings(nil) failed: %v", err)
	}
	if _, err := NewCustomEnv(Container(".bad")); err == nil {
		t.Error("Container('.bad') succeeded, want error")
	}
	if _, err := NewCustomEnv(Abbrevs("bad")); err == nil {
		t.Error("Abbrevs('bad') succeeded, want error")
	}
	if vars, err := ContextProtoVars(nil); err != nil || vars == nil {
		t.Errorf("ContextProtoVars(nil) got (%v, %v), want empty activation", vars, err)
	}

	// Types with ref.Type, reflect.Type, and TypeDescs with FileDescriptorProto / FileDescriptorSet.
	type nativeItem struct{ ID int64 }
	selfFD := &descpb.FileDescriptorProto{
		Name:    proto.String("self.proto"),
		Package: proto.String("self"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descpb.DescriptorProto{{
			Name: proto.String("Msg"),
			Field: []*descpb.FieldDescriptorProto{{
				Name:     proto.String("id"),
				Number:   proto.Int32(1),
				Label:    descpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				Type:     descpb.FieldDescriptorProto_TYPE_INT64.Enum(),
				JsonName: proto.String("id"),
			}},
		}},
	}
	env, err := NewEnv(
		Types(types.IntType, reflect.TypeOf(nativeItem{})),
		TypeDescs(selfFD, &descpb.FileDescriptorSet{File: []*descpb.FileDescriptorProto{selfFD}}),
	)
	if err != nil {
		t.Fatalf("NewEnv(Types/TypeDescs) failed: %v", err)
	}
	if _, err := NewCustomEnv(TypeDescs(&descpb.FileDescriptorSet{
		File: []*descpb.FileDescriptorProto{{Name: proto.String("bad.proto"), Dependency: []string{"missing.proto"}}},
	})); err == nil {
		t.Error("TypeDescs(missing import) succeeded, want error")
	}

	// Custom non-registry provider errors on Types, TypeDescs, JSONFieldNames, and Extend.
	customEnv, err := NewCustomEnv(CustomTypeProvider(&mockLegacyTypeProvider{}), CustomTypeAdapter(types.DefaultTypeAdapter))
	if err != nil {
		t.Fatalf("NewCustomEnv(customProv) failed: %v", err)
	}
	if _, err := customEnv.Extend(Types(types.IntType)); err == nil {
		t.Error("Types() on custom provider succeeded, want error")
	}
	if _, err := customEnv.Extend(TypeDescs(selfFD)); err == nil {
		t.Error("TypeDescs() on custom provider succeeded, want error")
	}
	if _, err := customEnv.Extend(JSONFieldNames(true)); err == nil {
		t.Error("JSONFieldNames(true) on custom provider succeeded, want error")
	}
	if _, err := customEnv.Extend(); err != nil {
		t.Fatalf("Extend() on custom provider/adapter failed: %v", err)
	}
	// Separate *types.Registry instances for adapter and provider in Extend.
	r1, _ := types.NewRegistry()
	r2, _ := types.NewRegistry()
	splitEnv, err := NewCustomEnv(CustomTypeAdapter(r1), CustomTypeProvider(r2))
	if err != nil {
		t.Fatalf("NewCustomEnv(split registries) failed: %v", err)
	}
	if _, err := splitEnv.Extend(); err != nil {
		t.Fatalf("Extend(split registries) failed: %v", err)
	}
	if _, err := NewCustomEnv(CustomTypeAdapter(types.DefaultTypeAdapter), CustomTypeProvider(r2)); err != nil {
		t.Fatalf("NewCustomEnv(adapter+registry) failed: %v", err)
	} else if _, err := splitEnv.Extend(); err != nil {
		t.Fatalf("Extend() failed: %v", err)
	}
	regOnlyAdp, _ := NewCustomEnv(CustomTypeAdapter(r1), CustomTypeProvider(&mockLegacyTypeProvider{}))
	if _, err := regOnlyAdp.Extend(); err != nil {
		t.Fatalf("Extend(registry adapter + custom provider) failed: %v", err)
	}

	// EvalDetails nil safety.
	var nilDet *EvalDetails
	if nilDet.State() == nil || nilDet.ActualCost() != nil || nilDet.PeakMemory() != nil {
		t.Error("nil EvalDetails methods returned unexpected values")
	}

	// ContextEval(nil) and ConcurrentEval(nil) and json_name extension check.
	fac := ast.NewExprFactory()
	lit := ast.NewAST(fac.NewLiteral(1, types.True), nil)
	prg, err := env.Program(lit)
	if err != nil {
		t.Fatalf("Program() failed: %v", err)
	}
	if _, _, err := prg.ContextEval(nil, NoVars()); err == nil {
		t.Error("ContextEval(nil) succeeded, want error")
	}
	if res := <-prg.ConcurrentEval(nil, NoVars()); res.Err == nil {
		t.Error("ConcurrentEval(nil) succeeded, want error")
	}

	info := ast.NewSourceInfo(nil)
	info.AddExtension(ast.NewExtension("json_name", ast.NewExtensionVersion(1, 1), ast.ComponentRuntime))
	if _, err := env.Program(ast.NewAST(fac.NewLiteral(1, types.True), info)); err == nil {
		t.Error("Program(json_name without JSONFieldNames) succeeded, want error")
	}

	// ConcurrentEval with panic recovery, unlimited concurrency (-1), and context cancellation.
	panicEnv, err := NewEnv(FunctionBindings(&functions.Overload{
		Operator: "boom_async",
		Async: func(_ context.Context, _ ...ref.Val) <-chan ref.Val {
			ch := make(chan ref.Val)
			return ch // blocks until ctx cancelled
		},
	}))
	if err != nil {
		t.Fatalf("NewEnv() failed: %v", err)
	}
	blockPrg, err := panicEnv.Program(
		ast.NewAST(fac.NewCall(1, "boom_async"), nil),
		AsyncMaxConcurrency(-1),
	)
	if err != nil {
		t.Fatalf("Program() failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if res := <-blockPrg.ConcurrentEval(ctx, NoVars()); !errors.Is(res.Err, context.DeadlineExceeded) {
		t.Errorf("ConcurrentEval(timeout) got %v, want DeadlineExceeded", res.Err)
	}

	// Eval and ConcurrentEval panic recovery.
	panicSyncEnv, _ := NewEnv(FunctionBindings(&functions.Overload{
		Operator: "panic_op",
		Unary: func(_ ref.Val) ref.Val {
			panic("boom")
		},
	}))
	panicPrg, _ := panicSyncEnv.Program(ast.NewAST(fac.NewCall(1, "panic_op", fac.NewLiteral(2, types.True)), nil))
	if _, _, err := panicPrg.Eval(NoVars()); err == nil || !strings.Contains(err.Error(), "internal error: boom") {
		t.Errorf("Eval(panic) got %v, want internal error: boom", err)
	}
	if res := <-panicPrg.ConcurrentEval(context.Background(), NoVars()); res.Err == nil || !strings.Contains(res.Err.Error(), "internal error: boom") {
		t.Errorf("ConcurrentEval(panic) got %v, want internal error: boom", res.Err)
	}
}

type protoOnlyRegistry struct{ types.Provider }

func (r *protoOnlyRegistry) RegisterDescriptor(protoreflect.FileDescriptor) error { return nil }
func (r *protoOnlyRegistry) RegisterType(...ref.Type) error                       { return nil }

type errProtoRegistry struct {
	types.Provider
	err error
}

func (r *errProtoRegistry) RegisterDescriptor(protoreflect.FileDescriptor) error { return r.err }
func (r *errProtoRegistry) RegisterType(...ref.Type) error                       { return r.err }
func (r *errProtoRegistry) RegisterNativeType(reflect.Type, ...types.NativeTypeOption) error {
	return r.err
}
func (r *errProtoRegistry) RegisterNativeTypeDesc(*types.NativeTypeDesc) error { return r.err }

type badInteropProvider struct {
	ref.TypeProvider
}

func (b *badInteropProvider) FindType(string) (*exprpb.Type, bool) { return &exprpb.Type{}, true }
func (b *badInteropProvider) FindFieldType(string, string) (*ref.FieldType, bool) {
	return &ref.FieldType{Type: &exprpb.Type{}}, true
}

type badCELProvider struct {
	types.Provider
}

func (b *badCELProvider) FindStructType(string) (*types.Type, bool) {
	return &types.Type{}, true
}
func (b *badCELProvider) FindStructFieldType(string, string) (*types.FieldType, bool) {
	return &types.FieldType{Type: &types.Type{}}, true
}

func TestRuntimeMoreCoverage(t *testing.T) {
	// CostSizingStrategy non-nil.
	if _, err := NewEnv(CostSizingStrategy(cost.DefaultSizingStrategy())); err != nil {
		t.Fatalf("NewEnv(CostSizingStrategy) failed: %v", err)
	}

	// Interop conversion error paths.
	ip := &interopCELTypeProvider{TypeProvider: &badInteropProvider{}}
	if _, ok := ip.FindStructType("bad"); ok {
		t.Error("FindStructType(bad) returned true")
	}
	if _, ok := ip.FindStructFieldType("bad", "f"); ok {
		t.Error("FindStructFieldType(bad) returned true")
	}
	lp := &interopLegacyTypeProvider{Provider: &badCELProvider{}}
	if _, ok := lp.FindType("bad"); ok {
		t.Error("FindType(bad) returned true")
	}
	if _, ok := lp.FindFieldType("bad", "f"); ok {
		t.Error("FindFieldType(bad) returned true")
	}

	// errProtoRegistry errors in Types and TypeDescs.
	errReg := &errProtoRegistry{err: errors.New("reg fail")}
	msg := &proto3pb.TestAllTypes{}
	fd := msg.ProtoReflect().Descriptor().ParentFile()
	var files protoregistry.Files
	_ = files.RegisterFile(fd)
	for _, opt := range []EnvOption{
		Types(msg),
		Types(types.IntType),
		Types(reflect.TypeOf(struct{}{})),
		Types(NativeTypeFor[struct{}]()),
		TypeDescs(fd),
		TypeDescs(&files),
	} {
		if _, err := NewCustomEnv(CustomTypeProvider(errReg), opt); err == nil {
			t.Errorf("expected error from %T", opt)
		}
	}
	// Native types unsupported by proto-only registry.
	protoOnly := &protoOnlyRegistry{}
	for _, opt := range []EnvOption{
		Types(reflect.TypeOf(struct{}{})),
		Types(NativeTypeFor[struct{}]()),
	} {
		if _, err := NewCustomEnv(CustomTypeProvider(protoOnly), opt); err == nil {
			t.Errorf("expected native-type unsupported error for %T", opt)
		}
	}
	// JSONFieldNames(true) configured before CustomTypeProvider hits configure() check.
	if _, err := NewCustomEnv(JSONFieldNames(true), CustomTypeProvider(&mockLegacyTypeProvider{})); err == nil {
		t.Error("JSONFieldNames(true) + CustomTypeProvider succeeded, want error")
	}
	// Globals error, FunctionBindings dispatcher error, CostTrackerOptions error.
	env, _ := NewEnv()
	fac := ast.NewExprFactory()
	a := ast.NewAST(fac.NewLiteral(1, types.True), nil)
	if _, err := env.Program(a, Globals(123)); err == nil {
		t.Error("Globals(123) succeeded, want error")
	}
	if _, err := env.Program(a, CostTracking(nil), CostTrackerOptions(cost.Model(cost.Overload("bad_id")))); err == nil {
		t.Error("bad CostTrackerOptions succeeded, want error")
	}
	if _, err := env.Program(a, Functions(&functions.Overload{Operator: "dup", Unary: func(v ref.Val) ref.Val { return v }}, &functions.Overload{Operator: "dup", Unary: func(v ref.Val) ref.Val { return v }})); err == nil {
		t.Error("duplicate Functions succeeded, want error")
	}
	badEnv, _ := NewCustomEnv(FunctionBindings(
		&functions.Overload{Operator: "bad_op"},
	))
	// ConcurrentEval panic recovery for CostLimitExceededError and EvalCancelledError.
	ceEnv, _ := NewEnv(FunctionBindings(
		&functions.Overload{Operator: "cost_panic", Unary: func(ref.Val) ref.Val {
			panic(cost.LimitExceededError{Message: "cost exceeded"})
		}},
		&functions.Overload{Operator: "cancel_panic", Unary: func(ref.Val) ref.Val {
			panic(interpreter.EvalCancelledError{Cause: interpreter.MemoryLimitExceeded, Message: "mem exceeded"})
		}},
	))
	_ = badEnv
	for _, op := range []string{"cost_panic", "cancel_panic"} {
		p, err := ceEnv.Program(ast.NewAST(fac.NewCall(1, op, fac.NewLiteral(2, types.True)), nil))
		if err != nil {
			t.Fatalf("Program(%s) failed: %v", op, err)
		}
		if res := <-p.ConcurrentEval(context.Background(), NoVars()); res.Err == nil {
			t.Errorf("ConcurrentEval(%s) succeeded, want error", op)
		}
	}
}
