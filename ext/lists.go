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

package ext

import (
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/checker"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/decls"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/interpreter"
	"github.com/google/cel-go/parser"
)

// Lists returns a cel.EnvOption to configure extended functions for list manipulation.
// As a general note, all indices are zero-based.
//
// # Distinct
//
// Introduced in version: 2
//
// Returns the distinct elements of a list.
//
//	<list(T)>.distinct() -> <list(T)>
//
// Examples:
//
//	[1, 2, 2, 3, 3, 3].distinct() // return [1, 2, 3]
//	["b", "b", "c", "a", "c"].distinct() // return ["b", "c", "a"]
//	[1, "b", 2, "b"].distinct() // return [1, "b", 2]
//
// # Range
//
// Introduced in version: 2
//
// Returns a list of integers from 0 to n-1.
//
//	lists.range(<int>) -> <list(int)>
//
// Examples:
//
//	lists.range(5) -> [0, 1, 2, 3, 4]
//
// # Reverse
//
// Introduced in version: 2
//
// Returns the elements of a list in reverse order.
//
//	<list(T)>.reverse() -> <list(T)>
//
// Examples:
//
//	[5, 3, 1, 2].reverse() // return [2, 1, 3, 5]
//
// # Slice
//
// Returns a new sub-list using the indexes provided.
//
//	<list>.slice(<int>, <int>) -> <list>
//
// Examples:
//
//	[1,2,3,4].slice(1, 3) // return [2, 3]
//	[1,2,3,4].slice(2, 4) // return [3 ,4]
//
// # Flatten
//
// Flattens a list recursively.
// If an optional depth is provided, the list is flattened to a the specificied level.
// A negative depth value will result in an error.
//
//	<list>.flatten(<list>) -> <list>
//	<list>.flatten(<list>, <int>) -> <list>
//
// Examples:
//
// [1,[2,3],[4]].flatten() // return [1, 2, 3, 4]
// [1,[2,[3,4]]].flatten() // return [1, 2, [3, 4]]
// [1,2,[],[],[3,4]].flatten() // return [1, 2, 3, 4]
// [1,[2,[3,[4]]]].flatten(2) // return [1, 2, 3, [4]]
// [1,[2,[3,[4]]]].flatten(-1) // error
//
// # Sort
//
// Introduced in version: 2
//
// Sorts a list with comparable elements. If the element type is not comparable
// or the element types are not the same, the function will produce an error.
//
//	<list(T)>.sort() -> <list(T)>
//	T in {int, uint, double, bool, duration, timestamp, string, bytes}
//
// Examples:
//
//	[3, 2, 1].sort() // return [1, 2, 3]
//	["b", "c", "a"].sort() // return ["a", "b", "c"]
//	[1, "b"].sort() // error
//	[[1, 2, 3]].sort() // error
//
// # SortBy
//
// Sorts a list by a key value, i.e., the order is determined by the result of
// an expression applied to each element of the list.
// The output of the key expression must be a comparable type, otherwise the
// function will return an error.
//
//	<list(T)>.sortBy(<bindingName>, <keyExpr>) -> <list(T)>
//	keyExpr returns a value in {int, uint, double, bool, duration, timestamp, string, bytes}
//
// Examples:
//
//	[
//	  Player { name: "foo", score: 0 },
//	  Player { name: "bar", score: -10 },
//	  Player { name: "baz", score: 1000 },
//	].sortBy(e, e.score).map(e, e.name)
//	== ["bar", "foo", "baz"]
func Lists(options ...ListsOption) cel.EnvOption {
	l := &listsLib{version: math.MaxUint32}
	for _, o := range options {
		l = o(l)
	}
	return cel.Lib(l)
}

type listsLib struct {
	version uint32
}

// LibraryName implements the SingletonLibrary interface method.
func (listsLib) LibraryName() string {
	return "cel.lib.ext.lists"
}

// ListsOption is a functional interface for configuring the strings library.
type ListsOption func(*listsLib) *listsLib

// ListsVersion configures the version of the string library.
//
// The version limits which functions are available. Only functions introduced
// below or equal to the given version included in the library. If this option
// is not set, all functions are available.
//
// See the library documentation to determine which version a function was introduced.
// If the documentation does not state which version a function was introduced, it can
// be assumed to be introduced at version 0, when the library was first created.
func ListsVersion(version uint32) ListsOption {
	return func(lib *listsLib) *listsLib {
		lib.version = version
		return lib
	}
}

// CompileOptions implements the Library interface method.
func (lib listsLib) CompileOptions() []cel.EnvOption {
	listType := cel.ListType(cel.TypeParamType("T"))
	listListType := cel.ListType(listType)
	listDyn := cel.ListType(cel.DynType)
	opts := []cel.EnvOption{
		cel.Function("slice",
			cel.MemberOverload("list_slice",
				[]*cel.Type{listType, cel.IntType, cel.IntType}, listType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					list := args[0].(traits.Lister)
					start := args[1].(types.Int)
					end := args[2].(types.Int)
					result, err := slice(list, start, end)
					if err != nil {
						return types.WrapErr(err)
					}
					return result
				}),
			),
		),
	}
	if lib.version >= 1 {
		opts = append(opts,
			cel.Function("flatten",
				cel.MemberOverload("list_flatten",
					[]*cel.Type{listListType}, listType,
					cel.UnaryBinding(func(arg ref.Val) ref.Val {
						// double-check as type-guards disabled
						list, ok := arg.(traits.Lister)
						if !ok {
							return types.ValOrErr(arg, "no such overload: %v.flatten()", arg.Type())
						}
						flatList, err := flatten(list, 1)
						if err != nil {
							return types.WrapErr(err)
						}

						return types.DefaultTypeAdapter.NativeToValue(flatList)
					}),
				),
				cel.MemberOverload("list_flatten_int",
					[]*cel.Type{listDyn, types.IntType}, listDyn,
					cel.BinaryBinding(func(arg1, arg2 ref.Val) ref.Val {
						// double-check as type-guards disabled
						list, ok := arg1.(traits.Lister)
						if !ok {
							return types.ValOrErr(arg1, "no such overload: %v.flatten(%v)", arg1.Type(), arg2.Type())
						}
						depth, ok := arg2.(types.Int)
						if !ok {
							return types.ValOrErr(arg1, "no such overload: %v.flatten(%v)", arg1.Type(), arg2.Type())
						}
						flatList, err := flatten(list, int64(depth))
						if err != nil {
							return types.WrapErr(err)
						}

						return types.DefaultTypeAdapter.NativeToValue(flatList)
					}),
				),
				// To handle the case where a variable of just `list(T)` is provided at runtime
				// with a graceful failure more, disable the type guards since the implementation
				// can handle lists which are already flat.
				decls.DisableTypeGuards(true),
			),
		)
	}
	if lib.version >= 2 {
		sortDecl := cel.Function("sort",
			append(
				templatedOverloads(comparableTypes, func(t *cel.Type) cel.FunctionOpt {
					return cel.MemberOverload(
						fmt.Sprintf("list_%s_sort", t.TypeName()),
						[]*cel.Type{cel.ListType(t)}, cel.ListType(t),
					)
				}),
				cel.SingletonUnaryBinding(
					func(arg ref.Val) ref.Val {
						// validated by type-guards
						list := arg.(traits.Lister)
						sorted, err := sortList(list)
						if err != nil {
							return types.WrapErr(err)
						}

						return sorted
					},
					// List traits
					traits.ListerType,
				),
			)...,
		)
		opts = append(opts, sortDecl)
		opts = append(opts, cel.Macros(cel.ReceiverMacro("sortBy", 2, sortByMacro)))
		opts = append(opts, cel.Function("@sortByAssociatedKeys",
			append(
				templatedOverloads(comparableTypes, func(u *cel.Type) cel.FunctionOpt {
					return cel.MemberOverload(
						fmt.Sprintf("list_%s_sortByAssociatedKeys", u.TypeName()),
						[]*cel.Type{listType, cel.ListType(u)}, listType,
					)
				}),
				cel.SingletonBinaryBinding(
					func(arg1, arg2 ref.Val) ref.Val {
						// validated by type-guards
						list := arg1.(traits.Lister)
						keys := arg2.(traits.Lister)
						sorted, err := sortListByAssociatedKeys(list, keys)
						if err != nil {
							return types.WrapErr(err)
						}
						return sorted
					},
					// List traits
					traits.ListerType,
				),
			)...,
		))

		opts = append(opts, cel.Function("lists.range",
			cel.Overload("lists_range",
				[]*cel.Type{cel.IntType}, cel.ListType(cel.IntType),
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					n := args[0].(types.Int)
					return genRange(n)
				}),
			),
		))
		opts = append(opts, cel.Function("reverse",
			cel.MemberOverload("list_reverse",
				[]*cel.Type{listType}, listType,
				cel.FunctionBinding(func(args ...ref.Val) ref.Val {
					list := args[0].(traits.Lister)
					return reverseList(list)
				}),
			),
		))
		opts = append(opts, cel.Function("distinct",
			cel.MemberOverload("list_distinct",
				[]*cel.Type{listType}, listType,
				cel.UnaryBinding(func(list ref.Val) ref.Val {
					return distinctList(list.(traits.Lister))
				}),
			),
		))
		opts = append(opts, cel.CostEstimatorOptions(
			checker.OverloadCostEstimate("list_distinct", estimateListDistinct()),
			checker.OverloadCostEstimate("list_flatten", estimateListFlatten()),
			checker.OverloadCostEstimate("list_flatten_int", estimateListFlatten()),
			checker.OverloadCostEstimate("list_reverse", estimateListReverse()),
			checker.OverloadCostEstimate("list_slice", estimateListSlice()),
			checker.OverloadCostEstimate("lists_range", estimateListsRangeCost()),
		))
		sortCostOpts := make([]checker.CostOption, len(comparableTypes))
		for i, t := range comparableTypes {
			cmpFactor := 0.2
			sortCostOpts[i] = checker.OverloadCostEstimate(
				fmt.Sprintf("list_%s_sortByAssociatedKeys", t.TypeName()),
				estimateListSortType(cmpFactor, t),
			)
		}
		sortByCostOpts := make([]checker.CostOption, len(comparableTypes))
		for i, t := range comparableTypes {
			cmpFactor := 0.1
			sortByCostOpts[i] = checker.OverloadCostEstimate(
				fmt.Sprintf("list_%s_sort", t.TypeName()),
				estimateListSortType(cmpFactor, t),
			)
		}
		opts = append(opts, cel.CostEstimatorOptions(sortCostOpts...))
		opts = append(opts, cel.CostEstimatorOptions(sortByCostOpts...))

	}
	return opts
}

// ProgramOptions implements the Library interface method.
func (listsLib) ProgramOptions() []cel.ProgramOption {
	return []cel.ProgramOption{
		cel.CostTrackerOptions(
			interpreter.OverloadCostTracker("list_distinct", trackListDistinctCost()),
			interpreter.OverloadCostTracker("list_flatten", trackListFlattenCost()),
			interpreter.OverloadCostTracker("list_flatten_int", trackListFlattenCost()),
			interpreter.OverloadCostTracker("list_reverse", trackListReverseCost()),
			interpreter.OverloadCostTracker("list_slice", trackListSliceCost()),
			interpreter.OverloadCostTracker("lists_range", trackListsRangeCost()),
		),
	}
}

func estimateListDistinct() checker.FunctionEstimator {
	return func(estimator checker.CostEstimator, target *checker.AstNode, args []checker.AstNode) *checker.CallEstimate {
		t := *target
		targetSize := checker.EstimateSize(estimator, t)
		costEstimate := targetSize.Multiply(targetSize).
			MultiplyByCostFactor(1).
			Add(createListCostEstimate).
			Add(callCostEstimate)
		return &checker.CallEstimate{CostEstimate: costEstimate, ResultSize: &targetSize}
	}
}

func estimateListFlatten() checker.FunctionEstimator {
	// The cost of a flatten operation is proportional to the depth of flattening, and the number of
	// elements copied, so the expectation is that the cost factor is based on an expectation that
	// elements within a list will be small (~16 elements) and that this quantity of elements is
	// proportial to the flatten depth.
	return estimateListCopy( /*variableCost=*/ true, func(args []checker.AstNode) *float64 {
		depthRet := float64(1)
		if len(args) == 0 {
			return &depthRet
		}
		depth := args[0].Expr()
		if depth.Kind() != ast.LiteralKind {
			depthRet = 256
			return &depthRet
		}
		if depth.AsLiteral().Type() != types.IntType {
			return nil
		}
		depthVal := depth.AsLiteral().(types.Int)
		depthRet = float64(depthVal)
		return &depthRet
	})
}

func estimateListsRangeCost() checker.FunctionEstimator {
	return func(estimator checker.CostEstimator, target *checker.AstNode, args []checker.AstNode) *checker.CallEstimate {
		sizeExpr := args[0].Expr()
		if sizeExpr.Kind() != ast.LiteralKind {
			return &checker.CallEstimate{CostEstimate: checker.UnknownCostEstimate()}
		}
		sizeEstimate := checker.UnknownSizeEstimate()
		sz := sizeExpr.AsLiteral()
		if szInt, ok := sz.(types.Int); ok {
			sizeEstimate = checker.FixedSizeEstimate(uint64(szInt))
		}
		costEstimate := sizeEstimate.MultiplyByCostFactor(1).Add(createListCostEstimate).Add(callCostEstimate)
		return &checker.CallEstimate{CostEstimate: costEstimate, ResultSize: &sizeEstimate}
	}
}

func estimateListReverse() checker.FunctionEstimator {
	return estimateListCopy( /* variableCost= */ false, func([]checker.AstNode) *float64 {
		depth := float64(1)
		return &depth
	})
}

func estimateListSlice() checker.FunctionEstimator {
	return func(estimator checker.CostEstimator, target *checker.AstNode, args []checker.AstNode) *checker.CallEstimate {
		sizeEstimate := checker.EstimateSize(estimator, *target)
		startVal := uint64(0)
		startExpr := args[0].Expr()
		if startExpr.Kind() == ast.LiteralKind && startExpr.AsLiteral().Type() == types.IntType {
			startVal = uint64(startExpr.AsLiteral().(types.Int))
		}
		endVal := sizeEstimate.Max
		endExpr := args[1].Expr()
		if endExpr.Kind() == ast.LiteralKind && endExpr.AsLiteral().Type() == types.IntType {
			endVal = uint64(endExpr.AsLiteral().(types.Int))
		}
		if endVal >= startVal {
			sizeEstimate = checker.FixedSizeEstimate(endVal - startVal)
		}
		costEstimate := sizeEstimate.MultiplyByCostFactor(1).Add(createListCostEstimate).Add(callCostEstimate)
		return &checker.CallEstimate{CostEstimate: costEstimate, ResultSize: &sizeEstimate}
	}
}

func estimateListSortType(cmpFactor float64, elemType *types.Type) checker.FunctionEstimator {
	return func(estimator checker.CostEstimator, target *checker.AstNode, args []checker.AstNode) *checker.CallEstimate {
		// the input keys will be copied, and then the sort will take size(list) * log(size(list))
		// then a new list constructed based on key order
		t := *target
		targetSize := checker.EstimateSize(estimator, t)
		minSize := math.Ceil(math.Log2(float64(targetSize.Min)))
		maxSize := math.Ceil(math.Log2(float64(targetSize.Max)))
		costScale := checker.CostEstimate{Min: uint64(minSize), Max: uint64(maxSize)}
		costEstimate := targetSize.MultiplyByCost(costScale).
			MultiplyByCostFactor(cmpFactor).
			Add(createListCostEstimate).
			Add(callCostEstimate)
		return &checker.CallEstimate{CostEstimate: costEstimate, ResultSize: &targetSize}
	}
}

func estimateListCopy(variableCost bool, depthEstimator func([]checker.AstNode) *float64) checker.FunctionEstimator {
	return func(estimator checker.CostEstimator, target *checker.AstNode, args []checker.AstNode) *checker.CallEstimate {
		depth := depthEstimator(args)
		if depth == nil {
			return nil
		}
		costFactor := 1.0
		targetSize := checker.EstimateSize(estimator, *target)
		if *depth > 1 {
			if variableCost {
				itemsNode := newItemsNode(*target)
				elemSize := checker.EstimateSize(estimator, itemsNode)
				targetSize = targetSize.Multiply(elemSize)
			}
			sz := math.Ceil(costFactor * *depth)
			targetSize = targetSize.Multiply(checker.FixedSizeEstimate(uint64(sz)))
		}
		costEstimate := targetSize.MultiplyByCostFactor(1).Add(createListCostEstimate).Add(callCostEstimate)
		return &checker.CallEstimate{CostEstimate: costEstimate, ResultSize: &targetSize}
	}
}

func newItemsNode(n checker.AstNode) itemsNode {
	p := make([]string, len(n.Path())+1)
	copy(p, n.Path())
	p[len(n.Path())] = "@items"
	return itemsNode{
		AstNode: n,
		path:    p,
	}
}

type itemsNode struct {
	checker.AstNode
	path []string
}

func (n itemsNode) Path() []string {
	return n.path
}

func (n itemsNode) Type() *types.Type {
	t := n.AstNode.Type()
	if t.TypeName() == "list" {
		return t.Parameters()[0]
	}
	return types.DynType
}

func (n itemsNode) ComputedSize() *checker.SizeEstimate {
	return nil
}

func trackListDistinctCost() interpreter.FunctionTracker {
	return func(args []ref.Val, _ ref.Val) *uint64 {
		cmpFactor := float64(0.0)
		size := float64(0.0)
		target := args[0]
		if l, ok := target.(traits.Lister); ok {
			it := l.Iterator()
			for it.HasNext() == types.True {
				elem := it.Next()
				elemSize := float64(interpreter.ActualSize(elem))
				cmpFactor += elemSize * 0.1
				size += 1.0
			}
		}
		cost := common.ListCreateBaseCost + callCost + uint64(math.Ceil(cmpFactor*size))
		return &cost
	}
}

func trackListFlattenCost() interpreter.FunctionTracker {
	return func(args []ref.Val, result ref.Val) *uint64 {
		target := args[0]
		depth := int64(1)
		if len(args) == 2 {
			if d, ok := numberToInt(args[1]); ok {
				depth = d
			}
		}
		if l, ok := target.(traits.Lister); ok {
			cost := flattenCount(l, depth) + common.ListCreateBaseCost + callCost
			return &cost
		}
		return nil
	}
}

func trackListReverseCost() interpreter.FunctionTracker {
	return func(_ []ref.Val, result ref.Val) *uint64 {
		size := interpreter.ActualSize(result)
		cost := common.ListCreateBaseCost + callCost + size
		return &cost
	}
}

func trackListSliceCost() interpreter.FunctionTracker {
	return func(_ []ref.Val, result ref.Val) *uint64 {
		size := interpreter.ActualSize(result)
		cost := common.ListCreateBaseCost + callCost + size
		return &cost
	}
}

func trackListsRangeCost() interpreter.FunctionTracker {
	return func(_ []ref.Val, result ref.Val) *uint64 {
		size := interpreter.ActualSize(result)
		cost := common.ListCreateBaseCost + callCost + size
		return &cost
	}
}

func genRange(n types.Int) ref.Val {
	newList := make([]ref.Val, n)
	for i := types.Int(0); i < n; i++ {
		newList[i] = i
	}
	return types.DefaultTypeAdapter.NativeToValue(newList)
}

func reverseList(list traits.Lister) ref.Val {
	listLength := list.Size().(types.Int)
	newList := make([]ref.Val, listLength)
	for i := types.Int(0); i < listLength; i++ {
		val := list.Get(listLength - i - 1)
		newList[i] = val
	}
	return types.DefaultTypeAdapter.NativeToValue(newList)
}

func slice(list traits.Lister, start, end types.Int) (ref.Val, error) {
	listLength := list.Size().(types.Int)
	if start < 0 || end < 0 {
		return nil, fmt.Errorf("cannot slice(%d, %d), negative indexes not supported", start, end)
	}
	if start > end {
		return nil, fmt.Errorf("cannot slice(%d, %d), start index must be less than or equal to end index", start, end)
	}
	if listLength < end {
		return nil, fmt.Errorf("cannot slice(%d, %d), list is length %d", start, end, listLength)
	}

	var newList []ref.Val
	for i := types.Int(start); i < end; i++ {
		val := list.Get(i)
		newList = append(newList, val)
	}
	return types.DefaultTypeAdapter.NativeToValue(newList), nil
}

func flatten(list traits.Lister, depth int64) ([]ref.Val, error) {
	if depth < 0 {
		return nil, fmt.Errorf("level must be non-negative")
	}

	var newList []ref.Val
	iter := list.Iterator()
	for iter.HasNext() == types.True {
		val := iter.Next()
		nestedList, isList := val.(traits.Lister)

		if !isList || depth == 0 {
			newList = append(newList, val)
			continue
		}
		flattenedList, err := flatten(nestedList, depth-1)
		if err != nil {
			return nil, err
		}
		newList = append(newList, flattenedList...)
	}

	return newList, nil
}

func flattenCount(list traits.Lister, depth int64) uint64 {
	if depth <= 1 {
		sz := list.Size().(types.Int)
		return uint64(sz)
	}
	count := uint64(0)
	iter := list.Iterator()
	for iter.HasNext() == types.True {
		val := iter.Next()
		nestedList, isList := val.(traits.Lister)
		if !isList {
			count++
			continue
		}
		count += flattenCount(nestedList, depth-1)
	}
	return count
}

func sortList(list traits.Lister) (ref.Val, error) {
	listLength := list.Size().(types.Int)
	if listLength == 0 {
		return list, nil
	}
	elem := list.Get(types.IntZero)
	if _, ok := elem.(traits.Comparer); !ok {
		return nil, fmt.Errorf("list elements must be comparable")
	}
	sortedList := make([]ref.Val, listLength)
	for i := types.IntZero; i < listLength; i++ {
		value := list.Get(i)
		if value.Type() != elem.Type() {
			return nil, fmt.Errorf("list elements must have the same type")
		}
		sortedList[i] = value
	}
	slices.SortFunc(sortedList, func(a, b ref.Val) int {
		cmp := a.(traits.Comparer).Compare(b)
		result := cmp.(types.Int)
		return int(result)
	})
	return types.DefaultTypeAdapter.NativeToValue(sortedList), nil
}

// Internal function used for the implementation of sort() and sortBy().
//
// Sorts a list of arbitrary elements, according to the order produced by sorting
// another list of comparable elements. If the element type of the keys is not
// comparable or the element types are not the same, the function will produce an error.
//
//	<list(T)>.@sortByAssociatedKeys(<list(U)>) -> <list(T)>
//	U in {int, uint, double, bool, duration, timestamp, string, bytes}
//
// Example:
//
//	["foo", "bar", "baz"].@sortByAssociatedKeys([3, 1, 2]) // return ["bar", "baz", "foo"]
func sortListByAssociatedKeys(list, keys traits.Lister) (ref.Val, error) {
	listLength := list.Size().(types.Int)
	keysLength := keys.Size().(types.Int)
	if listLength != keysLength {
		return nil, fmt.Errorf(
			"@sortByAssociatedKeys() expected a list of the same size as the associated keys list, but got %d and %d elements respectively",
			listLength,
			keysLength,
		)
	}
	if listLength == 0 {
		return list, nil
	}
	elem := keys.Get(types.IntZero)
	if _, ok := elem.(traits.Comparer); !ok {
		return nil, fmt.Errorf("list elements must be comparable")
	}
	sortedList := make([]ref.Val, listLength)
	for i := types.IntZero; i < listLength; i++ {
		if keys.Get(i).Type() != elem.Type() {
			return nil, fmt.Errorf("list elements must have the same type")
		}
		sortedList[i] = i
	}
	sort.Slice(sortedList, func(i, j int) bool {
		iKey := keys.Get(sortedList[i])
		jKey := keys.Get(sortedList[j])
		return iKey.(traits.Comparer).Compare(jKey) == types.IntNegOne
	})

	sorted := make([]ref.Val, 0, listLength)

	for _, sortedIdx := range sortedList {
		sorted = append(sorted, list.Get(sortedIdx))
	}
	return types.DefaultTypeAdapter.NativeToValue(sorted), nil
}

// sortByMacro transforms an expression like:
//
//	mylistExpr.sortBy(e, -math.abs(e))
//
// into something equivalent to:
//
//	cel.bind(
//	   __sortBy_input__,
//	   myListExpr,
//	   __sortBy_input__.@sortByAssociatedKeys(__sortBy_input__.map(e, -math.abs(e))
//	)
func sortByMacro(meh cel.MacroExprFactory, target ast.Expr, args []ast.Expr) (ast.Expr, *cel.Error) {
	varIdent := meh.NewIdent("@__sortBy_input__")
	varName := varIdent.AsIdent()

	targetKind := target.Kind()
	if targetKind != ast.ListKind &&
		targetKind != ast.SelectKind &&
		targetKind != ast.IdentKind &&
		targetKind != ast.ComprehensionKind &&
		targetKind != ast.CallKind {
		return nil, meh.NewError(target.ID(), "sortBy can only be applied to a list, identifier, comprehension, call or select expression")
	}

	mapCompr, err := parser.MakeMap(meh, meh.Copy(varIdent), args)
	if err != nil {
		return nil, err
	}
	callExpr := meh.NewMemberCall("@sortByAssociatedKeys",
		meh.Copy(varIdent),
		mapCompr,
	)

	bindExpr := meh.NewComprehension(
		meh.NewList(),
		"#unused",
		varName,
		target,
		meh.NewLiteral(types.False),
		varIdent,
		callExpr,
	)

	return bindExpr, nil
}

func distinctList(list traits.Lister) ref.Val {
	listLength := list.Size().(types.Int)
	if listLength == 0 {
		return list
	}
	uniqueList := make([]ref.Val, 0, listLength)
	for i := types.IntZero; i < listLength; i++ {
		val := list.Get(i)
		seen := false
		for j := types.IntZero; j < types.Int(len(uniqueList)); j++ {
			if i == j {
				continue
			}
			other := uniqueList[j]
			if types.Equal(val, other) == types.True {
				seen = true
				break
			}
		}
		if !seen {
			uniqueList = append(uniqueList, val)
		}
	}
	return types.DefaultTypeAdapter.NativeToValue(uniqueList)
}

func templatedOverloads(types []*cel.Type, template func(t *cel.Type) cel.FunctionOpt) []cel.FunctionOpt {
	overloads := make([]cel.FunctionOpt, len(types))
	for i, t := range types {
		overloads[i] = template(t)
	}
	return overloads
}

func numberToUint(v ref.Val) (uint64, bool) {
	switch val := v.(type) {
	case types.Int:
		if val >= types.IntZero {
			return uint64(val), true
		}
	case types.Uint:
		return uint64(val), true
	case types.Double:
		conv := val.ConvertToType(types.UintType)
		if !types.IsError(conv) {
			return uint64(conv.(types.Uint)), true
		}
	}
	return 0, false
}

func numberToInt(v ref.Val) (int64, bool) {
	switch val := v.(type) {
	case types.Int:
		return int64(val), true
	case types.Uint:
		if val <= math.MaxInt64 {
			return int64(val), true
		}
	case types.Double:
		conv := val.ConvertToType(types.UintType)
		if !types.IsError(conv) {
			return int64(conv.(types.Uint)), true
		}
	}
	return 0, false
}

var (
	comparableTypes = []*cel.Type{
		cel.IntType,
		cel.UintType,
		cel.DoubleType,
		cel.BoolType,
		cel.DurationType,
		cel.TimestampType,
		cel.StringType,
		cel.BytesType,
	}

	createListCostEstimate = checker.FixedCostEstimate(common.ListCreateBaseCost)
	compareCostFactor      = float64(0.1)
)
