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

package interpreter

import (
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/cost"
	"github.com/google/cel-go/common/overloads"
	"github.com/google/cel-go/common/types/ref"
)

// WARNING: Any changes to cost calculations in this file require a corresponding change in
// checker/cost_stdlib.go

// standardOverloadTrackers binds standard library overload IDs to their runtime cost trackers.
//
// Cost functions for extension libraries belong to the library which declares them, registered
// with OverloadCostTracker, rather than to this table.
var standardOverloadTrackers = map[string]FunctionTracker{}

func init() {
	registerStandardTracker(trackStringPrefixSuffix, overloads.StartsWithString, overloads.EndsWithString)
	registerStandardTracker(trackStringConversion, overloads.StringToBytes, overloads.BytesToString)
	registerStandardTracker(trackInList, overloads.InList)
	registerStandardTracker(trackComparison,
		overloads.LessString, overloads.GreaterString, overloads.LessEqualsString, overloads.GreaterEqualsString,
		overloads.LessBytes, overloads.GreaterBytes, overloads.LessEqualsBytes, overloads.GreaterEqualsBytes,
		overloads.Equals, overloads.NotEquals)
	registerStandardTracker(trackConcat, overloads.AddString, overloads.AddBytes)
	registerStandardTracker(trackMatches, overloads.Matches, overloads.MatchesString)
	registerStandardTracker(trackContainsString, overloads.ContainsString)
}

// registerStandardTracker binds a single tracker to each of the overload IDs it computes.
func registerStandardTracker(tracker FunctionTracker, overloadIDs ...string) {
	for _, id := range overloadIDs {
		standardOverloadTrackers[id] = tracker
	}
}

// trackStringPrefixSuffix computes the cost of startsWith and endsWith as a traversal of the
// prefix or suffix being tested.
func trackStringPrefixSuffix(args []ref.Val, _ ref.Val) *uint64 {
	total := cost.SafeMultiplyByFactor(actualSize(args[1]), common.StringTraversalCostFactor)
	return &total
}

// trackStringConversion computes the cost of a string and bytes conversion as a traversal of the
// input value.
func trackStringConversion(args []ref.Val, _ ref.Val) *uint64 {
	total := cost.SafeMultiplyByFactor(actualSize(args[0]), common.StringTraversalCostFactor)
	return &total
}

// trackInList computes the cost of a list containment check as a traversal of the list.
//
// If a list is composed entirely of constant values this is O(1), but we don't account for that
// here. We just assume all list containment checks are O(n).
func trackInList(args []ref.Val, _ ref.Val) *uint64 {
	total := actualSize(args[1])
	return &total
}

// trackComparison computes the cost of equality and ordering as a traversal of the smaller of the
// two operands, since a comparison stops at the first difference.
//
// When we check the equality of 2 scalar values (e.g. 2 integers, 2 floating-point numbers, 2
// booleans etc.), actualSize by definition returns 1 for each operand, resulting in an overall
// cost of 1.
func trackComparison(args []ref.Val, _ ref.Val) *uint64 {
	minSize := min(actualSize(args[0]), actualSize(args[1]))
	total := cost.SafeMultiplyByFactor(minSize, common.StringTraversalCostFactor)
	return &total
}

// trackConcat computes the cost of string and bytes concatenation as a traversal of both operands.
//
// In the worst case scenario, we would need to reallocate a new backing store and copy both
// operands over.
func trackConcat(args []ref.Val, _ ref.Val) *uint64 {
	argSize := cost.SafeAdd(actualSize(args[0]), actualSize(args[1]))
	total := cost.SafeMultiplyByFactor(argSize, common.StringTraversalCostFactor)
	return &total
}

// trackMatches computes the cost of a regex match as the product of the string and pattern
// traversals.
//
// https://swtch.com/~rsc/regexp/regexp1.html applies to RE2 implementation supported by CEL
func trackMatches(args []ref.Val, _ ref.Val) *uint64 {
	// Add one to string length for purposes of cost calculation to prevent product of string and
	// regex to be 0 in case where string is empty but regex is still expensive.
	strCost := cost.SafeMultiplyByFactor(cost.SafeAdd(1, actualSize(args[0])), common.StringTraversalCostFactor)
	// We don't know how many expressions are in the regex, just the string length (a huge
	// improvement here would be to somehow get a count the number of expressions in the regex or
	// how many states are in the regex state machine and use that to measure regex cost).
	// For now, we're making a guess that each expression in a regex is typically at least 4 chars
	// in length.
	regexCost := cost.SafeMultiplyByFactor(actualSize(args[1]), common.RegexStringLengthCostFactor)
	total := cost.SafeMultiply(strCost, regexCost)
	return &total
}

// trackContainsString computes the cost of a substring search as the product of the string and
// substring traversals.
func trackContainsString(args []ref.Val, _ ref.Val) *uint64 {
	strCost := cost.SafeMultiplyByFactor(actualSize(args[0]), common.StringTraversalCostFactor)
	substrCost := cost.SafeMultiplyByFactor(actualSize(args[1]), common.StringTraversalCostFactor)
	total := cost.SafeMultiply(strCost, substrCost)
	return &total
}
