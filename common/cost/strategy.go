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
	"slices"

	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/operators"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

// SizingStrategy defines how sizes and container element sizes are estimated and tracked.
type SizingStrategy interface {
	// EstimateSize computes the size estimate for an AST node during cost estimation.
	// For lists and maps, the returned SizeEstimate can recursively include Elem and Key sizes.
	EstimateSize(ctx EstimateContext, node AstNode) (SizeEstimate, bool)

	// TrackSize computes the actual runtime size of a value during cost tracking.
	TrackSize(ctx TrackContext, value ref.Val) (uint64, bool)
}

// DefaultSizingStrategy returns the default SizingStrategy implementation.
func DefaultSizingStrategy() SizingStrategy {
	return defaultSizing
}

type defaultSizingStrategy struct{}

func (d defaultSizingStrategy) EstimateSize(ctx EstimateContext, node AstNode) (SizeEstimate, bool) {
	if node == nil {
		return SizeEstimate{}, false
	}
	if sz := node.ComputedSize(); sz != nil {
		return *sz, true
	}
	if node.Type() == nil {
		if ctx != nil && ctx.Estimator() != nil {
			if sz := ctx.Estimator().EstimateSize(node); sz != nil {
				return *sz, true
			}
		}
		return SizeEstimate{}, false
	}
	switch node.Type().Kind() {
	case types.ListKind:
		elemType := types.DynType
		if len(node.Type().Parameters()) > 0 {
			elemType = node.Type().Parameters()[0]
		}
		var listSize *SizeEstimate
		var elemSize *SizeEstimate
		if node.Expr() != nil {
			switch node.Expr().Kind() {
			case ast.ListKind:
				elements := node.Expr().AsList().Elements()
				l := FixedSizeEstimate(uint64(len(elements)))
				listSize = &l
				if len(elements) > 0 {
					for _, elem := range elements {
						elemNode := NewAstNode(elem, nil, elemType, nil)
						sz := ctx.Size(elemNode)
						if elemSize == nil {
							elemSize = &sz
						} else {
							u := elemSize.Union(sz)
							elemSize = &u
						}
					}
				}
			case ast.CallKind:
				call := node.Expr().AsCall()
				args := call.Args()
				switch call.FunctionName() {
				case operators.Add:
					if len(args) == 2 {
						lhs := ctx.Size(NewAstNode(args[0], nil, node.Type(), nil))
						rhs := ctx.Size(NewAstNode(args[1], nil, node.Type(), nil))
						added := lhs.Add(rhs)
						listSize = &added
						elemSize = added.Elem
					}
				case operators.Conditional:
					if len(args) == 3 {
						tVal := ctx.Size(NewAstNode(args[1], nil, node.Type(), nil))
						fVal := ctx.Size(NewAstNode(args[2], nil, node.Type(), nil))
						u := tVal.Union(fVal)
						listSize = &u
						elemSize = u.Elem
					}
				}
			}
		}
		// 2. Estimator and 1-level path hint lookups
		if listSize == nil && ctx != nil && ctx.Estimator() != nil {
			listSize = ctx.Estimator().EstimateSize(node)
		}
		if elemSize == nil {
			if len(node.Path()) > 0 && ctx != nil && ctx.Estimator() != nil {
				elemPath := append(slices.Clone(node.Path()), "@items")
				elemNode := NewAstNode(nil, elemPath, elemType, nil)
				elemSize = ctx.Estimator().EstimateSize(elemNode)
			}
			if elemSize == nil {
				if sz := computeTypeSize(elemType); sz != nil {
					elemSize = sz
				}
			}
		}
		if listSize != nil {
			if elemSize != nil {
				listSize.Elem = elemSize
			}
			return *listSize, true
		}
		if elemSize != nil {
			res := UnknownSizeEstimate()
			res.Elem = elemSize
			return res, true
		}
		return SizeEstimate{}, false

	case types.MapKind:
		keyType := types.DynType
		valType := types.DynType
		if len(node.Type().Parameters()) >= 2 {
			keyType = node.Type().Parameters()[0]
			valType = node.Type().Parameters()[1]
		}
		var mapSize *SizeEstimate
		var keySize *SizeEstimate
		var valSize *SizeEstimate
		if node.Expr() != nil {
			switch node.Expr().Kind() {
			case ast.MapKind:
				entries := node.Expr().AsMap().Entries()
				m := FixedSizeEstimate(uint64(len(entries)))
				mapSize = &m
				for _, entry := range entries {
					mapEntry := entry.AsMapEntry()
					kNode := NewAstNode(mapEntry.Key(), nil, keyType, nil)
					vNode := NewAstNode(mapEntry.Value(), nil, valType, nil)
					kSz := ctx.Size(kNode)
					vSz := ctx.Size(vNode)
					if keySize == nil {
						keySize = &kSz
					} else {
						u := keySize.Union(kSz)
						keySize = &u
					}
					if valSize == nil {
						valSize = &vSz
					} else {
						u := valSize.Union(vSz)
						valSize = &u
					}
				}
			case ast.CallKind:
				call := node.Expr().AsCall()
				args := call.Args()
				if call.FunctionName() == operators.Conditional && len(args) == 3 {
					tVal := ctx.Size(NewAstNode(args[1], nil, node.Type(), nil))
					fVal := ctx.Size(NewAstNode(args[2], nil, node.Type(), nil))
					u := tVal.Union(fVal)
					mapSize = &u
					keySize = u.Key
					valSize = u.Elem
				}
			}
		}
		// 2. Estimator and 1-level path hint lookups
		if mapSize == nil && ctx != nil && ctx.Estimator() != nil {
			mapSize = ctx.Estimator().EstimateSize(node)
		}
		if len(node.Path()) > 0 && ctx != nil && ctx.Estimator() != nil {
			if keySize == nil {
				kPath := append(slices.Clone(node.Path()), "@keys")
				keySize = ctx.Estimator().EstimateSize(NewAstNode(nil, kPath, keyType, nil))
			}
			if valSize == nil {
				vPath := append(slices.Clone(node.Path()), "@values")
				valSize = ctx.Estimator().EstimateSize(NewAstNode(nil, vPath, valType, nil))
			}
		}
		if keySize == nil {
			if sz := computeTypeSize(keyType); sz != nil {
				keySize = sz
			}
		}
		if valSize == nil {
			if sz := computeTypeSize(valType); sz != nil {
				valSize = sz
			}
		}
		if mapSize != nil {
			mapSize.Key = keySize
			mapSize.Elem = valSize
			return *mapSize, true
		}
		if keySize != nil || valSize != nil {
			res := UnknownSizeEstimate()
			res.Key = keySize
			res.Elem = valSize
			return res, true
		}
		return SizeEstimate{}, false

	default:
		if ctx != nil && ctx.Estimator() != nil {
			if sz := ctx.Estimator().EstimateSize(node); sz != nil {
				return *sz, true
			}
		}
		if sz := computeTypeSize(node.Type()); sz != nil {
			return *sz, true
		}
	}
	return SizeEstimate{}, false
}

func (defaultSizingStrategy) TrackSize(ctx TrackContext, value ref.Val) (uint64, bool) {
	if value == nil {
		return 0, false
	}
	return ActualSize(value), true
}

var defaultSizing SizingStrategy = defaultSizingStrategy{}

// AggregateSizingStrategy returns a SizingStrategy that computes recursive size estimates
// by following paths during cost estimation, and calculates actual runtime size using
// AggregateSize during cost tracking. By default, stringUnitLength is configured to 1
// so that unscaled character/byte lengths are preserved for cost modeling.
func AggregateSizingStrategy(opts ...types.SizeCalculatorOption) SizingStrategy {
	if len(opts) == 0 {
		return defaultAggregateSizing
	}
	defaultOpts := []types.SizeCalculatorOption{types.SizeCalculatorStringUnitLength(1)}
	return &aggregateSizingStrategy{calc: types.NewSizeCalculator(append(defaultOpts, opts...)...)}
}

type aggregateSizingStrategy struct {
	calc *types.SizeCalculator
}

func (a *aggregateSizingStrategy) TrackSize(ctx TrackContext, value ref.Val) (uint64, bool) {
	if value == nil {
		return 0, false
	}
	return uint64(a.calc.AggregateSize(value)), true
}

func (a *aggregateSizingStrategy) EstimateSize(ctx EstimateContext, node AstNode) (SizeEstimate, bool) {
	if node == nil {
		return SizeEstimate{}, false
	}
	if sz := node.ComputedSize(); sz != nil {
		return *sz, true
	}
	if node.Type() == nil {
		if ctx != nil && ctx.Estimator() != nil {
			if sz := ctx.Estimator().EstimateSize(node); sz != nil {
				return *sz, true
			}
		}
		return SizeEstimate{}, false
	}
	switch node.Type().Kind() {
	case types.ListKind:
		elemType := types.DynType
		if len(node.Type().Parameters()) > 0 {
			elemType = node.Type().Parameters()[0]
		}
		var listSize *SizeEstimate
		var elemSize *SizeEstimate
		// 1. Literal list expression or call (Add, Conditional): compute sizes
		if node.Expr() != nil {
			switch node.Expr().Kind() {
			case ast.ListKind:
				elements := node.Expr().AsList().Elements()
				l := FixedSizeEstimate(uint64(len(elements)))
				listSize = &l
				if len(elements) > 0 {
					for _, elem := range elements {
						elemNode := NewAstNode(elem, nil, elemType, nil)
						sz := ctx.Size(elemNode)
						if elemSize == nil {
							elemSize = &sz
						} else {
							u := elemSize.Union(sz)
							elemSize = &u
						}
					}
				}
			case ast.CallKind:
				call := node.Expr().AsCall()
				args := call.Args()
				switch call.FunctionName() {
				case operators.Add:
					if len(args) == 2 {
						lhs := ctx.Size(NewAstNode(args[0], nil, node.Type(), nil))
						rhs := ctx.Size(NewAstNode(args[1], nil, node.Type(), nil))
						added := lhs.Add(rhs)
						listSize = &added
						elemSize = added.Elem
					}
				case operators.Conditional:
					if len(args) == 3 {
						tVal := ctx.Size(NewAstNode(args[1], nil, node.Type(), nil))
						fVal := ctx.Size(NewAstNode(args[2], nil, node.Type(), nil))
						u := tVal.Union(fVal)
						listSize = &u
						elemSize = u.Elem
					}
				}
			}
		}
		// 2. Estimator and recursive path hint lookups
		if listSize == nil && ctx != nil && ctx.Estimator() != nil {
			listSize = ctx.Estimator().EstimateSize(node)
		}
		if elemSize == nil && listSize != nil && listSize.Elem != nil {
			elemSize = listSize.Elem
		}
		if elemSize == nil || (elemSize.Elem == nil && (elemType.Kind() == types.ListKind || elemType.Kind() == types.MapKind)) {
			if len(node.Path()) > 0 && ctx != nil {
				elemPath := append(slices.Clone(node.Path()), "@items")
				elemNode := NewAstNode(nil, elemPath, elemType, nil)
				sz := ctx.Size(elemNode)
				if sz != UnknownSizeEstimate() {
					if elemSize == nil {
						elemSize = &sz
					} else {
						elemSize.Elem = sz.Elem
						elemSize.Key = sz.Key
					}
				}
			}
		}
		if elemSize == nil {
			if sz := computeTypeSize(elemType); sz != nil {
				elemSize = sz
			} else if ctx != nil && ctx.Estimator() != nil {
				elemNode := NewAstNode(nil, nil, elemType, nil)
				elemSize = ctx.Estimator().EstimateSize(elemNode)
			}
		}
		if listSize != nil {
			if elemSize != nil {
				listSize.Elem = elemSize
			}
			return *listSize, true
		}
		if elemSize != nil {
			res := UnknownSizeEstimate()
			res.Elem = elemSize
			return res, true
		}
		return SizeEstimate{}, false

	case types.MapKind:
		keyType := types.DynType
		valType := types.DynType
		if len(node.Type().Parameters()) >= 2 {
			keyType = node.Type().Parameters()[0]
			valType = node.Type().Parameters()[1]
		}
		var mapSize *SizeEstimate
		var keySize *SizeEstimate
		var valSize *SizeEstimate
		// 1. Literal map expression or call (Conditional): compute sizes
		if node.Expr() != nil {
			switch node.Expr().Kind() {
			case ast.MapKind:
				entries := node.Expr().AsMap().Entries()
				m := FixedSizeEstimate(uint64(len(entries)))
				mapSize = &m
				for _, entry := range entries {
					mapEntry := entry.AsMapEntry()
					kNode := NewAstNode(mapEntry.Key(), nil, keyType, nil)
					vNode := NewAstNode(mapEntry.Value(), nil, valType, nil)
					kSz := ctx.Size(kNode)
					vSz := ctx.Size(vNode)
					if keySize == nil {
						keySize = &kSz
					} else {
						u := keySize.Union(kSz)
						keySize = &u
					}
					if valSize == nil {
						valSize = &vSz
					} else {
						u := valSize.Union(vSz)
						valSize = &u
					}
				}
			case ast.CallKind:
				call := node.Expr().AsCall()
				args := call.Args()
				if call.FunctionName() == operators.Conditional && len(args) == 3 {
					tVal := ctx.Size(NewAstNode(args[1], nil, node.Type(), nil))
					fVal := ctx.Size(NewAstNode(args[2], nil, node.Type(), nil))
					u := tVal.Union(fVal)
					mapSize = &u
					keySize = u.Key
					valSize = u.Elem
				}
			}
		}
		// 2. Estimator and recursive path hint lookups
		if mapSize == nil && ctx != nil && ctx.Estimator() != nil {
			mapSize = ctx.Estimator().EstimateSize(node)
		}
		if keySize == nil && mapSize != nil && mapSize.Key != nil {
			keySize = mapSize.Key
		}
		if valSize == nil && mapSize != nil && mapSize.Elem != nil {
			valSize = mapSize.Elem
		}
		if len(node.Path()) > 0 && ctx != nil {
			if keySize == nil || (keySize.Elem == nil && (keyType.Kind() == types.ListKind || keyType.Kind() == types.MapKind)) {
				kPath := append(slices.Clone(node.Path()), "@keys")
				kSz := ctx.Size(NewAstNode(nil, kPath, keyType, nil))
				if kSz != UnknownSizeEstimate() {
					if keySize == nil {
						keySize = &kSz
					} else {
						keySize.Elem = kSz.Elem
						keySize.Key = kSz.Key
					}
				}
			}
			if valSize == nil || (valSize.Elem == nil && (valType.Kind() == types.ListKind || valType.Kind() == types.MapKind)) {
				vPath := append(slices.Clone(node.Path()), "@values")
				vSz := ctx.Size(NewAstNode(nil, vPath, valType, nil))
				if vSz != UnknownSizeEstimate() {
					if valSize == nil {
						valSize = &vSz
					} else {
						valSize.Elem = vSz.Elem
						valSize.Key = vSz.Key
					}
				}
			}
		}
		if keySize == nil {
			if sz := computeTypeSize(keyType); sz != nil {
				keySize = sz
			} else if ctx != nil && ctx.Estimator() != nil {
				keySize = ctx.Estimator().EstimateSize(NewAstNode(nil, nil, keyType, nil))
			}
		}
		if valSize == nil {
			if sz := computeTypeSize(valType); sz != nil {
				valSize = sz
			} else if ctx != nil && ctx.Estimator() != nil {
				valSize = ctx.Estimator().EstimateSize(NewAstNode(nil, nil, valType, nil))
			}
		}
		if mapSize != nil {
			mapSize.Key = keySize
			mapSize.Elem = valSize
			return *mapSize, true
		}
		if keySize != nil || valSize != nil {
			res := UnknownSizeEstimate()
			res.Key = keySize
			res.Elem = valSize
			return res, true
		}
		return SizeEstimate{}, false

	default:
		if ctx != nil && ctx.Estimator() != nil {
			if sz := ctx.Estimator().EstimateSize(node); sz != nil {
				return *sz, true
			}
		}
		if sz := computeTypeSize(node.Type()); sz != nil {
			return *sz, true
		}
	}
	return SizeEstimate{}, false
}

var defaultAggregateSizing SizingStrategy = &aggregateSizingStrategy{calc: types.NewSizeCalculator(types.SizeCalculatorStringUnitLength(1))}
