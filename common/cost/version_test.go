// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cost

import (
	"strings"
	"testing"

	"cel.dev/cel-go/common/ast"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
)

type noopSizingStrategy struct{}

func (noopSizingStrategy) EstimateSize(_ EstimateContext, _ AstNode) (SizeEstimate, bool) {
	return SizeEstimate{}, false
}

func (noopSizingStrategy) TrackSize(_ TrackContext, _ ref.Val) (uint64, bool) {
	return 0, false
}

type stubEstimator struct {
	sizeEst *SizeEstimate
	callEst *CallEstimate
}

func (s stubEstimator) EstimateSize(_ AstNode) *SizeEstimate {
	return s.sizeEst
}

func (s stubEstimator) EstimateCallCost(_, _ string, _ *AstNode, _ []AstNode) *CallEstimate {
	return s.callEst
}

func TestModelVersionOfAndVersionedEstimator(t *testing.T) {
	if got := ModelVersionOf(nil); got != defaultModelVersion {
		t.Errorf("ModelVersionOf(nil) = %v, want %v", got, defaultModelVersion)
	}
	if got := ModelVersionOf(stubEstimator{}); got != defaultModelVersion {
		t.Errorf("ModelVersionOf(stubEstimator{}) = %v, want %v", got, defaultModelVersion)
	}

	cNil := mustNewCoster(t, nil, nil, ModelVersion(0))
	estNil := cNil.getEstimator()
	if got := ModelVersionOf(estNil); got != 0 {
		t.Errorf("ModelVersionOf(cNil.getEstimator()) = %v, want 0", got)
	}
	if got := estNil.EstimateSize(nil); got != nil {
		t.Errorf("estNil.EstimateSize(nil) = %v, want nil", got)
	}
	if got := estNil.EstimateCallCost("f", "f_id", nil, nil); got != nil {
		t.Errorf("estNil.EstimateCallCost() = %v, want nil", got)
	}

	sz := FixedSizeEstimate(7)
	call := NewCallEstimate(FixedCostEstimate(3), &sz)
	cDel := mustNewCoster(t, nil, stubEstimator{sizeEst: &sz, callEst: call}, ModelVersion(1))
	estDel := cDel.getEstimator()
	if got := ModelVersionOf(estDel); got != 1 {
		t.Errorf("ModelVersionOf(cDel.getEstimator()) = %v, want 1", got)
	}
	if got := estDel.EstimateSize(nil); got == nil || *got != sz {
		t.Errorf("estDel.EstimateSize(nil) = %v, want %v", got, sz)
	}
	if got := estDel.EstimateCallCost("f", "f_id", nil, nil); got == nil || *got != *call {
		t.Errorf("estDel.EstimateCallCost() = %v, want %v", got, call)
	}
}

func TestCosterHelpersAndEvalContextSize(t *testing.T) {
	fac := ast.NewExprFactory()
	identExpr := fac.NewIdent(1, "x")
	checked := ast.NewCheckedAST(ast.NewAST(identExpr, nil), map[int64]*types.Type{1: types.StringType}, nil)

	// When a custom noop strategy returns false for a variable, sizeOrUnknown returns UnknownSizeEstimate.
	c := mustNewCoster(t, checked, nil, WithSizingStrategy(noopSizingStrategy{}))
	if got := c.sizeOrUnknown(identExpr); got != UnknownSizeEstimate() {
		t.Errorf("sizeOrUnknown(unhinted) = %v, want %v", got, UnknownSizeEstimate())
	}

	// Once a size is recorded on coster, estimatorEvalContext.Size resolves it via e.coster.computeSize.
	expected := FixedSizeEstimate(42)
	c.setSize(identExpr, &expected)
	if got := c.sizeOrUnknown(identExpr); got != expected {
		t.Errorf("sizeOrUnknown(cached) = %v, want %v", got, expected)
	}
	ctx := c.newEstimateContext()
	node := astNode{expr: identExpr, t: types.StringType}
	if got := ctx.Size(node); got != expected {
		t.Errorf("ctx.Size(node) = %v, want %v", got, expected)
	}
}

func TestNewModelOptions(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		opts := newModelOptions()
		if opts.version != defaultModelVersion {
			t.Errorf("version got %v, wanted %v", opts.version, defaultModelVersion)
		}
		if opts.strategy == nil {
			t.Error("strategy got nil, wanted the default sizing strategy")
		}
	})
	t.Run("nil strategy is ignored", func(t *testing.T) {
		opts := newModelOptions(WithSizingStrategy(nil), ModelVersion(0))
		if opts.strategy == nil {
			t.Error("strategy got nil, wanted the default sizing strategy to be retained")
		}
		if opts.version != 0 {
			t.Errorf("version got %v, wanted 0", opts.version)
		}
	})
}

func mustNewCoster(t *testing.T, checked *ast.AST, estimator Estimator, opts ...Option) *coster {
	t.Helper()
	c, err := newCoster(checked, estimator, opts...)
	if err != nil {
		t.Fatalf("newCoster() failed: %v", err)
	}
	return c
}

// TestBuildersOwnTheirInputs guards the immutability of built models: a caller reusing the slice
// it passed to a variadic builder must not change what was built.
func TestBuildersOwnTheirInputs(t *testing.T) {
	models := []OverloadModel{Overload("f", EvalCost(Const(1)))}
	opt := Model(models...)
	models[0] = Overload("f", EvalCost(Const(5)))
	est := newModelOptions(opt).modelSets[0].estimators()["f"](nil, nil, nil)
	if est.CostEstimate != FixedCostEstimate(1) {
		t.Errorf("Model() estimate = %v after the caller's slice changed, want %v", est.CostEstimate, FixedCostEstimate(1))
	}

	terms := []QuantityExpr{Const(2), Const(3)}
	sum := Sum(terms...)
	terms[0] = Const(9)
	if got := sum.estimate(nil); got != FixedSizeEstimate(5) {
		t.Errorf("Sum() estimate = %v after the caller's slice changed, want %v", got, FixedSizeEstimate(5))
	}
}

// TestNilOverloadFunctionsRejected guards that a missing estimator or tracker is reported when the
// estimator or tracker is configured, rather than panicking when the overload is first priced.
func TestNilOverloadFunctionsRejected(t *testing.T) {
	if _, err := newCoster(nil, nil, OverloadCostEstimate("f", nil)); err == nil || !strings.Contains(err.Error(), `OverloadCostEstimate("f")`) {
		t.Errorf("newCoster(OverloadCostEstimate(\"f\", nil)) errored: %v, wanted an error naming the overload", err)
	}
	if _, err := NewTracker(nil, OverloadTracker("f", nil)); err == nil || !strings.Contains(err.Error(), `OverloadTracker("f")`) {
		t.Errorf("NewTracker(OverloadTracker(\"f\", nil)) errored: %v, wanted an error naming the overload", err)
	}
}
