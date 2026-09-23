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

// ModelVersion selects a revision of the cost model's estimation rules.
//
// Revisions exist so that corrections to the model can land without silently moving numbers that
// existing deployments budget against. A caller holding estimates recorded under an earlier release
// can pin to the revision those estimates were produced under, review the delta at leisure, and
// adopt the correction deliberately.
//
// New callers get LatestModelVersion. Pinning is an escape hatch rather than a supported
// configuration: every revision corrects a defect, so an older revision is by construction less
// accurate than a newer one.
//
// Revisions affect estimation only. The cost charged at runtime is not versioned, because a
// revision that moved the charge would change what a given expression is billed rather than what it
// is predicted to be billed.
type ModelVersion uint32

const (
	// ModelVersion0 is the cost model as released in v0.32.0.
	//
	// Min() reports a lower bound of 1 for any interval whose upper bound is non-zero, rather than
	// the true minimum of its operands. For a comparison between two values of equal, known size
	// this understates the floor: the traversal always visits every element, so the minimum and the
	// maximum coincide.
	ModelVersion0 ModelVersion = 0

	// ModelVersion1 computes Min() as the exact minimum of its operands' intervals.
	//
	// The lower bound moves in both directions relative to ModelVersion0. It rises where both
	// operands are large and equally sized, and falls where an operand can legitimately be empty.
	ModelVersion1 ModelVersion = 1
)

// LatestModelVersion is the revision used when none is requested.
const LatestModelVersion = ModelVersion1

// ModelOption configures how an OverloadModel is compiled into an estimator or a tracker.
type ModelOption func(*modelOptions)

// modelOptions is the resolved configuration for compiling an OverloadModel.
type modelOptions struct {
	strategy SizingStrategy
	version  ModelVersion
}

// newModelOptions resolves the supplied options over the defaults.
func newModelOptions(opts ...ModelOption) *modelOptions {
	resolved := &modelOptions{
		strategy: DefaultSizingStrategy(),
		version:  LatestModelVersion,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(resolved)
		}
	}
	if resolved.strategy == nil {
		resolved.strategy = DefaultSizingStrategy()
	}
	return resolved
}

// WithSizingStrategy sets the SizingStrategy used to resolve sizes.
//
// A nil strategy is ignored, leaving the default in place.
func WithSizingStrategy(strategy SizingStrategy) ModelOption {
	return func(o *modelOptions) {
		if strategy != nil {
			o.strategy = strategy
		}
	}
}

// WithModelVersion pins the revision of the estimation rules.
func WithModelVersion(version ModelVersion) ModelOption {
	return func(o *modelOptions) {
		o.version = version
	}
}

// versionedEstimateContext is implemented by EstimateContext values that carry a model revision.
//
// It is deliberately unexported: EstimateContext is an interface third parties may implement, and
// adding a method to it would break them. Contexts that do not implement this are treated as
// running at the latest revision, which is the correct default for a context built without any
// knowledge of pinning.
type versionedEstimateContext interface {
	modelVersion() ModelVersion
}

// contextModelVersion returns the revision a context was created under.
func contextModelVersion(ctx EstimateContext) ModelVersion {
	if v, ok := ctx.(versionedEstimateContext); ok {
		return v.modelVersion()
	}
	return LatestModelVersion
}
