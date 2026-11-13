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
	"cel.dev/cel-go/cel/runtime"
	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/functions"
	"cel.dev/cel-go/interpreter"
)

// Program is an evaluable view of an Ast.
type Program = runtime.Program

// Activation used to resolve identifiers by name and references by id.
//
// An Activation is the primary mechanism by which a caller supplies input into a CEL program.
type Activation = runtime.Activation

// NewActivation returns an activation based on a map-based binding where the map keys are
// expected to be qualified names used with ResolveName calls.
//
// The input `bindings` may either be of type `Activation` or `map[string]any`.
//
// Lazy bindings may be supplied within the map-based input in either of the following forms:
// - func() any
// - func() ref.Val
//
// The output of the lazy binding will overwrite the variable reference in the internal map.
//
// Values which are not represented as ref.Val types on input may be adapted to a ref.Val using
// the types.Adapter configured in the environment.
func NewActivation(bindings any) (Activation, error) {
	return runtime.NewActivation(bindings)
}

// PartialActivation extends the Activation interface with a set of unknown AttributePatterns.
type PartialActivation = runtime.PartialActivation

// NoVars returns an empty Activation.
func NoVars() Activation {
	return runtime.NoVars()
}

// PartialVars returns a PartialActivation which contains variables and a set of AttributePattern
// values that indicate variables or parts of variables whose value is not yet known.
//
// This method relies on manually configured sets of missing attribute patterns. For a method which
// infers the missing variables from the input and the configured environment, use Env.PartialVars().
//
// The `vars` value may either be an Activation or any valid input to the NewActivation call.
func PartialVars(vars any, unknowns ...*AttributePatternType) (PartialActivation, error) {
	return runtime.PartialVars(vars, unknowns...)
}

// FunctionActivation extends the Activation interface with implementations for the functions
// declared with a late binding.
type FunctionActivation = runtime.FunctionActivation

// LateBoundFunction is the signature required of all late-bound function implementations.
//
// The overload id indicates which of the declared overloads matched the call arguments, which
// permits a single implementation to serve all overloads of the function.
//
// An implementation which depends on the state of the evaluation should capture that state in a
// closure, as the activation is not an input to the call.
type LateBoundFunction = runtime.LateBoundFunction

// FunctionVars returns a FunctionActivation combining the variable bindings from `vars` with
// the late-bound function implementations from `funcs`.
func FunctionVars(vars any, funcs map[string]LateBoundFunction) (FunctionActivation, error) {
	return runtime.FunctionVars(vars, funcs)
}

// AttributePattern returns an AttributePattern that matches a top-level variable. The pattern is
// mutable, and its methods support the specification of one or more qualifier patterns.
func AttributePattern(varName string) *AttributePatternType {
	return runtime.AttributePattern(varName)
}

// AttributePatternType represents a top-level variable with an optional set of qualifier patterns.
type AttributePatternType = runtime.AttributePatternType

// EvalDetails holds additional information observed during the Eval() call.
type EvalDetails = runtime.EvalDetails

// EvalResult encapsulates the response from a ConcurrentEval call.
type EvalResult = runtime.EvalResult

func newProgram(e *Env, a *ast.AST, opts []ProgramOption) (Program, error) {
	sharedDisp, hasAsync, err := e.initDispatcher()
	if err != nil {
		return nil, err
	}
	rtEnv := e.rt
	if err := e.applyRuntime(runtime.EnableErrorOnBadPresenceTest(e.HasFeature(featureEnableErrorOnBadPresenceTest))); err != nil {
		return nil, err
	}
	if limit := e.limits[limitRegexProgramSize]; limit > 0 {
		if err := e.applyRuntime(runtime.RegexProgramSizeLimit(limit)); err != nil {
			return nil, err
		}
	}
	rtEnv.Container = e.Container
	mergedOpts := make([]ProgramOption, 0, len(opts)+2)
	mergedOpts = append(mergedOpts, runtime.WithDispatcher(sharedDisp, hasAsync))
	if len(e.costModel.tracker) > 0 {
		mergedOpts = append(mergedOpts, runtime.CostTrackerOptions(e.costModel.tracker...))
	}
	mergedOpts = append(mergedOpts, opts...)
	return rtEnv.Program(a, mergedOpts...)
}

// Unused imports guard for types used only in doc/alias compatibility.
var (
	_ = (*functions.Overload)(nil)
	_ = interpreter.EmptyActivation
)
