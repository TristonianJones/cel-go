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

package checker

import (
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/cost"
	"github.com/google/cel-go/common/overloads"
)

// WARNING: Any changes to cost calculations in this file require a corresponding change in
// interpreter/cost_stdlib.go

// estimateContext carries the state needed to estimate the cost of a single overload invocation:
// the operands, the cost of evaluating them, and the size bookkeeping of the coster which is
// walking the expression.
type estimateContext struct {
	coster     *coster
	expr       ast.Expr
	function   string
	overloadID string
	target     *AstNode
	args       []AstNode
	argCosts   []CostEstimate
}

// argCostSum returns the total cost of evaluating the call's arguments.
//
// Note, the cost of evaluating the target is accounted for by the caller, and short-circuiting
// operators compose their argument costs themselves rather than summing them.
func (ctx *estimateContext) argCostSum() CostEstimate {
	var sum CostEstimate
	for _, a := range ctx.argCosts {
		sum = sum.Add(a)
	}
	return sum
}

// argSize returns the size estimate of the argument at the given index, or an unknown size if the
// index is out of range.
func (ctx *estimateContext) argSize(i int) SizeEstimate {
	if i < 0 || i >= len(ctx.args) {
		return UnknownSizeEstimate()
	}
	return ctx.coster.sizeOrUnknown(ctx.args[i])
}

// targetSize returns the size estimate of the call target, or an unknown size for global calls.
func (ctx *estimateContext) targetSize() SizeEstimate {
	if ctx.target == nil {
		return UnknownSizeEstimate()
	}
	return ctx.coster.sizeOrUnknown(*ctx.target)
}

// sizeOf returns the size estimate of an arbitrary node participating in the call.
func (ctx *estimateContext) sizeOf(node AstNode) SizeEstimate {
	return ctx.coster.sizeOrUnknown(node)
}

// argEntrySize returns the key and value size estimates of the container argument at the given
// index, if the argument has them.
func (ctx *estimateContext) argEntrySize(i int) *entrySizeEstimate {
	if i < 0 || i >= len(ctx.args) {
		return nil
	}
	return ctx.coster.computeEntrySize(ctx.args[i].Expr())
}

// setResultEntrySize records the key and value size estimates of the container produced by the
// call so that they propagate to the expressions which consume it.
func (ctx *estimateContext) setResultEntrySize(size *entrySizeEstimate) {
	ctx.coster.setEntrySize(ctx.expr, size)
}

// standardEstimator computes the cost of a standard library overload.
//
// A nil result indicates the estimator does not apply to the call, in which case the caller falls
// back to the default cost of an O(1) function.
type standardEstimator func(ctx *estimateContext) *CallEstimate

// standardOverloadEstimators binds standard library overload IDs to their cost estimators.
//
// Cost functions for extension libraries belong to the library which declares them, registered
// with OverloadCostEstimate, rather than to this table.
var standardOverloadEstimators = map[string]standardEstimator{}

func init() {
	registerStandardEstimator(estimateStringToBytes, overloads.StringToBytes)
	registerStandardEstimator(estimateBytesToString, overloads.BytesToString)
	registerStandardEstimator(estimateStringPrefixSuffix, overloads.StartsWithString, overloads.EndsWithString)
	registerStandardEstimator(estimateInList, overloads.InList)
	registerStandardEstimator(estimateMatches, overloads.Matches, overloads.MatchesString)
	registerStandardEstimator(estimateContainsString, overloads.ContainsString)
	registerStandardEstimator(estimateShortCircuit, overloads.LogicalOr, overloads.LogicalAnd)
	registerStandardEstimator(estimateConditional, overloads.Conditional)
	registerStandardEstimator(estimateConcat, overloads.AddString, overloads.AddBytes, overloads.AddList)
	registerStandardEstimator(estimateComparison,
		overloads.LessString, overloads.GreaterString, overloads.LessEqualsString, overloads.GreaterEqualsString,
		overloads.LessBytes, overloads.GreaterBytes, overloads.LessEqualsBytes, overloads.GreaterEqualsBytes,
		overloads.Equals, overloads.NotEquals)
}

// registerStandardEstimator binds a single estimator to each of the overload IDs it computes.
func registerStandardEstimator(est standardEstimator, overloadIDs ...string) {
	for _, id := range overloadIDs {
		standardOverloadEstimators[id] = est
	}
}

// estimateStringToBytes computes the cost of a string to bytes conversion as a traversal of the
// input string.
func estimateStringToBytes(ctx *estimateContext) *CallEstimate {
	if len(ctx.args) != 1 {
		return nil
	}
	sz := ctx.argSize(0)
	// ResultSize max is when each char converts to 4 bytes.
	return &CallEstimate{
		CostEstimate: sz.MultiplyByCostFactor(common.StringTraversalCostFactor).Add(ctx.argCostSum()),
		ResultSize:   &SizeEstimate{Min: sz.Min, Max: cost.SafeMultiply(sz.Max, 4)}}
}

// estimateBytesToString computes the cost of a bytes to string conversion as a traversal of the
// input bytes.
func estimateBytesToString(ctx *estimateContext) *CallEstimate {
	if len(ctx.args) != 1 {
		return nil
	}
	sz := ctx.argSize(0)
	// ResultSize min is when 4 bytes convert to 1 char.
	return &CallEstimate{
		CostEstimate: sz.MultiplyByCostFactor(common.StringTraversalCostFactor).Add(ctx.argCostSum()),
		ResultSize:   &SizeEstimate{Min: sz.Min / 4, Max: sz.Max}}
}

// estimateStringPrefixSuffix computes the cost of startsWith and endsWith as a traversal of the
// prefix or suffix being tested.
func estimateStringPrefixSuffix(ctx *estimateContext) *CallEstimate {
	if len(ctx.args) != 1 {
		return nil
	}
	return &CallEstimate{
		CostEstimate: ctx.argSize(0).MultiplyByCostFactor(common.StringTraversalCostFactor).Add(ctx.argCostSum())}
}

// estimateInList computes the cost of a list containment check as a traversal of the list.
//
// If a list is composed entirely of constant values this is O(1), but we don't account for that
// here. We just assume all list containment checks are O(n).
func estimateInList(ctx *estimateContext) *CallEstimate {
	if len(ctx.args) != 2 {
		return nil
	}
	return &CallEstimate{CostEstimate: ctx.argSize(1).MultiplyByCostFactor(1).Add(ctx.argCostSum())}
}

// estimateMatches computes the cost of a regex match as the product of the string and pattern
// traversals.
//
// https://swtch.com/~rsc/regexp/regexp1.html applies to RE2 implementation supported by CEL
func estimateMatches(ctx *estimateContext) *CallEstimate {
	var strNode, regexNode AstNode
	if ctx.overloadID == overloads.MatchesString && ctx.target != nil && len(ctx.args) == 1 {
		strNode = *ctx.target
		regexNode = ctx.args[0]
	} else if ctx.overloadID == overloads.Matches && ctx.target == nil && len(ctx.args) == 2 {
		strNode = ctx.args[0]
		regexNode = ctx.args[1]
	}
	if strNode == nil || regexNode == nil {
		return nil
	}
	// Add one to string length for purposes of cost calculation to prevent product of string and regex to be 0
	// in case where string is empty but regex is still expensive.
	strCost := ctx.sizeOf(strNode).Add(FixedSizeEstimate(1)).MultiplyByCostFactor(common.StringTraversalCostFactor)
	// We don't know how many expressions are in the regex, just the string length (a huge
	// improvement here would be to somehow get a count the number of expressions in the regex or
	// how many states are in the regex state machine and use that to measure regex cost).
	// For now, we're making a guess that each expression in a regex is typically at least 4 chars
	// in length.
	regexCost := ctx.sizeOf(regexNode).MultiplyByCostFactor(common.RegexStringLengthCostFactor)
	return &CallEstimate{CostEstimate: strCost.Multiply(regexCost).Add(ctx.argCostSum())}
}

// estimateContainsString computes the cost of a substring search as the product of the string and
// substring traversals.
func estimateContainsString(ctx *estimateContext) *CallEstimate {
	if ctx.target == nil || len(ctx.args) != 1 {
		return nil
	}
	strCost := ctx.targetSize().MultiplyByCostFactor(common.StringTraversalCostFactor)
	substrCost := ctx.argSize(0).MultiplyByCostFactor(common.StringTraversalCostFactor)
	return &CallEstimate{CostEstimate: strCost.Multiply(substrCost).Add(ctx.argCostSum())}
}

// estimateShortCircuit computes the cost of the logical operators, whose minimum cost is the cost
// of the left-hand side alone when the operator short circuits.
func estimateShortCircuit(ctx *estimateContext) *CallEstimate {
	if len(ctx.argCosts) != 2 {
		return nil
	}
	lhs := ctx.argCosts[0]
	rhs := ctx.argCosts[1]
	return &CallEstimate{CostEstimate: CostEstimate{Min: lhs.Min, Max: lhs.Add(rhs).Max}}
}

// estimateConditional computes the cost of a ternary as the cost of the condition plus whichever
// branch is taken, and propagates the size of the branch results.
func estimateConditional(ctx *estimateContext) *CallEstimate {
	if len(ctx.args) != 3 || len(ctx.argCosts) != 3 {
		return nil
	}
	size := ctx.argSize(1).Union(ctx.argSize(2))
	ctx.setResultEntrySize(ctx.argEntrySize(1).union(ctx.argEntrySize(2)))
	conditionalCost := ctx.argCosts[0]
	ifTrueCost := ctx.argCosts[1]
	ifFalseCost := ctx.argCosts[2]
	argCost := conditionalCost.Add(ifTrueCost.Union(ifFalseCost))
	return &CallEstimate{CostEstimate: argCost, ResultSize: &size}
}

// estimateConcat computes the cost of string, bytes, and list concatenation, and propagates the
// size of the concatenated result.
func estimateConcat(ctx *estimateContext) *CallEstimate {
	if len(ctx.args) != 2 {
		return nil
	}
	lhsSize := ctx.argSize(0)
	rhsSize := ctx.argSize(1)
	resultSize := lhsSize.Add(rhsSize)
	resultEntrySize := ctx.argEntrySize(0).union(ctx.argEntrySize(1))
	if resultEntrySize != nil {
		ctx.setResultEntrySize(resultEntrySize)
	}
	if ctx.overloadID == overloads.AddList {
		// list concatenation is O(1), but we handle it here to track size
		return &CallEstimate{CostEstimate: FixedCostEstimate(1).Add(ctx.argCostSum()), ResultSize: &resultSize}
	}
	return &CallEstimate{
		CostEstimate: resultSize.MultiplyByCostFactor(common.StringTraversalCostFactor).Add(ctx.argCostSum()),
		ResultSize:   &resultSize}
}

// estimateComparison computes the cost of equality and ordering as a traversal of the smaller of
// the two operands, since a comparison stops at the first difference.
func estimateComparison(ctx *estimateContext) *CallEstimate {
	if len(ctx.args) != 2 {
		return nil
	}
	lhsSize := ctx.argSize(0)
	rhsSize := ctx.argSize(1)
	minCost := uint64(0)
	smallestMax := min(lhsSize.Max, rhsSize.Max)
	if smallestMax > 0 {
		minCost = 1
	}
	// equality of 2 scalar values results in a cost of 1
	return &CallEstimate{
		CostEstimate: CostEstimate{Min: minCost, Max: smallestMax}.
			MultiplyByCostFactor(common.StringTraversalCostFactor).Add(ctx.argCostSum()),
	}
}
