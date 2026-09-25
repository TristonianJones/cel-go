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

package cost

import (
	"errors"
	"fmt"
)

// Validate reports whether the model is well-formed.
//
// A model is checked when it is registered, rather than when it is evaluated, because neither of
// the two evaluation paths can report a problem: an estimator has nowhere to return an error, and
// a tracker which cannot answer an expression would have to choose between charging nothing and
// charging the maximum, one of which lets an expression through and the other of which fails every
// evaluation that sets a cost limit.
//
// The checks are:
//   - the overload has an ID, and an evaluation cost expression;
//   - every element or key projection is applied to an expression which denotes a value.
//
// Projections read the value they project from, so ElemOf, KeyOf, ElemTotal and the ArgElem,
// ArgKey, TargetElem and TargetKey shorthands are defined over Arg, Target, Result, and
// projections of those. A projection of a computed quantity, such as ElemOf(Sum(Arg(0), Arg(1))),
// has an estimate but no runtime counterpart, and is reported here.
func (m OverloadModel) Validate() error {
	if m.ID == "" {
		return errors.New("overload model requires an ID")
	}
	if m.Cost == nil {
		return fmt.Errorf("overload %q requires an EvalCost expression", m.ID)
	}
	if err := validateQuantityExpr(m.Cost); err != nil {
		return fmt.Errorf("overload %q eval cost: %w", m.ID, err)
	}
	if err := validateQuantityExpr(m.Size); err != nil {
		return fmt.Errorf("overload %q result size: %w", m.ID, err)
	}
	return nil
}

// ValidateOverloadModels reports the first model which is not well-formed.
func ValidateOverloadModels(models ...OverloadModel) error {
	for _, m := range models {
		if err := m.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// validateQuantityExpr reports the first operand of an expression tree which cannot be evaluated.
func validateQuantityExpr(expr QuantityExpr) error {
	if expr == nil {
		return nil
	}
	label, operands := describeQuantityExpr(expr)
	if _, isProjection := expr.(projection); isProjection {
		if len(operands) == 0 || operands[0] == nil {
			return fmt.Errorf("%s requires an operand", label)
		}
		if _, ok := operands[0].(valueSource); !ok {
			operandLabel, _ := describeQuantityExpr(operands[0])
			return fmt.Errorf(
				"%s is not defined over %s: element and key projections apply to Arg, Target, Result, and projections of those",
				label, operandLabel)
		}
	}
	for _, operand := range operands {
		if err := validateQuantityExpr(operand); err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
	}
	return nil
}

// projection marks the expressions which read the elements or keys of the value they are applied
// to, and so constrain what that operand may be.
type projection interface {
	QuantityExpr
	projects()
}

func (elemExpr) projects()      {}
func (keyExpr) projects()       {}
func (elemTotalExpr) projects() {}

// describeQuantityExpr reports the constructor name of a quantity expression and its operands.
//
// Every expression type must be listed: an omission makes an expression's operands invisible to
// validation, and reports an unhelpful name in the error message. TestDescribeQuantityExpr covers
// each constructor.
func describeQuantityExpr(expr QuantityExpr) (string, []QuantityExpr) {
	switch e := expr.(type) {
	case constExpr:
		return "Const", nil
	case argExpr:
		return "Arg", nil
	case intArgExpr:
		return "IntArg", nil
	case targetExpr:
		return "Target", nil
	case intTargetExpr:
		return "IntTarget", nil
	case resultExpr:
		return "Result", nil
	case elemExpr:
		return "ElemOf", []QuantityExpr{e.expr}
	case keyExpr:
		return "KeyOf", []QuantityExpr{e.expr}
	case elemTotalExpr:
		return "ElemTotal", []QuantityExpr{e.expr}
	case addExpr:
		return "Sum", e.terms
	case subExpr:
		return "Sub", []QuantityExpr{e.lhs, e.rhs}
	case mulExpr:
		return "Mul", e.terms
	case scaleExpr:
		return "Scale", []QuantityExpr{e.expr}
	case minExpr:
		return "Min", []QuantityExpr{e.lhs, e.rhs}
	case maxExpr:
		return "Max", []QuantityExpr{e.lhs, e.rhs}
	case unionExpr:
		return "Union", e.terms
	case rangedExpr:
		return "Ranged", []QuantityExpr{e.minExpr, e.maxExpr}
	case atLeastOneExpr:
		return "AtLeastOneQuantity", []QuantityExpr{e.expr}
	case listExpr:
		return "List", []QuantityExpr{e.lenExpr, e.elemExpr}
	case mapExpr:
		return "Map", []QuantityExpr{e.sizeExpr, e.keyExpr, e.valExpr}
	default:
		return fmt.Sprintf("%T", expr), nil
	}
}
