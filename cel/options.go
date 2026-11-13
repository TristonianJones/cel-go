// Copyright 2019 Google LLC
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

package cel

import (
	"errors"
	"fmt"
	"reflect"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"cel.dev/cel-go/cel/async"
	"cel.dev/cel-go/cel/runtime"
	"cel.dev/cel-go/common/cost"
	"cel.dev/cel-go/common/decls"
	"cel.dev/cel-go/common/env"
	"cel.dev/cel-go/common/functions"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/interpreter"
	"cel.dev/cel-go/parser"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// These constants beginning with "Feature" enable optional behavior in
// the library.  See the documentation for each constant to see its
// effects, compatibility restrictions, and standard conformance.
const (
	_ = iota

	// Enable the tracking of function call expressions replaced by macros.
	featureEnableMacroCallTracking

	// Enable eager validation of declarations to ensure that Env values created
	// with `Extend` inherit a validated list of declarations from the parent Env.
	featureEagerlyValidateDeclarations

	// Enable the use of the default UTC timezone when a timezone is not specified
	// on a CEL timestamp operation. This fixes the scenario where the input time
	// is not already in UTC.
	featureDefaultUTCTimeZone

	// Enable the serialization of logical operator ASTs as variadic calls, thus
	// compressing the logic graph to a single call when multiple like-operator
	// expressions occur: e.g. a && b && c && d -> call(_&&_, [a, b, c, d])
	featureVariadicLogicalASTs

	// Enable error generation when a presence test or optional field selection is
	// performed on a primitive type.
	featureEnableErrorOnBadPresenceTest

	// Enable escape syntax for field identifiers (`).
	featureIdentEscapeSyntax

	// Enable accessing fields by JSON names within protobuf messages
	featureJSONFieldNames

	// Enable strict RFC 3339 validation of timestamp strings and strict range validation of
	// numeric timezone offsets. Strict validation is the default; the feature only has an effect
	// when it is explicitly disabled, see StrictTimestampParsing.
	featureStrictTimestampParsing

	// Enable listing the names attempted during resolution in undeclared reference errors.
	featureResolutionCandidates
)

var featureIDsToNames = map[int]string{
	featureEnableMacroCallTracking: "cel.feature.macro_call_tracking",
	featureIdentEscapeSyntax:       "cel.feature.backtick_escape_syntax",
	featureJSONFieldNames:          "cel.feature.json_field_names",
}

func featureNameByID(id int) (string, bool) {
	name, found := featureIDsToNames[id]
	return name, found
}

func featureIDByName(name string) (int, bool) {
	for id, n := range featureIDsToNames {
		if n == name {
			return id, true
		}
	}
	return 0, false
}

// limitID is used as a key for configurable limits. These are options that
// support exporting to YAML environment config.
type limitID int

const (
	_ = limitID(iota)
	// The number of recursive calls permitted in parsing.
	limitParseRecursionDepth
	// The number of code points permitted in an input expression string.
	limitCodePointSize
	// The number of attempts to recover from a parse error.
	limitParseErrorRecovery
	// The maximum nesting depth permitted for ASTs loaded outside the parser.
	limitMaxASTDepth
	// The maximum number of expression nodes permitted in parsing (including macro expansion).
	limitExpressionNodeCount
	// The maximum regex program plan size permitted.
	limitRegexProgramSize
)

// defaultMaxASTDepth mirrors the parser's default maxRecursionDepth (250) and
// is applied to ASTs that enter through non-parser ingestion paths (e.g. via
// ParsedExprToAst / CheckedExprToAst) when no explicit limit is configured.
const defaultMaxASTDepth = 250

var limitIDsToNames = map[limitID]string{
	limitCodePointSize:       "cel.limit.expression_code_points",
	limitParseErrorRecovery:  "cel.limit.parse_error_recovery",
	limitParseRecursionDepth: "cel.limit.parse_recursion_depth",
	limitMaxASTDepth:         "cel.limit.max_ast_depth",
	limitExpressionNodeCount: "cel.limit.expression_node_count",
	limitRegexProgramSize:    "cel.limit.regex_program_size",
}

func limitNameByID(id limitID) (string, bool) {
	v, ok := limitIDsToNames[id]
	return v, ok
}

func limitIDByName(name string) (limitID, bool) {
	for k, v := range limitIDsToNames {
		if v == name {
			return k, true
		}
	}
	return limitID(0), false
}

// EnvOption is a functional interface for configuring the environment.
type EnvOption func(e *Env) (*Env, error)

// ClearMacros options clears all parser macros.
//
// Clearing macros will ensure CEL expressions can only contain linear evaluation paths, as
// comprehensions such as `all` and `exists` are enabled only via macros.
func ClearMacros() EnvOption {
	return func(e *Env) (*Env, error) {
		e.macros = NoMacros
		return e, nil
	}
}

// CustomTypeAdapter swaps the default types.Adapter implementation with a custom one.
//
// Note: This option must be specified before the Types and TypeDescs options when used together.
func CustomTypeAdapter(adapter types.Adapter) EnvOption {
	return func(e *Env) (*Env, error) {
		return e, e.applyRuntime(runtime.CustomTypeAdapter(adapter))
	}
}

// CustomTypeProvider replaces the types.Provider implementation with a custom one.
//
// The `provider` variable type may either be types.Provider or ref.TypeProvider (deprecated)
//
// Note: This option must be specified before the Types and TypeDescs options when used together.
func CustomTypeProvider(provider any) EnvOption {
	return func(e *Env) (*Env, error) {
		return e, e.applyRuntime(runtime.CustomTypeProvider(provider))
	}
}

// Declarations option extends the declaration set configured in the environment.
//
// Note: Declarations will by default be appended to the pre-existing declaration set configured
// for the environment. The NewEnv call builds on top of the standard CEL declarations. For a
// purely custom set of declarations use NewCustomEnv.
//
// Deprecated: use FunctionDecls and VariableDecls or FromConfig instead.
func Declarations(decls ...*exprpb.Decl) EnvOption {
	declOpts := []EnvOption{}
	var err error
	var opt EnvOption
	// Convert the declarations to `EnvOption` values ahead of time.
	// Surface any errors in conversion when the options are applied.
	for _, d := range decls {
		opt, err = ExprDeclToDeclaration(d)
		if err != nil {
			break
		}
		declOpts = append(declOpts, opt)
	}
	return func(e *Env) (*Env, error) {
		if err != nil {
			return nil, err
		}
		for _, o := range declOpts {
			e, err = o(e)
			if err != nil {
				return nil, err
			}
		}
		return e, nil
	}
}

// EagerlyValidateDeclarations ensures that any collisions between configured declarations are caught
// at the time of the `NewEnv` call.
//
// Eagerly validating declarations is also useful for bootstrapping a base `cel.Env` value.
// Calls to base `Env.Extend()` will be significantly faster when declarations are eagerly validated
// as declarations will be collision-checked at most once and only incrementally by way of `Extend`
//
// Disabled by default as not all environments are used for type-checking.
func EagerlyValidateDeclarations(enabled bool) EnvOption {
	return features(featureEagerlyValidateDeclarations, enabled)
}

// HomogeneousAggregateLiterals disables mixed type list and map literal values.
//
// Note, it is still possible to have heterogeneous aggregates when provided as variables to the
// expression, as well as via conversion of well-known dynamic types, or with unchecked
// expressions.
func HomogeneousAggregateLiterals() EnvOption {
	return ASTValidators(ValidateHomogeneousAggregateLiterals())
}

// variadicLogicalOperatorASTs flatten like-operator chained logical expressions into a single
// variadic call with N-terms. This behavior is useful when serializing to a protocol buffer as
// it will reduce the number of recursive calls needed to deserialize the AST later.
//
// For example, given the following expression the call graph will be rendered accordingly:
//
//	expression: a && b && c && (d || e)
//	ast: call(_&&_, [a, b, c, call(_||_, [d, e])])
func variadicLogicalOperatorASTs() EnvOption {
	return features(featureVariadicLogicalASTs, true)
}

// Macros option extends the macro set configured in the environment.
//
// Note: This option must be specified after ClearMacros if used together.
func Macros(macros ...Macro) EnvOption {
	return func(e *Env) (*Env, error) {
		e.macros = append(e.macros, macros...)
		return e, nil
	}
}

// Container sets the container for resolving variable names. Defaults to an empty container.
//
// If all references within an expression are relative to a protocol buffer package, then
// specifying a container of `google.type` would make it possible to write expressions such as
// `Expr{expression: 'a < b'}` instead of having to write `google.type.Expr{...}`.
func Container(name string) EnvOption {
	return func(e *Env) (*Env, error) {
		return e, e.applyRuntime(runtime.Container(name))
	}
}

// Abbrevs configures a set of simple names as abbreviations for fully-qualified names.
//
// An abbreviation (abbrev for short) is a simple name that expands to a fully-qualified name.
// Abbreviations can be useful when working with variables, functions, and especially types from
// multiple namespaces:
//
//	// CEL object construction
//	qual.pkg.version.ObjTypeName{
//	   field: alt.container.ver.FieldTypeName{value: ...}
//	}
//
// Only one the qualified names above may be used as the CEL container, so at least one of these
// references must be a long qualified name within an otherwise short CEL program. Using the
// following abbreviations, the program becomes much simpler:
//
//	// CEL Go option
//	Abbrevs("qual.pkg.version.ObjTypeName", "alt.container.ver.FieldTypeName")
//	// Simplified Object construction
//	ObjTypeName{field: FieldTypeName{value: ...}}
//
// There are a few rules for the qualified names and the simple abbreviations generated from them:
// - Qualified names must be dot-delimited, e.g. `package.subpkg.name`.
// - The last element in the qualified name is the abbreviation.
// - Abbreviations must not collide with each other.
// - The abbreviation must not collide with unqualified names in use.
//
// Abbreviations are distinct from container-based references in the following important ways:
// - Abbreviations must expand to a fully-qualified name.
// - Expanded abbreviations do not participate in namespace resolution.
// - Abbreviation expansion is done instead of the container search for a matching identifier.
// - Containers follow C++ namespace resolution rules with searches from the most qualified name
//
//	to the least qualified name.
//
// - Container references within the CEL program may be relative, and are resolved to fully
//
//	qualified names at either type-check time or program plan time, whichever comes first.
//
// If there is ever a case where an identifier could be in both the container and as an
// abbreviation, the abbreviation wins as this will ensure that the meaning of a program is
// preserved between compilations even as the container evolves.
func Abbrevs(qualifiedNames ...string) EnvOption {
	return func(e *Env) (*Env, error) {
		return e, e.applyRuntime(runtime.Abbrevs(qualifiedNames...))
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

// protoTypeRegistry is an internal-only interface containing the minimum methods required to support
// custom types. It is a subset of methods from ref.TypeRegistry.
// Types adds one or more type declarations to the environment, allowing for construction of
// type-literals whose definitions are included in the common expression built-in set.
func Types(addTypes ...any) EnvOption {
	return func(e *Env) (*Env, error) {
		return e, e.applyRuntime(runtime.Types(addTypes...))
	}
}

// TypeDescs adds type declarations from any protoreflect.FileDescriptor, protoregistry.Files,
// google.protobuf.FileDescriptorProto or google.protobuf.FileDescriptorSet provided.
func TypeDescs(descs ...any) EnvOption {
	return func(e *Env) (*Env, error) {
		return e, e.applyRuntime(runtime.TypeDescs(descs...))
	}
}

// JSONFieldNames supports accessing protocol buffer fields by json-name.
//
// Enabling JSON field name support will create a copy of the types.Registry with fields indexed
// by JSON name, and whether JSON name or Proto-style names are supported will be inferred from
// the AST extensions metadata.
func JSONFieldNames(enabled bool) EnvOption {
	return features(featureJSONFieldNames, enabled)
}

// ProgramOption is a functional interface for configuring evaluation bindings and behaviors.
type ProgramOption = runtime.ProgramOption

// CustomDecorator appends an InterpreterDecorator to the program.
//
// Deprecated: Use CustomDecoratorV2 instead.
func CustomDecorator(dec interpreter.InterpretableDecorator) ProgramOption {
	return runtime.CustomDecorator(dec)
}

// CustomDecoratorV2 appends an InterpretableDecoratorV2 to the program.
func CustomDecoratorV2(dec interpreter.InterpretableDecoratorV2) ProgramOption {
	return runtime.CustomDecoratorV2(dec)
}

// Functions adds function overloads to the program.
func Functions(funcs ...*functions.Overload) ProgramOption {
	return runtime.Functions(funcs...)
}

// Globals configures default global variables for the program, which may be shadowed by input
// variables supplied to Eval.
func Globals(vars any) ProgramOption {
	return runtime.Globals(vars)
}

// OptimizeRegex registers regex optimizations for constant pattern arguments at program creation.
func OptimizeRegex(regexOptimizations ...*interpreter.RegexOptimization) ProgramOption {
	return runtime.OptimizeRegex(regexOptimizations...)
}

// ConfigOptionFactory declares a signature which accepts a configuration element, e.g. env.Extension
// and optionally produces an EnvOption in response.
//
// If there are multiple ConfigOptionFactory values which could apply to the same configuration node
// the first one that returns an EnvOption and a `true` response will be used, and the config node
// will not be passed along to any other option factory.
//
// Only the *env.Extension type is provided at this time, but validators, optimizers, and other tuning
// parameters may be supported in the future.
type ConfigOptionFactory func(any) (EnvOption, bool)

// FromConfig produces and applies a set of EnvOption values derived from an env.Config object.
//
// For configuration elements which refer to features outside of the `cel` package, an optional set of
// ConfigOptionFactory values may be passed in to support the conversion from static configuration to
// configured cel.Env value.
//
// Note: disabling the standard library will clear the EnvOptions values previously set for the
// environment with the exception of propagating types and adapters over to the new environment.
//
// Note: to support custom types referenced in the configuration file, you must ensure that one of
// the following options appears before the FromConfig option: Types, TypeDescs, or CustomTypeProvider
// as the type provider configured at the time when the config is processed is the one used to derive
// type references from the configuration.
func FromConfig(config *env.Config, optFactories ...ConfigOptionFactory) EnvOption {
	return func(e *Env) (*Env, error) {
		if err := config.Validate(); err != nil {
			return nil, err
		}
		opts, err := configToEnvOptions(config, e.CELTypeProvider(), optFactories)
		if err != nil {
			return nil, err
		}
		for _, o := range opts {
			e, err = o(e)
			if err != nil {
				return nil, err
			}
		}
		return e, nil
	}
}

// configToEnvOptions generates a set of EnvOption values (or error) based on a config, a type provider,
// and an optional set of environment options.
func configToEnvOptions(config *env.Config, provider types.Provider, optFactories []ConfigOptionFactory) ([]EnvOption, error) {
	envOpts := []EnvOption{}
	// Configure the standard lib subset.
	if config.StdLib != nil {
		envOpts = append(envOpts, func(e *Env) (*Env, error) {
			if e.HasLibrary("cel.lib.std") {
				return nil, errors.New("invalid subset of stdlib: create a custom env")
			}
			return e, nil
		})
		if !config.StdLib.Disabled {
			envOpts = append(envOpts, StdLib(StdLibSubset(config.StdLib)))
		}
	} else {
		envOpts = append(envOpts, StdLib())
	}

	// Configure the container
	if config.Container != "" {
		envOpts = append(envOpts, Container(config.Container))
	}

	// Configure abbreviations
	for _, imp := range config.Imports {
		envOpts = append(envOpts, Abbrevs(imp.Name))
	}

	// Configure features and common limits.
	for _, feat := range config.Features {
		// Note, if a feature is not found, it is skipped as it is possible the feature
		// is not intended to be supported publicly. In the future, a refinement of
		// to this strategy to report unrecognized features and validators should probably
		// be covered as a standard ConfigOptionFactory
		if id, found := featureIDByName(feat.Name); found {
			envOpts = append(envOpts, features(id, feat.Enabled))
		}
	}

	// Configure the context variable declaration
	if config.ContextVariable != nil {
		typeName := config.ContextVariable.TypeName
		if _, found := provider.FindStructType(typeName); !found {
			return nil, fmt.Errorf("invalid context proto type: %q", typeName)
		}
		// Attempt to instantiate the proto in order to reflect to its descriptor
		msg := provider.NewValue(typeName, map[string]ref.Val{})
		pbMsg, ok := msg.Value().(proto.Message)
		if !ok {
			return nil, fmt.Errorf("unsupported context type: %T", msg.Value())
		}
		envOpts = append(envOpts, DeclareContextProto(pbMsg.ProtoReflect().Descriptor()))
	}

	// Configure variables
	if len(config.Variables) != 0 {
		vars := make([]*decls.VariableDecl, 0, len(config.Variables))
		for _, v := range config.Variables {
			vDef, err := v.AsCELVariable(provider)
			if err != nil {
				return nil, err
			}
			vars = append(vars, vDef)
		}
		envOpts = append(envOpts, VariableDecls(vars...))
	}

	// Configure functions
	if len(config.Functions) != 0 {
		funcs := make([]*decls.FunctionDecl, 0, len(config.Functions))
		for _, f := range config.Functions {
			fnDef, err := f.AsCELFunction(provider)
			if err != nil {
				return nil, err
			}
			funcs = append(funcs, fnDef)
		}
		envOpts = append(envOpts, FunctionDecls(funcs...))
	}

	for _, limit := range config.Limits {
		if id, found := limitIDByName(limit.Name); found {
			envOpts = append(envOpts, setLimit(id, limit.Value))
		}
	}

	// Configure validators
	for _, val := range config.Validators {
		if fac, found := astValidatorFactories()[val.Name]; found {
			envOpts = append(envOpts, func(e *Env) (*Env, error) {
				validator, err := fac(val)
				if err != nil {
					return nil, fmt.Errorf("%w", err)
				}
				return ASTValidators(validator)(e)
			})
		} else if opt, handled := handleExtendedConfigOption(val, optFactories); handled {
			envOpts = append(envOpts, opt)
		}
		// we don't error when the validator isn't found as it may be part
		// of an extension library and enabled implicitly.
	}

	// Configure extensions
	for _, ext := range config.Extensions {
		// version number has been validated by the call to `Validate`
		ver, _ := ext.VersionNumber()
		if ext.Name == "optional" {
			envOpts = append(envOpts, OptionalTypes(OptionalTypesVersion(ver)))
		} else {
			opt, handled := handleExtendedConfigOption(ext, optFactories)
			if !handled {
				return nil, fmt.Errorf("unrecognized extension: %s", ext.Name)
			}
			envOpts = append(envOpts, opt)
		}
	}

	return envOpts, nil
}

func handleExtendedConfigOption(conf any, optFactories []ConfigOptionFactory) (EnvOption, bool) {
	for _, optFac := range optFactories {
		if opt, useOption := optFac(conf); useOption {
			return opt, true
		}
	}
	return nil, false
}

// EvalOption indicates an evaluation option that may affect the evaluation behavior or information
// in the output result.
type EvalOption = runtime.EvalOption

const (
	OptTrackState        EvalOption = runtime.OptTrackState
	OptExhaustiveEval    EvalOption = runtime.OptExhaustiveEval
	OptOptimize          EvalOption = runtime.OptOptimize
	OptPartialEval       EvalOption = runtime.OptPartialEval
	OptTrackCost         EvalOption = runtime.OptTrackCost
	OptCheckStringFormat EvalOption = runtime.OptCheckStringFormat
	OptTrackMemory       EvalOption = runtime.OptTrackMemory
	OptTraceIterations   EvalOption = runtime.OptTraceIterations
)

func EvalOptions(opts ...EvalOption) ProgramOption { return runtime.EvalOptions(opts...) }
func InterruptCheckFrequency(checkFrequency uint) ProgramOption {
	return runtime.InterruptCheckFrequency(checkFrequency)
}
func AsyncCallObserver(observer async.Observer) ProgramOption {
	return runtime.AsyncCallObserver(observer)
}
func AsyncCompletionBufferSize(size int) ProgramOption {
	return runtime.AsyncCompletionBufferSize(size)
}
func AsyncMaxConcurrency(maxConcurrency int) ProgramOption {
	return runtime.AsyncMaxConcurrency(maxConcurrency)
}
func ConcurrentDrainStrategy(strategy async.DrainStrategy) ProgramOption {
	return runtime.ConcurrentDrainStrategy(strategy)
}
func BypassAsyncProtection() ProgramOption { return runtime.BypassAsyncProtection() }

// CostEstimatorOptions configure type-check time options for estimating expression cost.
func CostEstimatorOptions(costOpts ...cost.CostOption) EnvOption {
	return func(e *Env) (*Env, error) {
		e.costOptions = append(e.costOptions, costOpts...)
		return e, nil
	}
}

// CostModel configures overload cost estimators and cost trackers from a set of cost.OverloadModel definitions.
//
// Models are validated here because neither the estimator nor the tracker compiled from a model
// can report a problem once evaluation has begun. See cost.OverloadModel.Validate.
func CostModel(models ...cost.OverloadModel) EnvOption {
	return func(e *Env) (*Env, error) {
		if len(models) == 0 {
			return e, nil
		}
		if err := cost.ValidateOverloadModels(models...); err != nil {
			return nil, err
		}
		e.ensureMutableCostConfig()
		e.costConfig.models = append(e.costConfig.models, models...)
		return e, nil
	}
}

// CostSizingStrategy configures a SizingStrategy for both cost estimation and runtime cost tracking.
func CostSizingStrategy(strategy cost.SizingStrategy) EnvOption {
	return func(e *Env) (*Env, error) {
		if sameSizingStrategy(e.costConfig.strategy, strategy) {
			return e, nil
		}
		e.ensureMutableCostConfig()
		e.costConfig.strategy = strategy
		return e, nil
	}
}

func sameSizingStrategy(a, b cost.SizingStrategy) bool {
	if a == nil || b == nil {
		return a == b
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	return ta == tb && ta.Comparable() && a == b
}

// CostModelVersion pins cost estimation and runtime cost tracking to a revision of the cost
// model's rules.
//
// The pin applies to the standard models and to any model supplied via CostModel, for both
// estimation and tracking, so an environment's estimates and charges come from one consistent set
// of rules.
//
// If this option is not set, the latest cost model version is used. Use this only as a temporary
// migration aid to hold estimates stable against a baseline recorded under an earlier release.
// Because version 0 preserves legacy behaviors with known soundness gaps where static estimates can
// underestimate runtime cost, callers are strongly encouraged to switch to CostModelVersion(1) or
// higher (or omit this option to use the latest revision) for improved soundness; see
// cost.ModelVersion for the per-revision details.
func CostModelVersion(version uint32) EnvOption {
	return func(e *Env) (*Env, error) {
		if e.costConfig.version != nil && *e.costConfig.version == version {
			return e, nil
		}
		e.ensureMutableCostConfig()
		e.costConfig.version = &version
		return e, nil
	}
}

func CostTrackerOptions(costOpts ...cost.TrackerOption) ProgramOption {
	return runtime.CostTrackerOptions(costOpts...)
}
func CostTracking(costEstimator cost.ActualCostEstimator) ProgramOption {
	return runtime.CostTracking(costEstimator)
}
func MemoryTracking(memOpts ...types.MemoryTrackerOption) ProgramOption {
	return runtime.MemoryTracking(memOpts...)
}
func MemoryLimit(memLimit uint32) ProgramOption { return runtime.MemoryLimit(memLimit) }
func CostLimit(costLimit uint64) ProgramOption  { return runtime.CostLimit(costLimit) }

func fieldToCELType(field protoreflect.FieldDescriptor) (*Type, error) {
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		msgName := (string)(field.Message().FullName())
		return ObjectType(msgName), nil
	}
	if primitiveType, found := types.ProtoCELPrimitives[field.Kind()]; found {
		return primitiveType, nil
	}
	if field.Kind() == protoreflect.EnumKind {
		return IntType, nil
	}
	return nil, fmt.Errorf("field %s type %s not implemented", field.FullName(), field.Kind().String())
}

func fieldToVariable(field protoreflect.FieldDescriptor, jsonFieldNames bool) (*decls.VariableDecl, error) {
	name := string(field.Name())
	if jsonFieldNames {
		name = field.JSONName()
	}
	if field.IsMap() {
		mapKey := field.MapKey()
		mapValue := field.MapValue()
		keyType, err := fieldToCELType(mapKey)
		if err != nil {
			return nil, err
		}
		valueType, err := fieldToCELType(mapValue)
		if err != nil {
			return nil, err
		}
		return decls.NewVariable(name, MapType(keyType, valueType)), nil
	}
	if field.IsList() {
		elemType, err := fieldToCELType(field)
		if err != nil {
			return nil, err
		}
		return decls.NewVariable(name, ListType(elemType)), nil
	}
	celType, err := fieldToCELType(field)
	if err != nil {
		return nil, err
	}
	return decls.NewVariable(name, celType), nil
}

// DeclareContextProto returns an option to extend CEL environment with declarations from the given context proto.
// Each field of the proto defines a variable of the same name in the environment.
// https://github.com/google/cel-spec/blob/master/doc/langdef.md#evaluation-environment
//
// If using JSONFieldNames(), ensure that the option is set before DeclareContextProto is provided.
func DeclareContextProto(descriptor protoreflect.MessageDescriptor) EnvOption {
	return func(e *Env) (*Env, error) {
		if e.contextProto != nil {
			return nil, fmt.Errorf("context proto already declared as %q, got %q",
				e.contextProto.FullName(), descriptor.FullName())
		}
		e.contextProto = descriptor
		fields := descriptor.Fields()
		vars := make([]*decls.VariableDecl, 0, fields.Len())
		jsonFieldNames := e.HasFeature(featureJSONFieldNames)
		for i := 0; i < fields.Len(); i++ {
			field := fields.Get(i)
			variable, err := fieldToVariable(field, jsonFieldNames)
			if err != nil {
				return nil, err
			}
			vars = append(vars, variable)
		}
		var err error
		e, err = VariableDecls(vars...)(e)
		if err != nil {
			return nil, err
		}
		return Types(dynamicpb.NewMessage(descriptor))(e)
	}
}

func ContextProtoVars(ctx proto.Message, opts ...types.RegistryOption) (Activation, error) {
	return runtime.ContextProtoVars(ctx, opts...)
}

// EnableMacroCallTracking ensures that call expressions which are replaced by macros
// are tracked in the `SourceInfo` of parsed and checked expressions.
//
// This is required for Env.ResidualAst to succeed when the residual Ast contains an
// unresolved comprehension macro (all, exists, exists_one, filter, map): without it,
// ResidualAst cannot re-serialize the macro's expanded internal representation back
// into CEL source syntax and returns an error instead of the residual expression.
func EnableMacroCallTracking() EnvOption {
	return features(featureEnableMacroCallTracking, true)
}

// EnableIdentifierEscapeSyntax enables identifier escaping (`) syntax for
// fields.
func EnableIdentifierEscapeSyntax() EnvOption {
	return features(featureIdentEscapeSyntax, true)
}

// CrossTypeNumericComparisons makes it possible to compare across numeric types, e.g. double < int
// By default, this option is enabled. CrossTypeNumericComparisons(false) is not
// supported, use library subsetting instead.
func CrossTypeNumericComparisons(enabled bool) EnvOption {
	return func(e *Env) (*Env, error) {
		if !enabled {
			return nil, errors.New("CrossTypeNumericComparisons(false) is not supported, use library subsetting instead")
		}
		return e, nil
	}
}

// DefaultUTCTimeZone ensures that time-based operations use the UTC timezone rather than the
// input time's local timezone.
func DefaultUTCTimeZone(enabled bool) EnvOption {
	return features(featureDefaultUTCTimeZone, enabled)
}

// StrictTimestampParsing controls how strictly timestamp strings and numeric timezone offsets are
// validated.
//
// Strict parsing is the default and is equivalent to StrictTimestampParsing(true):
//
//   - timestamp(string) accepts only RFC 3339 date-time strings. Inputs which time.Parse accepts
//     but RFC 3339 forbids are rejected: a ',' fractional second separator, single-digit time
//     fields, and numeric offsets whose hours exceed 23 or whose minutes exceed 59.
//   - Numeric timezone offsets passed to the timestamp accessors, e.g. getHours('+05:30'), must
//     have hours in [-23, 23] and minutes in [0, 59].
//
// StrictTimestampParsing(false) restores the parsing behavior of cel-go releases prior to v0.30.0
// for the timestamp(string) conversion and prior to v0.31.0 for timezone offsets: timestamp
// strings are accepted whenever time.Parse(time.RFC3339, ...) accepts them, and numeric offsets
// are not range checked. Note that some of these inputs resolve to an instant other than the one
// the string appears to describe; for example an offset of '+05:60' is treated as '+06:00'.
//
// Disabling strict parsing is intended as a compatibility aid for embedders which must keep
// evaluating expressions, or data, that were accepted by earlier releases. The relaxed rules also
// apply to timestamp literal validation (see ValidateTimestampLiterals).
func StrictTimestampParsing(enabled bool) EnvOption {
	return features(featureStrictTimestampParsing, enabled)
}

// ResolutionCandidates enables listing the fully-qualified names the type-checker attempted when
// an identifier or type name fails to resolve.
//
// The candidate names follow the namespace resolution order of the Container and account for
// abbreviations and aliases, for example:
//
//	undeclared reference to 'foo' (in container 'my.app') (tried 'my.app.foo', 'my.foo', 'foo')
//
// Disabled by default.
func ResolutionCandidates(enabled bool) EnvOption {
	return features(featureResolutionCandidates, enabled)
}

// features sets the given feature flags.  See list of Feature constants above.
func features(flag int, enabled bool) EnvOption {
	return func(e *Env) (*Env, error) {
		e.ensureMutableFeatures()
		e.features[flag] = enabled
		return e, nil
	}
}

func setLimit(id limitID, limit int) EnvOption {
	if limit < 0 {
		limit = -1
	}
	return func(e *Env) (*Env, error) {
		e.ensureMutableLimits()
		e.limits[id] = limit
		return e, nil
	}
}

// ParserRecursionLimit adjusts the AST depth the parser will tolerate.
// Defaults defined in the parser package.
func ParserRecursionLimit(limit int) EnvOption {
	return setLimit(limitParseRecursionDepth, limit)
}

// ParserErrorRecoveryLimit sets the number of attemtps the parser will take
// to recover after encountering an error.
func ParserErrorRecoveryLimit(limit int) EnvOption {
	return setLimit(limitParseErrorRecovery, limit)
}

// ParserExpressionSizeLimit adjusts the number of code points the expression parser is allowed to parse.
// Defaults are defined in the parser package. A negative value means unbounded.
func ParserExpressionSizeLimit(limit int) EnvOption {
	return setLimit(limitCodePointSize, limit)
}

// ExpressionNodeLimit adjusts the maximum number of expression nodes permitted during parsing
// and checking, including nodes created by macro expansion. Defaults are defined in the parser
// package (100,000). A negative value means unbounded.
func ExpressionNodeLimit(limit int) EnvOption {
	return setLimit(limitExpressionNodeCount, limit)
}

// ExpressionNestingDepthLimit records the maximum nesting depth permitted for ASTs in the
// environment configuration so that the value round-trips through env.Config export/import.
//
// ASTs loaded outside the parser (e.g. via ParsedExprToAst / CheckedExprToAst) bypass the
// parser's recursion limit, so those conversion paths validate nesting depth against the
// parser-matching default (250) to avoid a Go stack overflow during later checking or planning.
// Embedders that fully control their AST inputs and want to skip the check can construct the AST
// through the common/ast package directly rather than the cel conversion helpers.
func ExpressionNestingDepthLimit(limit int) EnvOption {
	return setLimit(limitMaxASTDepth, limit)
}

// RegexProgramSizeLimit caps the maximum regex program plan size permitted for regular expressions.
// A negative or zero value means unbounded.
func RegexProgramSizeLimit(limit int) EnvOption {
	return func(e *Env) (*Env, error) {
		var err error
		e, err = setLimit(limitRegexProgramSize, limit)(e)
		if err != nil {
			return nil, err
		}
		if limit > 0 {
			e, err = ASTValidators(ValidateRegexProgramSizeLimit(limit))(e)
			if err != nil {
				return nil, err
			}
		}
		return e, nil
	}
}

// EnableHiddenAccumulatorName sets the parser to use the identifier '@result' for accumulators
// which is not normally accessible from CEL source.
func EnableHiddenAccumulatorName(enabled bool) EnvOption {
	return func(e *Env) (*Env, error) {
		e.prsrOpts = append(e.prsrOpts, parser.EnableHiddenAccumulatorName(enabled))
		return e, nil
	}
}

// EnablePrattParser enables the Pratt parser implementation instead of the ANTLR parser.
//
// Enabled by default.
func EnablePrattParser(enabled bool) EnvOption {
	return func(e *Env) (*Env, error) {
		e.prsrOpts = append(e.prsrOpts, parser.EnablePrattParser(enabled))
		return e, nil
	}
}
