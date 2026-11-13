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
	"errors"
	"fmt"
	"reflect"

	"cel.dev/cel-go/cel/async"
	"cel.dev/cel-go/common/containers"
	"cel.dev/cel-go/common/cost"
	"cel.dev/cel-go/common/functions"
	"cel.dev/cel-go/common/stdlib/bindings"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/pb"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/interpreter"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	descpb "google.golang.org/protobuf/types/descriptorpb"
)

// EnvOption is a functional interface for configuring the runtime environment.
type EnvOption func(e *Env) (*Env, error)

// StdLib adds the standard library runtime function overloads to the environment.
func StdLib() EnvOption {
	return FunctionBindings(bindings.Overloads()...)
}

// FunctionBindings adds one or more runtime function overloads to the environment.
//
// If an overload with the same Operator already exists on the environment, it is replaced.
func FunctionBindings(funcs ...*functions.Overload) EnvOption {
	return func(e *Env) (*Env, error) {
		e.ensureMutableBindings()
		for _, fn := range funcs {
			if fn != nil {
				e.bindings[fn.Operator] = fn
			}
		}
		return e, nil
	}
}

// Container sets the container name for the runtime environment.
func Container(name string) EnvOption {
	return func(e *Env) (*Env, error) {
		cont, err := e.Container.Extend(containers.Name(name))
		if err != nil {
			return nil, err
		}
		e.Container = cont
		return e, nil
	}
}

// Abbrevs configures a set of simple names as abbreviations for fully-qualified names.
func Abbrevs(qualifiedNames ...string) EnvOption {
	return func(e *Env) (*Env, error) {
		cont, err := e.Container.Extend(containers.Abbrevs(qualifiedNames...))
		if err != nil {
			return nil, err
		}
		e.Container = cont
		return e, nil
	}
}

// CustomTypeAdapter swaps the default types.Adapter implementation with a custom one.
//
// Note: This option must be specified before the Types and TypeDescs options when used together.
func CustomTypeAdapter(adapter types.Adapter) EnvOption {
	return func(e *Env) (*Env, error) {
		if adapter == nil {
			return nil, errors.New("custom type adapter must not be nil")
		}
		e.adapter = adapter
		return e, nil
	}
}

// CustomTypeProvider replaces the types.Provider implementation with a custom one.
//
// The `provider` variable type may either be types.Provider or ref.TypeProvider (deprecated).
//
// Note: This option must be specified before the Types and TypeDescs options when used together.
func CustomTypeProvider(provider any) EnvOption {
	return func(e *Env) (*Env, error) {
		if provider == nil {
			return nil, errors.New("custom type provider must not be nil")
		}
		var err error
		e.provider, err = maybeInteropProvider(provider)
		return e, err
	}
}

// NativeTypeDesc describes a native Go struct type for registration with CEL.
type NativeTypeDesc = types.NativeTypeDesc

// NativeTypeOption is a functional option for configuring handling of native types.
type NativeTypeOption = types.NativeTypeOption

var (
	// NativeTypeAlias configures a custom CEL type name (alias) for a native type descriptor.
	NativeTypeAlias = types.NativeTypeAlias
)

// NativeTypeFor constructs a NativeTypeDesc for a Go struct type T with the given options.
func NativeTypeFor[T any](opts ...NativeTypeOption) *NativeTypeDesc {
	return types.NativeTypeFor[T](opts...)
}

type protoTypeRegistry interface {
	RegisterDescriptor(protoreflect.FileDescriptor) error
	RegisterType(...ref.Type) error
}

type nativeTypeRegistry interface {
	RegisterNativeType(reflect.Type, ...types.NativeTypeOption) error
	RegisterNativeTypeDesc(*types.NativeTypeDesc) error
}

// Types adds one or more type registrations to the environment.
//
// The input types may be instances of `proto.Message`, `ref.Type`, `reflect.Type`, or
// `*NativeTypeDesc`.
func Types(addTypes ...any) EnvOption {
	return func(e *Env) (*Env, error) {
		reg, isReg := e.provider.(protoTypeRegistry)
		if !isReg {
			return nil, fmt.Errorf("custom types not supported by provider: %T", e.provider)
		}
		for _, t := range addTypes {
			switch v := t.(type) {
			case proto.Message:
				fdMap := pb.CollectFileDescriptorSet(v)
				for _, fd := range fdMap {
					if err := reg.RegisterDescriptor(fd); err != nil {
						return nil, err
					}
				}
			case ref.Type:
				if err := reg.RegisterType(v); err != nil {
					return nil, err
				}
			case reflect.Type:
				nreg, isNReg := e.provider.(nativeTypeRegistry)
				if !isNReg {
					return nil, fmt.Errorf("native types not supported by provider: %T", e.provider)
				}
				if err := nreg.RegisterNativeType(v); err != nil {
					return nil, err
				}
			case *types.NativeTypeDesc:
				nreg, isNReg := e.provider.(nativeTypeRegistry)
				if !isNReg {
					return nil, fmt.Errorf("native types not supported by provider: %T", e.provider)
				}
				if err := nreg.RegisterNativeTypeDesc(v); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("unsupported type: %T", t)
			}
		}
		return e, nil
	}
}

// TypeDescs adds type registrations from any protoreflect.FileDescriptor, protoregistry.Files,
// google.protobuf.FileDescriptorProto, or google.protobuf.FileDescriptorSet provided.
func TypeDescs(descs ...any) EnvOption {
	return func(e *Env) (*Env, error) {
		reg, isReg := e.provider.(protoTypeRegistry)
		if !isReg {
			return nil, fmt.Errorf("custom types not supported by provider: %T", e.provider)
		}
		var fds *descpb.FileDescriptorSet
		for _, d := range descs {
			if f, ok := d.(*descpb.FileDescriptorProto); ok {
				if fds == nil {
					fds = &descpb.FileDescriptorSet{File: []*descpb.FileDescriptorProto{}}
				}
				fds.File = append(fds.File, f)
			}
		}
		if fds != nil {
			if err := registerFileSet(reg, fds); err != nil {
				return nil, err
			}
		}
		for _, d := range descs {
			switch f := d.(type) {
			case *protoregistry.Files:
				if err := registerFiles(reg, f); err != nil {
					return nil, err
				}
			case protoreflect.FileDescriptor:
				if err := reg.RegisterDescriptor(f); err != nil {
					return nil, err
				}
			case *descpb.FileDescriptorSet:
				if err := registerFileSet(reg, f); err != nil {
					return nil, err
				}
			case *descpb.FileDescriptorProto:
				// handled above
			default:
				return nil, fmt.Errorf("unsupported type descriptor: %T", d)
			}
		}
		return e, nil
	}
}

func registerFileSet(reg protoTypeRegistry, fileSet *descpb.FileDescriptorSet) error {
	files, err := protodesc.NewFiles(fileSet)
	if err != nil {
		return fmt.Errorf("protodesc.NewFiles(%v) failed: %v", fileSet, err)
	}
	return registerFiles(reg, files)
}

func registerFiles(reg protoTypeRegistry, files *protoregistry.Files) error {
	var err error
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		err = reg.RegisterDescriptor(fd)
		return err == nil
	})
	return err
}

// JSONFieldNames enables or disables accessing protocol buffer fields by JSON field name.
func JSONFieldNames(enabled bool) EnvOption {
	return func(e *Env) (*Env, error) {
		e.jsonFieldNames = enabled
		if enabled {
			if err := e.applyJSONFieldNames(); err != nil {
				return nil, err
			}
		} else if reg, isReg := e.provider.(*types.Registry); isReg {
			if err := reg.WithJSONFieldNames(false); err != nil {
				return nil, err
			}
		}
		return e, nil
	}
}

// EnableErrorOnBadPresenceTest controls whether presence tests (`has()`) return an error on
// invalid field selections.
func EnableErrorOnBadPresenceTest(enabled bool) EnvOption {
	return func(e *Env) (*Env, error) {
		e.errorOnBadPresenceTest = enabled
		return e, nil
	}
}

// CostModel adds overload cost models used during runtime cost tracking.
func CostModel(models ...cost.OverloadModel) EnvOption {
	return func(e *Env) (*Env, error) {
		e.ensureMutableCostConfig()
		e.costConfig.models = append(e.costConfig.models, models...)
		return e, nil
	}
}

// CostSizingStrategy sets the sizing strategy used during runtime cost tracking.
func CostSizingStrategy(strategy cost.SizingStrategy) EnvOption {
	return func(e *Env) (*Env, error) {
		e.ensureMutableCostConfig()
		e.costConfig.strategy = strategy
		return e, nil
	}
}

// CostModelVersion sets the cost model revision used during runtime cost tracking.
func CostModelVersion(version uint32) EnvOption {
	return func(e *Env) (*Env, error) {
		e.ensureMutableCostConfig()
		v := version
		e.costConfig.version = &v
		return e, nil
	}
}

// RegexProgramSizeLimit sets the maximum RE2 program size allowed for compiled regexes.
func RegexProgramSizeLimit(limit int) EnvOption {
	return func(e *Env) (*Env, error) {
		if limit < 0 {
			return nil, fmt.Errorf("regex program size limit must be non-negative: %d", limit)
		}
		e.regexProgramSizeLimit = limit
		return e, nil
	}
}

// ProgramOptions appends default ProgramOption values to the environment.
func ProgramOptions(opts ...ProgramOption) EnvOption {
	return func(e *Env) (*Env, error) {
		e.progOpts = append(e.progOpts, opts...)
		return e, nil
	}
}

// ProgramOption is a functional interface for configuring evaluation bindings and behaviors.
type ProgramOption func(p *prog) (*prog, error)

// WithDispatcher configures the shared base dispatcher for a program, bypassing the environment's
// own function bindings. This is primarily used by the top-level cel package when it builds a
// dispatcher from checked declarations.
func WithDispatcher(disp interpreter.Dispatcher, hasAsync bool) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.baseDispatcher = disp
		p.hasAsync = p.hasAsync || hasAsync
		return p, nil
	}
}

// CustomDecorator appends an InterpretableDecorator to the program.
//
// Deprecated: Use CustomDecoratorV2 instead.
func CustomDecorator(dec interpreter.InterpretableDecorator) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.plannerOptions = append(p.plannerOptions, interpreter.CustomDecorator(dec))
		return p, nil
	}
}

// CustomDecoratorV2 appends an InterpretableDecoratorV2 to the program.
func CustomDecoratorV2(dec interpreter.InterpretableDecoratorV2) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.plannerOptions = append(p.plannerOptions, interpreter.CustomDecoratorV2(dec))
		return p, nil
	}
}

// Functions adds function overloads to the program.
func Functions(funcs ...*functions.Overload) ProgramOption {
	return func(p *prog) (*prog, error) {
		for _, fn := range funcs {
			if fn != nil && fn.Async != nil {
				p.hasAsync = true
				break
			}
		}
		p.progFuncs = append(p.progFuncs, funcs...)
		return p, nil
	}
}

// Globals configures default global variables for the program, which may be shadowed by input
// variables supplied to Eval.
func Globals(vars any) ProgramOption {
	return func(p *prog) (*prog, error) {
		defaultVars, err := NewActivation(vars)
		if err != nil {
			return nil, err
		}
		if p.defaultVars != nil {
			defaultVars = interpreter.NewHierarchicalActivation(p.defaultVars, defaultVars)
		}
		if _, err := interpreter.FindFunctionActivation(defaultVars); err != nil {
			return nil, err
		}
		p.defaultVars = defaultVars
		return p, nil
	}
}

// OptimizeRegex registers regex optimizations for constant pattern arguments at program creation.
func OptimizeRegex(regexOptimizations ...*interpreter.RegexOptimization) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.regexOptimizations = append(p.regexOptimizations, regexOptimizations...)
		return p, nil
	}
}

// EvalOption indicates an evaluation option that may affect the evaluation behavior or information
// in the output result.
type EvalOption int

const (
	// OptTrackState will cause the runtime to return an immutable EvalState value in the Result.
	OptTrackState EvalOption = 1 << iota

	// OptExhaustiveEval causes the runtime to disable short-circuits and track state.
	OptExhaustiveEval EvalOption = 1<<iota | OptTrackState

	// OptOptimize precomputes functions and operators with constants as arguments at program
	// creation time. It also pre-compiles regex pattern constants passed to 'matches', reports any
	// compilation errors at program creation and uses the compiled regex pattern for all 'matches'
	// function invocations.
	OptOptimize EvalOption = 1 << iota

	// OptPartialEval enables the evaluation of a partial state where the input data that may be
	// known to be missing, either as top-level variables, or somewhere within a variable's object
	// member graph.
	OptPartialEval EvalOption = 1 << iota

	// OptTrackCost enables the runtime cost calculation and returns cost within EvalDetails via
	// ActualCost().
	OptTrackCost EvalOption = 1 << iota

	// OptCheckStringFormat was a compile-time option; retained as a no-op for flag parity.
	//
	// Deprecated: this option is a no-op.
	OptCheckStringFormat EvalOption = 1 << iota

	// OptTrackMemory enables the runtime peak memory tracking and returns the peak watermark within
	// EvalDetails via PeakMemory().
	OptTrackMemory EvalOption = 1 << iota

	// OptTraceIterations tracks state and additionally records, in evaluation order, the start of
	// each comprehension iteration with its list index or map key, and each observed value with the
	// iteration in which it was observed.
	//
	// The records of the EvalState returned from EvalDetails.State() are available from
	// interpreter.TraceRecords. Combine with OptExhaustiveEval to observe every iteration rather
	// than only the iterations evaluated before the result was determined.
	//
	// Tracing is intended for diagnosing an evaluation result, e.g. re-evaluating an input which
	// failed a policy, and adds overhead proportional to the number of observed values.
	OptTraceIterations EvalOption = 1<<iota | OptTrackState
)

// EvalOptions sets one or more evaluation options which may affect the evaluation or Result.
// Optimize enables constant folding and regex pre-compilation at program creation time.
func Optimize() ProgramOption {
	return func(p *prog) (*prog, error) {
		p.evalOpts |= OptOptimize
		p.optimize = buildOptimizePlanner(p)
		return p, nil
	}
}

// EvalOptions sets one or more evaluation options which may affect the evaluation or Result.
func EvalOptions(opts ...EvalOption) ProgramOption {
	return func(p *prog) (*prog, error) {
		for _, opt := range opts {
			p.evalOpts |= opt
			if opt&OptOptimize == OptOptimize && p.optimize == nil {
				p.optimize = buildOptimizePlanner(p)
			}
			if opt&OptTrackCost == OptTrackCost && p.costObserver == nil {
				p.costObserver = buildCostObserver
			}
			if opt&OptTrackMemory == OptTrackMemory && p.memObserver == nil {
				p.memObserver = buildMemObserver
			}
		}
		return p, nil
	}
}

// InterruptCheckFrequency sets the frequency of interrupt checks during comprehension evaluation.
func InterruptCheckFrequency(checkFrequency uint) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.interruptCheckFrequency = checkFrequency
		return p, nil
	}
}

// AsyncCallObserver registers an observer to receive lifecycle events for asynchronous function
// calls during ConcurrentEval.
func AsyncCallObserver(observer async.Observer) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.asyncObserver = observer
		return p, nil
	}
}

// AsyncCompletionBufferSize sets the buffer size of the channel used to collect completed async
// calls during ConcurrentEval.
func AsyncCompletionBufferSize(size int) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.asyncCompletionBufferSize = size
		return p, nil
	}
}

// AsyncMaxConcurrency sets the maximum number of asynchronous function calls that may be
// in-flight at once during a single ConcurrentEval execution.
func AsyncMaxConcurrency(maxConcurrency int) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.asyncMaxConcurrency = maxConcurrency
		return p, nil
	}
}

// ConcurrentDrainStrategy sets the strategy used by ConcurrentEval to decide when to re-evaluate
// after async function completions arrive.
func ConcurrentDrainStrategy(strategy async.DrainStrategy) ProgramOption {
	return func(p *prog) (*prog, error) {
		if strategy == nil {
			return nil, errors.New("concurrent drain strategy must not be nil")
		}
		p.drainStrategy = strategy
		return p, nil
	}
}

// BypassAsyncProtection allows a Program containing asynchronous functions to be evaluated with
// Eval and ContextEval instead of requiring ConcurrentEval.
func BypassAsyncProtection() ProgramOption {
	return func(p *prog) (*prog, error) {
		p.hasAsync = false
		return p, nil
	}
}

// CostTrackerOptions sets options for the runtime cost tracker.
func CostTrackerOptions(costOpts ...cost.TrackerOption) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.costOptions = append(p.costOptions, costOpts...)
		return p, nil
	}
}

// CostTracking enables runtime cost tracking with an optional cost estimator for extension
// functions.
func CostTracking(costEstimator cost.ActualCostEstimator) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.evalOpts |= OptTrackCost
		p.callCostEstimator = costEstimator
		p.costObserver = buildCostObserver
		return p, nil
	}
}

// MemoryTracking enables runtime memory tracking with optional memory tracker options.
func MemoryTracking(memOpts ...types.MemoryTrackerOption) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.evalOpts |= OptTrackMemory
		p.memoryOptions = append(p.memoryOptions, memOpts...)
		p.memObserver = buildMemObserver
		return p, nil
	}
}

// MemoryLimit sets a peak memory limit in bytes for evaluation.
func MemoryLimit(memLimit uint32) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.evalOpts |= OptTrackMemory
		p.memoryLimit = &memLimit
		p.memObserver = buildMemObserver
		return p, nil
	}
}

// CostLimit sets an actual cost limit for evaluation.
func CostLimit(costLimit uint64) ProgramOption {
	return func(p *prog) (*prog, error) {
		p.evalOpts |= OptTrackCost
		p.costLimit = &costLimit
		p.costObserver = buildCostObserver
		return p, nil
	}
}

// ContextProtoVars uses the fields of the input proto.Message as top-level variables within an
// Activation.
func ContextProtoVars(ctx proto.Message, opts ...types.RegistryOption) (Activation, error) {
	if ctx == nil || !ctx.ProtoReflect().IsValid() {
		return interpreter.EmptyActivation(), nil
	}
	regOpts := []types.RegistryOption{types.ProtoTypeDefs(ctx)}
	regOpts = append(regOpts, opts...)
	ro := make([]any, 0, len(regOpts))
	for _, opt := range regOpts {
		ro = append(ro, opt)
	}
	reg, err := types.NewRegistry(ro...)
	if err != nil {
		return nil, err
	}
	pbRef := ctx.ProtoReflect()
	typeName := string(pbRef.Descriptor().FullName())
	fields := pbRef.Descriptor().Fields()
	vars := make(map[string]any, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		fieldName := field.TextName()
		if reg.JSONFieldNames() {
			fieldName = field.JSONName()
		}
		sft, found := reg.FindStructFieldType(typeName, fieldName)
		if !found {
			return nil, fmt.Errorf("no such field: %s", fieldName)
		}
		fieldVal, err := sft.GetFrom(ctx)
		if err != nil {
			return nil, err
		}
		vars[fieldName] = fieldVal
	}
	return NewActivation(vars)
}
