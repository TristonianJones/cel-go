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

import "testing"

// unversionedContext stands in for a third-party EstimateContext, which cannot implement the
// unexported versionedEstimateContext interface.
type unversionedContext struct {
	EstimateContext
}

func TestContextModelVersion(t *testing.T) {
	tests := []struct {
		name string
		ctx  EstimateContext
		want ModelVersion
	}{
		{
			// A context with no revision of its own is treated as running at the latest, which is
			// the right default for a context built without any knowledge of pinning.
			name: "unversioned_context_defaults_to_latest",
			ctx:  unversionedContext{},
			want: LatestModelVersion,
		},
		{
			// estimatorContext must forward its coster's pin. It is the context handed to sizing
			// strategies and user function estimators, so a silent default here would ignore the
			// caller's pin.
			name: "estimator_context_reports_coster_pin",
			ctx:  (&coster{modelVersion: ModelVersion0}).newEstimateContext(nil, nil),
			want: ModelVersion0,
		},
		{
			name: "estimator_context_reports_latest_pin",
			ctx:  (&coster{modelVersion: ModelVersion1}).newEstimateContext(nil, nil),
			want: ModelVersion1,
		},
		{
			// estimatorEvalContext bakes the revision in at build time, since it has no coster to
			// consult.
			name: "eval_context_reports_baked_in_version",
			ctx:  &estimatorEvalContext{version: ModelVersion0},
			want: ModelVersion0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextModelVersion(tc.ctx); got != tc.want {
				t.Errorf("contextModelVersion() got %v, wanted %v", got, tc.want)
			}
		})
	}
}

func TestNewModelOptions(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		opts := newModelOptions()
		if opts.version != LatestModelVersion {
			t.Errorf("version got %v, wanted %v", opts.version, LatestModelVersion)
		}
		if opts.strategy == nil {
			t.Error("strategy got nil, wanted the default sizing strategy")
		}
	})
	t.Run("nil option and nil strategy are ignored", func(t *testing.T) {
		opts := newModelOptions(nil, WithSizingStrategy(nil), WithModelVersion(ModelVersion0))
		if opts.strategy == nil {
			t.Error("strategy got nil, wanted the default sizing strategy to be retained")
		}
		if opts.version != ModelVersion0 {
			t.Errorf("version got %v, wanted %v", opts.version, ModelVersion0)
		}
	})
}
