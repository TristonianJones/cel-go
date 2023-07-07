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

package cel

import (
	"fmt"
	"regexp"

	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/overloads"

	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// ASTValidators configures a set of ASTValidator instances into the target environment.
//
// Validators are applied in the order in which the are specified and are treated as singletons.
// The same ASTValidator with a given name will not be applied more than once.
func ASTValidators(validators ...ASTValidator) EnvOption {
	return func(e *Env) (*Env, error) {
		for _, v := range validators {
			if !e.HasValidator(v.Name()) {
				e.validators = append(e.validators, v)
			}
		}
		return e, nil
	}
}

// ASTValidator defines a singleton interface for validating a type-checked Ast against an environment.
//
// Note: the Issues argument is mutable in the sense that it is intended to collect errors which will be
// reported to the caller.
type ASTValidator interface {
	// Name returns the name of the validator. Names must be unique.
	Name() string

	// Validate validates a given Ast within an Environment and collects a set of potential issues.
	Validate(*Env, *ast.CheckedAST, *Issues)
}

// ExtendedValidations collects a set of common AST validations which reduce the likelihood of runtime errors.
//
// - Validate duration and timestamp literals
// - Ensure regex strings are valid
// - Disable mixed type list and map literals
func ExtendedValidations() EnvOption {
	return ASTValidators(
		ValidateDurationLiterals(),
		ValidateTimestampLiterals(),
		ValidateRegexLiterals(),
		ValidateHomogeneousAggregateLiterals(),
	)
}

// ValidateDurationLiterals ensures that duration literal arguments are valid immediately after type-check.
func ValidateDurationLiterals() ASTValidator {
	return newFormatValidator(overloads.TypeConvertDuration, 0, evalCall)
}

// ValidateTimestampLiterals ensures that timestamp literal arguments are valid immediately after type-check.
func ValidateTimestampLiterals() ASTValidator {
	return newFormatValidator(overloads.TypeConvertTimestamp, 0, evalCall)
}

// ValidateRegexLiterals ensures that regex patterns are validated after type-check.
func ValidateRegexLiterals() ASTValidator {
	return newFormatValidator(overloads.Matches, 0, compileRegex)
}

// ValidateHomogeneousAggregateLiterals checks that all list and map literals entries have the same types, i.e.
// no mixed list element types or mixed map key or map value types.
//
// Note: the string format call relies on a mixed element type list for ease of use, so this check skips all
// literals which occur within string format calls.
func ValidateHomogeneousAggregateLiterals() ASTValidator {
	return homogeneousAggregateLiteralValidator{}
}

type argChecker func(env *Env, call, arg ast.NavigableExpr) error

func newFormatValidator(funcName string, argNum int, check argChecker) formatValidator {
	return formatValidator{
		funcName: funcName,
		check:    check,
		argNum:   argNum,
	}
}

type formatValidator struct {
	funcName string
	argNum   int
	check    argChecker
}

// Name returns the unique name of this function format validator.
func (v formatValidator) Name() string {
	return fmt.Sprintf("cel.lib.std.validate.functions.%s", v.funcName)
}

// Validate searches the AST for uses of a given function name with a constant argument and performs a check
// on whether the argument is a valid literal value.
func (v formatValidator) Validate(e *Env, a *ast.CheckedAST, iss *Issues) {
	root := ast.NavigateCheckedAST(a)
	funcCalls := ast.MatchDescendants(root, ast.FunctionMatcher(v.funcName))
	for _, call := range funcCalls {
		callArgs := call.AsCall().Args()
		if len(callArgs) <= v.argNum {
			continue
		}
		litArg := callArgs[v.argNum]
		if litArg.Kind() != ast.LiteralKind {
			continue
		}
		if err := v.check(e, call, litArg); err != nil {
			iss.ReportErrorAtID(litArg.ID(), "invalid %s argument", v.funcName)
		}
	}
}

func evalCall(env *Env, call, arg ast.NavigableExpr) error {
	ast := ParsedExprToAst(&exprpb.ParsedExpr{Expr: call.ToExpr()})
	prg, err := env.Program(ast)
	if err != nil {
		return err
	}
	_, _, err = prg.Eval(NoVars())
	return err
}

func compileRegex(_ *Env, _, arg ast.NavigableExpr) error {
	pattern := arg.AsLiteral().Value().(string)
	_, err := regexp.Compile(pattern)
	return err
}

type homogeneousAggregateLiteralValidator struct{}

// Name returns the unique name of the homogeneous type validator.
func (homogeneousAggregateLiteralValidator) Name() string {
	return "cel.lib.std.validate.types.homogeneous"
}

// Validate validates that all lists and map literals have homogeneous types, i.e. don't contain dyn types.
//
// This validator makes an exception for list and map literals which occur at any level of nesting within
// string format calls.
func (v homogeneousAggregateLiteralValidator) Validate(_ *Env, a *ast.CheckedAST, iss *Issues) {
	root := ast.NavigateCheckedAST(a)
	listExprs := ast.MatchDescendants(root, ast.KindMatcher(ast.ListKind))
	for _, listExpr := range listExprs {
		// TODO: Add a validator config object which allows libraries to influence validation options
		// for validators that *might* be configured. In this case, a way of skipping certain function
		// overloads.
		if hasStringFormatAncestor(listExpr) {
			continue
		}
		l := listExpr.AsList()
		elements := l.Elements()
		optIndices := l.OptionalIndices()
		var elemType *Type
		for i, e := range elements {
			et := e.Type()
			if isOptionalIndex(i, optIndices) {
				et = et.Parameters()[0]
			}
			if elemType == nil {
				elemType = et
				continue
			}
			if !elemType.IsEquivalentType(et) {
				v.typeMismatch(iss, e.ID(), elemType, et)
				break
			}
		}
	}
	mapExprs := ast.MatchDescendants(root, ast.KindMatcher(ast.MapKind))
	for _, mapExpr := range mapExprs {
		if hasStringFormatAncestor(mapExpr) {
			continue
		}
		m := mapExpr.AsMap()
		entries := m.Entries()
		var keyType, valType *Type
		for _, e := range entries {
			key, val := e.Key(), e.Value()
			kt, vt := key.Type(), val.Type()
			if e.IsOptional() {
				vt = vt.Parameters()[0]
			}
			if keyType == nil && valType == nil {
				keyType, valType = kt, vt
				continue
			}
			if !keyType.IsEquivalentType(kt) {
				v.typeMismatch(iss, key.ID(), keyType, kt)
			}
			if !valType.IsEquivalentType(vt) {
				v.typeMismatch(iss, val.ID(), valType, vt)
			}
		}
	}
}

func hasStringFormatAncestor(e ast.NavigableExpr) bool {
	if parent, found := e.Parent(); found {
		if parent.Kind() == ast.CallKind && parent.AsCall().FunctionName() == "format" {
			return true
		}
		if parent.Kind() == ast.ListKind || parent.Kind() == ast.MapKind {
			return hasStringFormatAncestor(parent)
		}
	}
	return false
}

func isOptionalIndex(i int, optIndices []int32) bool {
	for _, optInd := range optIndices {
		if i == int(optInd) {
			return true
		}
	}
	return false
}

func (homogeneousAggregateLiteralValidator) typeMismatch(iss *Issues, id int64, expected, actual *Type) {
	iss.ReportErrorAtID(id, "expected type '%s' but found '%s'", FormatCelType(expected), FormatCelType(actual))
}
